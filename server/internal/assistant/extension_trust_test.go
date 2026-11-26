package assistant

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/proto"
)

// trustMemStore keeps extension trusts and grants, information categories and the track registry in memory.
type trustMemStore struct {
	*infoCategoryMemStore
	tmu    sync.Mutex
	trusts map[string]db.ExtensionTrust
}

func (st *trustMemStore) ExtensionTrusts(context.Context) (map[string]db.ExtensionTrust, error) {
	st.tmu.Lock()
	defer st.tmu.Unlock()
	out := map[string]db.ExtensionTrust{}
	for k, v := range st.trusts {
		v.Categories = slices.Clone(v.Categories)
		out[k] = v
	}
	return out, nil
}

func (st *trustMemStore) SetExtensionTrust(_ context.Context, ext, trust string, cats []string, _ string) error {
	st.tmu.Lock()
	defer st.tmu.Unlock()
	if st.trusts == nil {
		st.trusts = map[string]db.ExtensionTrust{}
	}
	st.trusts[ext] = db.ExtensionTrust{Extension: ext, Trust: trust, Categories: slices.Clone(cats)}
	return nil
}

func (st *trustMemStore) ForgetExtensionTrust(_ context.Context, ext string) error {
	st.tmu.Lock()
	defer st.tmu.Unlock()
	delete(st.trusts, ext)
	return nil
}

// trustService is conversationsService's assistant (its own conversations, a guest's, the health space's) on a store
// that keeps extension trusts and information categories.
func trustService(t *testing.T) (*Service, *trustMemStore) {
	t.Helper()
	f := spaceHub()
	f.sessions["server"] = []proto.Session{
		{MachineID: "server", SessionID: "main-0000-0000", Cwd: "/home/me/.persona/assistant", Assistant: true, Title: "Persona", UpdatedAt: convDay.Add(5 * time.Hour)},
		{MachineID: "server", SessionID: "slee-0000-0000", Cwd: "/home/me/.persona/tracks/qa-private", Assistant: true, Space: testSpace, UpdatedAt: convDay.Add(4 * time.Hour)},
	}
	st := &trustMemStore{infoCategoryMemStore: &infoCategoryMemStore{trackRegistryStore: &trackRegistryStore{memStore: &memStore{}, rows: []db.Track{testSpaceRow}}}}
	s := newServiceOn(t, f, st)
	setSettings(t, s, func(v *Settings) { v.MachineID, v.Main = "server", "main-0000-0000" })
	return s, st
}

// extensionListedIDs are the conversation ids assistant.conversations shows, asked plainly (ext "") or for extension ext.
func extensionListedIDs(t *testing.T, s *Service, ext string) string {
	t.Helper()
	method, params := assistantwire.MethodAssistantConversations, `{"since":"2026-01-02T00:00:00Z"}`
	if ext != "" {
		b, _ := json.Marshal(assistantwire.AssistantExtensionRead{Extension: ext, Method: method, Params: json.RawMessage(params)})
		method, params = assistantwire.MethodAssistantExtensionRead, string(b)
	}
	var res assistantwire.AssistantConversationsResult
	if err := askConversations(t, s, method, params, &res); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range res.Items {
		ids = append(ids, c.ID)
	}
	return strings.Join(ids, ",")
}

// What an extension reads of the health space (#13246, scene 6): nothing unless the user marked it their own (trust
// owner, such as their memory extension) or granted it the health category; a plain read, which names no extension,
// is a third party's with no grant.
func TestExtensionTrustDecidesReads(t *testing.T) {
	ctx := context.Background()
	s, st := trustService(t)
	if got := extensionListedIDs(t, s, ""); got != "main-0000-0000" {
		t.Fatalf("plain read: %s", got)
	}
	if got := extensionListedIDs(t, s, "qa-memory"); got != "main-0000-0000" {
		t.Fatalf("an extension the user never marked: %s", got)
	}
	_ = st.SetExtensionTrust(ctx, "qa-memory", assistantwire.ExtensionTrustOwner, nil, "user")
	if got := extensionListedIDs(t, s, "qa-memory"); got != "main-0000-0000,slee-0000-0000" {
		t.Fatalf("the user's own extension: %s", got)
	}
	if got := extensionListedIDs(t, s, "qa-other"); got != "main-0000-0000" {
		t.Fatalf("another extension, while one is the user's own: %s", got)
	}
	_ = st.SetExtensionTrust(ctx, "qa-other", assistantwire.ExtensionTrustThirdParty, []string{categoryFinance}, "user")
	if got := extensionListedIDs(t, s, "qa-other"); got != "main-0000-0000" {
		t.Fatalf("a third party granted finance only: %s", got)
	}
	_ = st.SetExtensionTrust(ctx, "qa-other", assistantwire.ExtensionTrustThirdParty, []string{categoryHealth}, "user")
	if got := extensionListedIDs(t, s, "qa-other"); got != "main-0000-0000,slee-0000-0000" {
		t.Fatalf("a third party granted health: %s", got)
	}

	// A main conversation tagged with a sensitive category is hidden from a third party not granted it.
	if err := s.tagConversation(ctx, "main-0000-0000", []string{categoryFinance}, infoTaggedByModel, ""); err != nil {
		t.Fatal(err)
	}
	if got := extensionListedIDs(t, s, "qa-third"); got != "" {
		t.Fatalf("a third party, the main conversation tagged finance: %q", got)
	}
	if got := extensionListedIDs(t, s, "qa-memory"); got != "main-0000-0000,slee-0000-0000" {
		t.Fatalf("the user's own extension, the main conversation tagged finance: %s", got)
	}

	// Other reads follow the same grant: the health space's memory is the owner's to read, not a third party's.
	if !s.extensionHides(s.withExtensionReader(ctx, "qa-third"), testSpace) || s.extensionHides(s.withExtensionReader(ctx, "qa-memory"), testSpace) {
		t.Fatal("extensionHides does not follow the grant")
	}
	if !s.extensionHides(ctx, testSpace) || s.extensionHides(ctx, "") {
		t.Fatal("a read with no extension named is not a third party's with no grant")
	}

	// A removed extension's trust goes with it: installed again, it starts as a third party's.
	var none map[string]any
	if err := askConversations(t, s, assistantwire.MethodExtensionTrustForget, `{"extension":"qa-memory"}`, &none); err != nil {
		t.Fatal(err)
	}
	if _, kept := st.trusts["qa-memory"]; kept || !s.extensionHides(s.withExtensionReader(ctx, "qa-memory"), testSpace) {
		t.Fatal("a removed extension is still the user's own")
	}
}
