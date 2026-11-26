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
	"github.com/sunfayao/persona/server/internal/nodeproto"
	"github.com/sunfayao/persona/server/internal/prompts"
)

// captureMemStore keeps captures as assistant_captures does: one per client id, the loop's results only on a capture
// still new or sorted.
type captureMemStore struct {
	*infoCategoryMemStore
	capMu sync.Mutex
	caps  []db.Capture
}

func (st *captureMemStore) InsertCapture(_ context.Context, c db.Capture) (db.Capture, bool, error) {
	st.capMu.Lock()
	defer st.capMu.Unlock()
	for _, was := range st.caps {
		if was.ClientID == c.ClientID {
			return was, false, nil
		}
	}
	c.Status, c.Sort, c.FiledTo, c.CreatedAt, c.UpdatedAt = assistantwire.CaptureNew, []byte("{}"), []byte("{}"), time.Now().UTC(), time.Now().UTC()
	st.caps = append(st.caps, c)
	return c, true, nil
}

func (st *captureMemStore) Captures(_ context.Context, f db.CaptureFilter) ([]db.Capture, error) {
	st.capMu.Lock()
	defer st.capMu.Unlock()
	var out []db.Capture
	for i := len(st.caps) - 1; i >= 0; i-- {
		c := st.caps[i]
		if (len(f.Statuses) == 0 || slices.Contains(f.Statuses, c.Status)) && !c.UpdatedAt.Before(f.UpdatedSince) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (st *captureMemStore) CapturesToSort(_ context.Context, maxAttempts, limit int) ([]db.Capture, error) {
	st.capMu.Lock()
	defer st.capMu.Unlock()
	// New ones first, then the oldest, as the query orders them.
	var out []db.Capture
	for _, wantNew := range []bool{true, false} {
		for _, c := range st.caps {
			var sorted assistantwire.CaptureSort
			_ = json.Unmarshal(c.Sort, &sorted)
			isNew := c.Status == assistantwire.CaptureNew
			if isNew == wantNew && c.Attempts < maxAttempts && (isNew || c.Status == assistantwire.CaptureSorted && sorted.AutoFile) && len(out) < limit {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func (st *captureMemStore) SetCaptureResult(_ context.Context, id, status string, sort, filedTo []byte, errText string, tried bool) (bool, error) {
	st.capMu.Lock()
	defer st.capMu.Unlock()
	for i, c := range st.caps {
		if c.ID != id || (c.Status != assistantwire.CaptureNew && c.Status != assistantwire.CaptureSorted) {
			continue
		}
		c.Status, c.Error, c.UpdatedAt = status, errText, time.Now().UTC()
		if sort != nil {
			c.Sort = sort
		}
		if filedTo != nil {
			c.FiledTo = filedTo
		}
		if tried {
			c.Attempts++
		}
		st.caps[i] = c
		return true, nil
	}
	return false, nil
}

func (st *captureMemStore) capture(t *testing.T, id string) db.Capture {
	t.Helper()
	st.capMu.Lock()
	defer st.capMu.Unlock()
	for _, c := range st.caps {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no capture %s", id)
	return db.Capture{}
}

// inboxDevs are the manager's computers, whose nodes keep track directories and write into their inbox/ unless old.
type inboxDevs struct {
	*fleetDevs
	old bool
}

func (d inboxDevs) NodeAnnounced(_, capability string) bool {
	return capability == nodeproto.TrackSpaces || !d.old && capability == nodeproto.TrackInbox
}

// captureService is the health hub's assistant on a registry with two space tracks that take captures (qa-plain,
// qa-finance with finance by default) and one that does not, the node answering inbox writes into files.
func captureService(t *testing.T) (*Service, *fakeHub, *captureMemStore, *[]map[string]any) {
	t.Helper()
	f := spaceHub()
	f.devices[0].Online = true
	st := &captureMemStore{infoCategoryMemStore: &infoCategoryMemStore{trackRegistryStore: &trackRegistryStore{memStore: &memStore{}, rows: []db.Track{
		{Slug: "qa-plain", Kind: trackKindSpace, Title: "QA plain", Template: trackTemplateSpace, Revision: 1},
		{Slug: "qa-finance", Kind: trackKindSpace, Title: "QA finance", Template: trackTemplateSpace, DefaultCategories: []string{categoryFinance}, Revision: 1},
		{Slug: "qa-closed", Kind: trackKindSpace, Title: "QA closed", Template: trackTemplateSpace, ConfigOverride: []byte(`{"capture_target":false}`), Revision: 1},
	}}}}
	s := newServiceOn(t, f, st)
	setSettings(t, s, func(v *Settings) { v.MachineID = "server" })
	s.devs = inboxDevs{fleetDevs: newFleetDevs()}
	var written []map[string]any
	f.nodes = map[string]nodeAnswer{assistantwire.DeviceTrackInboxWrite: func(_ string, raw json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		written = append(written, p)
		return map[string]any{"path": "inbox/" + p["name"].(string)}, nil
	}}
	return s, f, st, &written
}

// The sorting prompt (#13502), byte for byte; the note goes as data in the user message, cut at 2,000 characters; an
// answer keeps only known categories, a known track and action, a confidence between 0 and 1.
func TestCaptureSortPrompt(t *testing.T) {
	ctx := context.Background()
	m := &infoTagModel{answer: `{"space":"qa-plain","confidence":1.7,"action":"later","title":"  A placeholder\ntitle ","deep":true,"categories":["Finance","someday"]}`}
	note := captureNote(db.Capture{Text: strings.Repeat("x", captureNoteMax+10), URL: "https://example.com/qa"})
	if len([]rune(note)) != captureNoteMax {
		t.Fatalf("note of %d characters", len([]rune(note)))
	}
	in := captureSortInput{Note: "qa: <a placeholder> & note", Source: assistantwire.CaptureSourceWatch,
		Spaces: []captureSpace{{Slug: "qa-plain", Title: "QA plain"}}}
	a, err := sortCaptureWith(ctx, m, in)
	if err != nil || !slices.Equal(a.Categories, []string{categoryFinance}) {
		t.Fatalf("answer %+v, %v", a, err)
	}
	golden(t, "capture_sort", m.system[0])
	if m.system[0] != prompts.RenderCaptureSort() {
		t.Error("the system prompt is not the template")
	}
	if want := `{"note":"qa: <a placeholder> & note","source":"watch","spaces":[{"slug":"qa-plain","title":"QA plain"}]}`; m.user[0] != want {
		t.Errorf("user message = %s, want %s", m.user[0], want)
	}
	targets := []trackEntry{{Slug: "qa-plain"}, {Slug: "qa-finance"}}
	got := captureSortOf(a, targets, "")
	if got.Track != "qa-plain" || got.Confidence != 1 || got.Action != "keep" || got.Title != "A placeholder title" || !got.Deep || !got.AutoFile {
		t.Errorf("sorted = %+v", got)
	}
	unsure := "qa-plain"
	if got := captureSortOf(captureSortAnswer{Space: &unsure, Confidence: 0.79, Action: "research"}, targets, ""); got.AutoFile || got.Track != "qa-plain" || got.Action != "research" {
		t.Errorf("an unsure answer = %+v: sorted, left for the user", got)
	}
	other := "qa-elsewhere"
	if got := captureSortOf(captureSortAnswer{Space: &other, Confidence: 0.99}, targets, ""); got.Track != "" || got.AutoFile {
		t.Errorf("a track that takes no captures = %+v", got)
	}
	if got := captureSortOf(captureSortAnswer{Confidence: 0.1}, targets, "qa-finance"); got.Track != "qa-finance" || !got.AutoFile {
		t.Errorf("the user's choice = %+v: filed there", got)
	}
}

// The briefing's quick-capture section, as the model is given it (#13502).
func TestGoldenBriefFactsCaptures(t *testing.T) {
	lines := []string{"2 条待归类", "示例标题 → qa-plain", "1 条随手记"}
	golden(t, "routine_facts.captures", prompts.RenderBriefFacts(prompts.BriefFacts{Hours: 12, Captures: lines}))
	golden(t, "routine_facts.captures_card", prompts.RenderBriefFacts(prompts.BriefFacts{Card: true, Hours: 12, Captures: lines}))
}
