package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/extwire"
	"github.com/nwverse/persona/server/internal/nodeproto"
	"github.com/nwverse/persona/server/internal/proto"
)

// An extension's token never sees the health space's conversations or a private card (extension_private.go), whatever
// it was granted: not in the lists, their totals and counts, and not read by id.
func TestExtensionTokensNeverSeeTheHealthSpace(t *testing.T) {
	h, url, _, b := reachHub(t)
	h.takeFleetSessions(assistantwire.FleetSessions{MachineID: "mb", Sessions: []proto.Session{
		{MachineID: "mb", SessionID: "b1", Title: "B-SESSION", Cwd: "/work/project"},
		{MachineID: "mb", SessionID: "bh", Title: "HEALTH-SESSION", Cwd: "/home/me/.persona/tracks/qa-private", Assistant: true, Space: "qa-private"},
		{MachineID: "mb", SessionID: "bd", Title: "HEALTH-DIR", Cwd: "/home/me/.persona/tracks/qa-private/notes", Assistant: true},
		{MachineID: "mb", SessionID: "bm", Title: "HEALTH-MOVED", Cwd: "/work/project", Space: "qa-private"},
	}})
	eventually(t, 5*time.Second, "mb's sessions", func() bool { _, ok := h.localMachine("mb").session("bd"); return ok })
	cards := []proto.Interaction{
		{ID: "ix_ok", MachineID: "mb", SessionID: "b1", Cwd: "/work/project", Kind: proto.IxQuestion, Summary: "OK-CARD"},
		{ID: "ix_private", MachineID: "mb", SessionID: "b1", Cwd: "/work/project", Kind: proto.IxQuestion, Private: true, Summary: "PRIVATE-CARD"},
		{ID: "ix_health", MachineID: "mb", SessionID: "bh", Kind: proto.IxQuestion, Summary: "HEALTH-CARD"},
		{ID: "ix_health_cwd", MachineID: "mb", SessionID: "b1", Cwd: "/home/me/.persona/tracks/qa-private", Kind: proto.IxQuestion, Summary: "HEALTH-CWD-CARD"},
	}
	for _, ix := range cards {
		if err := b.peer.Notify(assistantwire.EventFleetInteraction, assistantwire.FleetInteraction{Interaction: ix}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, 5*time.Second, "the cards", func() bool { return h.interactionOwner("ix_health_cwd") != "" })

	for _, path := range []string{"/api/v1/sessions", "/api/v1/sessions?machine_id=mb", "/api/v1/sessions?limit=50",
		"/api/v1/sessions?limit=50&summary=1", "/api/v1/sessions?counts=1&machine_id=mb", "/api/v1/sessions?limit=50&with_counts=1&machine_id=mb"} {
		code, body := extDo(t, url, http.MethodGet, path, "", nil)
		if code != http.StatusOK || strings.Contains(body, "HEALTH") || strings.Contains(body, `"bh"`) || strings.Contains(body, `"bd"`) || strings.Contains(body, `"bm"`) {
			t.Fatalf("%s: %d %s", path, code, body)
		}
		if !strings.Contains(path, "counts=1&") && !strings.Contains(body, "B-SESSION") {
			t.Fatalf("%s: the session it may see is missing: %s", path, body)
		}
	}
	// Totals and counts leave them out too.
	_, body := extDo(t, url, http.MethodGet, "/api/v1/sessions?counts=1&machine_id=mb", "", nil)
	var counts struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal([]byte(body), &counts); err != nil || counts.Total != 1 {
		t.Fatalf("counts %s", body)
	}

	for _, sid := range []string{"bh", "bd", "bm", "never-listed"} {
		for _, path := range []string{"/items", "/items?fold_work=1", "/items/i1/full", "/items/i1/image", "/items/i1/visualization", "/info", "/screen"} {
			if code, body := extDo(t, url, http.MethodGet, "/api/v1/sessions/mb/"+sid+path, "", nil); code != http.StatusNotFound {
				t.Errorf("%s%s: %d %s", sid, path, code, body)
			}
		}
	}
	if code, body := extDo(t, url, http.MethodGet, "/api/v1/sessions/mb/b1/info", "", nil); code != http.StatusOK {
		t.Fatalf("b1 info: %d %s", code, body)
	}

	code, body := extDo(t, url, http.MethodGet, "/api/v1/interactions", "", nil)
	if code != http.StatusOK || !strings.Contains(body, "OK-CARD") || strings.Contains(body, "PRIVATE") || strings.Contains(body, "HEALTH") {
		t.Fatalf("pending cards: %d %s", code, body)
	}
}

// The re-check of #10941: the extensions' agent.needs_you and agent.finished, which need no scope, are not sent of a
// card or a session they are not shown (extension_private.go): a card of the health space by its directory or its
// conversation's, a private card, a card or a session the hub does not list, a session of the space.
func TestExtensionsAreNotToldWhatTheyAreNotShown(t *testing.T) {
	h, a, _ := pathsHub(t, quiet, quiet)
	heardA := heard(a, extwire.EventEmit)
	health := testSpaceDir("/home/me")
	h.takeFleetSessions(assistantwire.FleetSessions{MachineID: "ma", Sessions: []proto.Session{{MachineID: "ma", SessionID: "s3", Title: "A's work"},
		{MachineID: "ma", SessionID: "sh", Title: "HEALTH-CONVERSATION", Cwd: health, Assistant: true, Space: "qa-private"}}})
	eventually(t, 5*time.Second, "ma's sessions", func() bool { _, ok := h.localMachine("ma").session("sh"); return ok })
	cards := []proto.Interaction{
		{ID: "ix_health_cwd", MachineID: "ma", SessionID: "s3", Cwd: health, Kind: proto.IxQuestion, Summary: "HEALTH-CWD-CARD"},
		{ID: "ix_health", MachineID: "ma", SessionID: "sh", Kind: proto.IxQuestion, Summary: "HEALTH-CARD"},
		{ID: "ix_private", MachineID: "ma", SessionID: "s3", Cwd: "/work", Kind: proto.IxQuestion, Private: true, Summary: "PRIVATE-CARD"},
		{ID: "ix_unknown", MachineID: "ma", SessionID: "never-listed", Kind: proto.IxQuestion, Summary: "UNKNOWN-CARD"},
		{ID: "ix_ok", MachineID: "ma", SessionID: "s3", Cwd: "/work", Kind: proto.IxQuestion, Summary: "OK-CARD"},
	}
	for _, ix := range cards {
		if err := a.peer.Notify(assistantwire.EventFleetInteraction, assistantwire.FleetInteraction{Interaction: ix}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, 5*time.Second, "the cards", func() bool { return h.interactionOwner("ix_ok") != "" })
	push := func(n assistantwire.Notification) {
		t.Helper()
		if err := a.call(t, assistantwire.MethodNotify, assistantwire.NotifyRequest{Notification: n}, nil); err != nil {
			t.Fatalf("%s: %v", n.ID, err)
		}
	}
	// What they are not told first: the bus keeps the order, so once the last one is heard the others were not sent.
	for i, ix := range cards {
		push(assistantwire.Notification{ID: "card" + strconv.Itoa(i), Kind: assistantwire.NotifyInput, Interaction: &proto.Interaction{ID: ix.ID}})
	}
	push(assistantwire.Notification{ID: "done_health", Kind: assistantwire.NotifyDone, DeviceID: "ma", SessionID: "sh", Title: "HEALTH-TITLE"})
	push(assistantwire.Notification{ID: "done_unknown", Kind: assistantwire.NotifyDone, DeviceID: "ma", SessionID: "never-listed", Title: "UNKNOWN-TITLE"})
	push(assistantwire.Notification{ID: "push_none", Kind: assistantwire.NotifyPush, DeviceID: "ma", Title: "NO-SESSION"})
	push(assistantwire.Notification{ID: "done_ok", Kind: assistantwire.NotifyDone, DeviceID: "ma", SessionID: "s3", Title: "OK-TITLE"})
	eventually(t, 5*time.Second, "the events they are shown", func() bool {
		got := heardA()
		return strings.Contains(got, "OK-CARD") && strings.Contains(got, "NO-SESSION") && strings.Contains(got, "OK-TITLE")
	})
	got := heardA()
	for _, w := range []string{"HEALTH", "PRIVATE", "UNKNOWN", "ix_health", "ix_private", "ix_unknown", `"sh"`, "never-listed"} {
		if strings.Contains(got, w) {
			t.Errorf("the extensions are told %q:\n%s", w, got)
		}
	}
}

// The regression check of #10941, a forged session: an extension's token resumed any id it named, and a computer with
// no transcript of it started a new conversation under that id in the folder the extension chose, which the manager
// took for the health chat the id names (its to-dos, feed and held outcome lines shown). On a computer whose node does
// not check such a resume (no resume_outside_health), it resumes only a conversation its computer lists outside the
// space, as a scoped token does; nothing of the other two reaches the computer.
func TestAnExtensionResumesOnlyAListedChatOutsideTheHealthSpace(t *testing.T) {
	h, url, _, b := reachHub(t)
	// A node from before resume_outside_health: an id the hub does not list never reaches it. (With no word of the node
	// at all the hub answers that id 503, retryable: TestAnExtensionsResumeBeforeTheComputersListIsRetryable.)
	b.report(t, devicewire.State{Machine: proto.Machine{ID: "mb", Name: "mb"},
		Info: proto.NodeInfo{Version: "test", Protocol: nodeproto.Version, Capabilities: []string{nodeproto.Sessions}}})
	eventually(t, 5*time.Second, "mb's node", func() bool { return h.localMachine("mb").nodeReported() })
	h.takeFleetSessions(assistantwire.FleetSessions{MachineID: "mb", Sessions: []proto.Session{
		{MachineID: "mb", SessionID: "b1", Title: "B-SESSION", Cwd: "/work/project"},
		{MachineID: "mb", SessionID: "bh", Title: "HEALTH-SESSION", Cwd: "/home/me/.persona/tracks/qa-private", Assistant: true, Space: "qa-private"},
	}})
	eventually(t, 5*time.Second, "mb's sessions", func() bool { _, ok := h.localMachine("mb").session("bh"); return ok })
	starts := func(id string) int {
		n := 0
		for _, l := range reached(b) {
			if strings.Contains(l, `"method":"session.start"`) && strings.Contains(l, `"resume_session_id":"`+id+`"`) {
				n++
			}
		}
		return n
	}
	for _, id := range []string{"6f1d2c3b-4a5e-4f60-8a7b-9c0d1e2f3a4b", "bh"} {
		code, body := extDo(t, url, http.MethodPost, "/api/v1/sessions",
			`{"machine_id":"mb","launcher":"claude","cwd":"/tmp","resume_session_id":"`+id+`","prompt":"hi"}`, nil)
		if code != http.StatusNotFound {
			t.Errorf("resume of %s: %d %s", id, code, body)
		}
		if n := starts(id); n != 0 {
			t.Errorf("resume of %s reached the computer %d times", id, n)
		}
	}
	if code, body := extDo(t, url, http.MethodPost, "/api/v1/sessions",
		`{"machine_id":"mb","launcher":"claude","cwd":"/work/project","resume_session_id":"b1","prompt":"go on"}`, nil); code != http.StatusOK {
		t.Fatalf("resume of its own chat: %d %s", code, body)
	}
	if starts("b1") != 1 {
		t.Fatalf("the resume of b1 did not reach the computer: %v", reached(b))
	}
}

// Exercise the registered HTTP handlers with in-memory credentials and a fake node that records queue calls.
func TestRestrictedSessionQueueRoutes(t *testing.T) {
	h := newTestHub(t)
	h.store = &tokenStore{rows: map[string]db.DeviceToken{}}
	node := newAssistantNode(t, h)
	dirs := withAgentDirs(t, h)
	dirs.rules[node.m.cfg.ID] = []proto.HiddenAgentDir{{Pattern: "/work/hidden", Enabled: true, Action: proto.AgentDirHide}}
	node.m.setSessions([]proto.Session{
		{SessionID: "ordinary", Cwd: "/work/project"},
		{SessionID: "private-dir", Cwd: "/home/me/.persona/tracks/qa-private/reports"},
		{SessionID: "private-marker", Cwd: "/work/project", Space: "qa-private"},
		{SessionID: "hidden", Cwd: "/work/hidden"},
	})
	h.historyReads.remember(node.m.cfg.ID, "history-only", time.Now())
	extension := extTokensOf(t, h).issue("example-extension", "Example Extension", []string{extwire.ScopeAgentsControl})
	eventually(t, 5*time.Second, "the hidden directory rule reached the hub", func() bool {
		row, ok := node.m.session("hidden")
		return ok && row.HiddenByDirectory()
	})
	routes := h.routes()
	code, out := call(t, routes, http.MethodPost, "/api/v1/auth/tokens", h.cfg.Token, `{"kind":"other","scopes":["agents.control"]}`)
	scoped, _ := out["token"].(string)
	if code != http.StatusOK || out["role"] != roleScoped || scoped == "" {
		t.Fatalf("scoped fixture issuance: status %d, role %v", code, out["role"])
	}

	for _, credential := range []struct{ name, token string }{{"extension", extension}, {"scoped", scoped}} {
		for _, route := range []struct{ suffix, method, body string }{
			{"queue", "session.queue.edit", `{"keep":[0]}`},
			{"queue/send-now", "session.queue.send_now", `{}`},
		} {
			t.Run(credential.name+"/"+route.suffix, func(t *testing.T) {
				path := func(id string) string {
					return "/api/v1/sessions/" + node.m.cfg.ID + "/" + id + "/" + route.suffix
				}
				for _, id := range []string{"private-dir", "private-marker", "hidden", "history-only", "absent"} {
					before := node.sent()
					code, out := call(t, routes, http.MethodPost, path(id), credential.token, route.body)
					if code != http.StatusNotFound || node.sent() != before {
						t.Errorf("%s: status %d, %d node calls, response %v", id, code, node.sent()-before, out)
					}
				}
				before := node.sent()
				code, out := call(t, routes, http.MethodPost, path("ordinary"), credential.token, route.body)
				if code != http.StatusOK || node.sent() != before+1 {
					t.Fatalf("ordinary: status %d, %d node calls, response %v", code, node.sent()-before, out)
				}
				params, _ := node.lastSend(t)[route.method].(map[string]any)
				if params["session_id"] != "ordinary" {
					t.Errorf("expected %s for ordinary, got %v", route.method, node.lastSend(t))
				}
			})
		}
	}
}

// The action routes refuse what the read routes hide (#12216): an extension's token cannot resolve or choose on a
// pending card it is never shown (a health-space card, one whose directory is in the space, or a private card). The
// answer is the same 404 a nonexistent card gets and nothing is forwarded to the manager; a card it is shown still
// goes through.
func TestExtensionCardActionsRefuseHiddenPendingCards(t *testing.T) {
	h, url, _, b := reachHub(t)
	health := testSpaceDir("/home/me")
	h.takeFleetSessions(assistantwire.FleetSessions{MachineID: "mb", Sessions: []proto.Session{
		{MachineID: "mb", SessionID: "b1", Title: "B-SESSION", Cwd: "/work/project"},
		{MachineID: "mb", SessionID: "bh", Title: "HEALTH-SESSION", Cwd: health, Assistant: true, Space: "qa-private"},
	}})
	eventually(t, 5*time.Second, "mb's sessions", func() bool { _, ok := h.localMachine("mb").session("bh"); return ok })
	cards := []proto.Interaction{
		{ID: "ix_ok", MachineID: "mb", SessionID: "b1", Cwd: "/work/project", Kind: proto.IxQuestion, Summary: "OK-CARD"},
		{ID: "ix_health", MachineID: "mb", SessionID: "bh", Kind: proto.IxQuestion, Summary: "HEALTH-CARD"},
		{ID: "ix_health_cwd", MachineID: "mb", SessionID: "b1", Cwd: health, Kind: proto.IxQuestion, Summary: "HEALTH-CWD-CARD"},
		{ID: "ix_private", MachineID: "mb", SessionID: "b1", Cwd: "/work/project", Kind: proto.IxQuestion, Private: true, Summary: "PRIVATE-CARD"},
	}
	for _, ix := range cards {
		if err := b.peer.Notify(assistantwire.EventFleetInteraction, assistantwire.FleetInteraction{Interaction: ix}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, 5*time.Second, "the cards", func() bool { return h.interactionOwner("ix_private") != "" })

	hidden := []string{"ix_health", "ix_health_cwd", "ix_private"}
	for _, id := range hidden {
		if code, body := extDo(t, url, http.MethodPost, "/api/v1/interactions/"+id+"/resolve", `{"cancel":true}`, nil); code != http.StatusNotFound {
			t.Errorf("resolve %s: %d %s, want 404", id, code, body)
		}
		if code, body := extDo(t, url, http.MethodPost, "/api/v1/interactions/"+id+"/choose", `{"options":[0],"rev":"r1"}`, nil); code != http.StatusNotFound {
			t.Errorf("choose %s: %d %s, want 404", id, code, body)
		}
	}
	// The route-scope check (extensions.authorize) names the path, but the action must never be forwarded to the
	// manager: no http.request carrying a hidden card's id leaves the hub.
	for _, line := range strings.Split(b.log(), "\n") {
		if strings.HasPrefix(line, assistantwire.MethodHTTP+" ") && (strings.Contains(line, "ix_health") || strings.Contains(line, "ix_private")) {
			t.Fatalf("a hidden card action reached the manager: %s", line)
		}
	}
	// A card the extension is shown is not refused by the new guard: the resolve is forwarded (an http.request carrying
	// its id), not answered 404.
	if code, body := extDo(t, url, http.MethodPost, "/api/v1/interactions/ix_ok/resolve", `{"cancel":true}`, nil); code == http.StatusNotFound {
		t.Fatalf("a visible card's resolve was refused as hidden: %d %s", code, body)
	}
	if !strings.Contains(b.log(), assistantwire.MethodHTTP+" ") || !strings.Contains(b.log(), "ix_ok/resolve") {
		t.Fatalf("a visible card's resolve was not forwarded:\n%s", b.log())
	}
}

// The user's own extension (#13246, trust owner in the manager, assistantwire.Profile.ExtensionOwners; such as their
// memory extension) reads the tracks' conversations and cards on the hub's read routes; a private card stays hidden,
// and once the user takes the mark away the extension is shown none of them again.
func TestTheUsersOwnExtensionReadsTheTracks(t *testing.T) {
	h, url, _, b := reachHub(t)
	h.takeFleetSessions(assistantwire.FleetSessions{MachineID: "mb", Sessions: []proto.Session{
		{MachineID: "mb", SessionID: "b1", Title: "B-SESSION", Cwd: "/work/project"},
		{MachineID: "mb", SessionID: "bh", Title: "HEALTH-SESSION", Cwd: "/home/me/.persona/tracks/qa-private", Assistant: true, Space: "qa-private"},
	}})
	eventually(t, 5*time.Second, "mb's sessions", func() bool { _, ok := h.localMachine("mb").session("bh"); return ok })
	for _, ix := range []proto.Interaction{
		{ID: "ix_private", MachineID: "mb", SessionID: "b1", Cwd: "/work/project", Kind: proto.IxQuestion, Private: true, Summary: "PRIVATE-CARD"},
		{ID: "ix_health", MachineID: "mb", SessionID: "bh", Kind: proto.IxQuestion, Summary: "HEALTH-CARD"},
	} {
		if err := b.peer.Notify(assistantwire.EventFleetInteraction, assistantwire.FleetInteraction{Interaction: ix}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, 5*time.Second, "the cards", func() bool { return h.interactionOwner("ix_health") != "" })
	owners := func(ids ...string) {
		a := &h.assistant
		a.mu.Lock()
		a.profile = &assistantwire.Profile{MachineID: "mb", ExtensionOwners: ids}
		a.mu.Unlock()
	}

	owners("bx")
	if code, body := extDo(t, url, http.MethodGet, "/api/v1/sessions?machine_id=mb", "", nil); code != http.StatusOK || !strings.Contains(body, "HEALTH-SESSION") {
		t.Fatalf("the user's own extension, list: %d %s", code, body)
	}
	if code, body := extDo(t, url, http.MethodGet, "/api/v1/sessions/mb/bh/info", "", nil); code == http.StatusNotFound {
		t.Fatalf("the user's own extension, read: %d %s", code, body)
	}
	if code, body := extDo(t, url, http.MethodGet, "/api/v1/interactions", "", nil); code != http.StatusOK || !strings.Contains(body, "HEALTH-CARD") ||
		strings.Contains(body, "PRIVATE") {
		t.Fatalf("the user's own extension, cards: %d %s", code, body)
	}

	owners("someone-else")
	if code, body := extDo(t, url, http.MethodGet, "/api/v1/sessions?machine_id=mb", "", nil); code != http.StatusOK || strings.Contains(body, "HEALTH") {
		t.Fatalf("a third party's, list: %d %s", code, body)
	}
	if code, body := extDo(t, url, http.MethodGet, "/api/v1/sessions/mb/bh/info", "", nil); code != http.StatusNotFound {
		t.Fatalf("a third party's, read: %d %s", code, body)
	}
	if code, body := extDo(t, url, http.MethodGet, "/api/v1/interactions", "", nil); code != http.StatusOK || strings.Contains(body, "HEALTH") {
		t.Fatalf("a third party's, cards: %d %s", code, body)
	}
}
