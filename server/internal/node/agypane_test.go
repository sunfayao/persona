package node

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
)

const (
	agyTestID      = "11111111-2222-4333-8444-555555555555"
	agyOtherTestID = "22222222-3333-4444-8555-666666666666"
	agyTestPane    = "w1:p1"
	agyTestRow     = "agy-w1-p1"
)

func agyTestAgent(pane, cwd, id string) map[string]any {
	a := agentOf(agyAgent, "", proto.StatusIdle, pane, "w1:t1", "w1", cwd, "agy")
	if id != "" {
		a["agent_session"] = map[string]any{"kind": "id", "value": id, "source": "herdr:antigravity_cli", "agent": agyAgent}
	}
	return a
}

func agyTestFile(t *testing.T, root, id, contents string) string {
	t.Helper()
	file := filepath.Join(root, "brain", id, agyTranscriptPath)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	agyTestWrite(t, file, contents)
	return file
}

func agyTestWrite(t *testing.T, file, contents string) {
	t.Helper()
	if err := os.WriteFile(file, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func agyTestAppend(t *testing.T, file, contents string) {
	t.Helper()
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(contents); err != nil {
		t.Fatal(err)
	}
}

func agyTestCall(t *testing.T, n *Node, method, id string, params map[string]any) any {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	params["session_id"] = id
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	out, err := n.handle(i18n.With(context.Background(), i18n.En), method, raw)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return out
}

func agyTestItems(t *testing.T, n *Node, id string, params map[string]any) ([]proto.Item, map[string]any) {
	t.Helper()
	page := agyTestCall(t, n, "session.items", id, params).(map[string]any)
	return page["items"].([]proto.Item), page
}

func TestAgyPaneUserImagesAndCheckpoints(t *testing.T) {
	node, herdr, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	seedAgents(t, node, herdr, agyTestAgent(agyTestPane, home, agyTestID))
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jB9sAAAAASUVORK5CYII="
	agyTestFile(t, node.agyDataRoot(), agyTestID, fmt.Sprintf(`{"step_index":0,"type":"USER_INPUT","content":"Look\n<attached_images>\nuser.png\n</attached_images>","media":[{"mime_type":"image/png","uri":"./user.png"},{"mime_type":"image/png","data":%q}]}
{"step_index":1,"type":"PLANNER_RESPONSE","content":"Done"}
{"step_index":2,"type":"USER_INPUT","client_msg_id":"photo-only","media":[{"mime_type":"image/png","data":%q},{"mime_type":"image/png","data":%q}]}
{"step_index":3,"type":"CHECKPOINT"}
`, png, png, png))
	index := agyTestCall(t, node, "session.items", agyTestRow, map[string]any{"user_inputs": true}).(proto.UserInputIndex)
	if index.Total != 2 || index.Inputs[0].Seq != 1 || index.Inputs[1].Seq != 3 {
		t.Fatalf("user inputs: %+v", index)
	}
	recent, page := agyTestItems(t, node, agyTestRow, map[string]any{"turns": 1})
	if page["has_more"] != true || len(recent) != 3 || index.Inputs[1].ID != recent[0].ID {
		t.Fatalf("latest turn: %+v", page)
	}
	for _, image := range recent[:2] {
		if image.Role != "user" || image.Kind != proto.KindImage || image.ToolUseID != "" || image.ClientMsgID != "photo-only" ||
			image.Image == nil || !image.Image.Lazy || image.Image.Data != "" {
			t.Fatalf("user image: %+v", image)
		}
		fetched := agyTestCall(t, node, "session.image", agyTestRow, map[string]any{"item_id": image.ID}).(*proto.Image)
		if fetched.Mime != "image/png" || fetched.Data != png {
			t.Fatalf("fetched image: %+v", fetched)
		}
	}
	if notice := recent[2]; notice.Role != "system" || notice.Kind != proto.KindNotice || notice.Text != transcript.CompactedNotice {
		t.Fatalf("checkpoint: %+v", notice)
	}
	older, page := agyTestItems(t, node, agyTestRow, map[string]any{"turns": 1, "before_seq": 3})
	if page["has_more"] != false || len(older) != 3 || older[0].Kind != proto.KindText || older[1].Kind != proto.KindImage ||
		older[0].Seq != older[1].Seq || !strings.HasSuffix(older[1].ID, "#media1") {
		t.Fatalf("earlier turn did not suppress the duplicate path: %+v", page)
	}
}

func TestAgyPaneSessionList(t *testing.T) {
	n, herdr, home := herdrNode(t)
	const (
		acpRow     = "agy-" + agyTestID
		otherRow   = "agy-" + agyOtherTestID
		desktopRow = "desktop-agy-" + agyTestID
		secondPane = "w1:p2"
		secondRow  = "agy-w1-p2"
		model      = "gemini-test"
	)
	n.runtimes = newRuntimeRegistry()
	runtime := n.runtimes.add(nil)
	runtime.ready, runtime.hello.Runtime = true, agyAgent
	n.runtimes.setSessions(runtime, []proto.Session{
		{SessionID: acpRow, Agent: agyAgent, Cwd: home, Status: proto.StatusIdle, Title: "ACP conversation", Model: model},
		{SessionID: otherRow, Agent: agyAgent, Cwd: home, Status: proto.StatusIdle, Model: "other-model"},
		{SessionID: desktopRow, Agent: agyAgent, Cwd: home, Status: proto.StatusOffline},
	})
	for _, tc := range []struct {
		name, status, conversation string
		ambiguous                  bool
		wantACP                    bool
	}{
		{name: "live", status: proto.StatusIdle, conversation: agyTestID},
		{name: "unresolved", status: proto.StatusIdle, wantACP: true},
		{name: "ambiguous", status: proto.StatusIdle, conversation: agyTestID, ambiguous: true, wantACP: true},
		{name: "offline", status: proto.StatusOffline, conversation: agyTestID, wantACP: true},
		{name: "interrupted", status: proto.StatusInterrupted, conversation: agyTestID, wantACP: true},
		{name: "closed", wantACP: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := map[string]string{otherRow: "other-model", desktopRow: ""}
			var agents []any
			if tc.status != "" {
				pane := agyTestAgent(agyTestPane, home, tc.conversation)
				pane["agent_status"] = tc.status
				agents = append(agents, pane)
				want[agyTestRow] = ""
			}
			if tc.ambiguous {
				agents = append(agents, agyTestAgent(secondPane, home, tc.conversation))
				want[secondRow] = ""
			}
			if tc.wantACP {
				want[acpRow] = model
			} else {
				want[agyTestRow] = model
			}
			// A pane whose conversation is known is titled after it (agytitle.go), else after its terminal.
			title := "agy"
			if tc.conversation != "" && !tc.ambiguous {
				title = "ACP conversation"
			}
			herdr.reply("session.snapshot", snapshot(nil, nil, agents, nil))
			rows := listSessions(t, n)
			got := make(map[string]string, len(rows))
			for _, row := range rows {
				got[row.SessionID] = row.Model
				if row.SessionID == agyTestRow && (row.PaneID != agyTestPane || row.Host != host.Herdr || row.Status != tc.status || row.Title != title) {
					t.Errorf("pane metadata changed: %+v", row)
				}
			}
			if len(rows) != len(want) || !maps.Equal(got, want) {
				t.Errorf("got %d rows with models %v, want %v", len(rows), got, want)
			}
		})
	}
}

func TestAgyPanePagesAndFullItems(t *testing.T) {
	n, f, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, agyTestID))
	const fixture = `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","created_at":"2026-11-01T00:00:00Z","content":"<USER_REQUEST>\n<persona-context>hidden</persona-context>\nCheck the files\n</USER_REQUEST><ADDITIONAL_METADATA>hidden</ADDITIONAL_METADATA>"}
{"step_index":1,"type":"USER_INPUT","source":"SYSTEM","content":"hidden system input"}
{"step_index":2,"type":"PLANNER_RESPONSE","created_at":"2026-11-01T00:00:01Z","thinking":"  First find the file. ","content":" I will check. ","tool_calls":[{"name":"run_command","args":{"CommandLine":"ls","toolSummary":"List files"}},{"name":"view_file","args":{"AbsolutePath":"sample.txt"}}]}
{"step_index":3,"type":"GENERIC","content":"Created At: 2026-11-01\nOutput:\nsample.txt\n","truncated_fields":["content"]}
{"step_index":4,"type":"GENERIC","status":"ERROR","error":"Read failed"}
{"step_index":5,"type":"PLANNER_RESPONSE","content":%q}
{"step_index":6,"type":"UNKNOWN"}
`
	long := strings.Repeat("result ", 5000)
	agyTestFile(t, n.agyDataRoot(), agyTestID, fmt.Sprintf(fixture, long))
	output := filepath.Join(n.agyDataRoot(), "brain", agyTestID, ".system_generated", "steps", "3", "output.txt")
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		t.Fatal(err)
	}
	agyTestWrite(t, output, "Created At: 2026-11-01\nOutput:\nsample.txt\nsecond.txt\n")
	items, page := agyTestItems(t, n, agyTestRow, map[string]any{"limit": 1})
	if len(items) != 1 || items[0].Seq != 6 || items[0].FullSize != len(long)-1 || page["to_seq"] != 7 || page["has_more"] != true {
		t.Fatalf("latest page: %+v", page)
	}
	full := agyTestCall(t, n, "session.item", agyTestRow, map[string]any{"item_id": items[0].ID}).(proto.Item)
	if full.ID != items[0].ID || full.Text != strings.TrimSpace(long) {
		t.Fatalf("full item %s has %d bytes", full.ID, len(full.Text))
	}
	items, page = agyTestItems(t, n, agyTestRow, map[string]any{"before_seq": 4, "limit": 1})
	if page["has_more"] != true || page["to_seq"] != 3 || len(items) != 4 ||
		items[0].Kind != "thinking" || items[0].Text != "First find the file." || items[1].Text != "I will check." ||
		items[2].ToolName != "Bash" || string(items[2].ToolInput) != `{"command":"ls","description":"List files"}` ||
		items[3].ToolName != "Read" || string(items[3].ToolInput) != `{"file_path":"sample.txt"}` {
		t.Fatalf("planner page: %+v", page)
	}
	results, _ := agyTestItems(t, n, agyTestRow, map[string]any{"from_seq": 4, "through_seq": 5})
	if len(results) != 2 || results[0].Kind != "tool_result" || results[0].Text != "sample.txt\n" ||
		results[0].ToolUseID != items[2].ToolUseID || results[1].ToolUseID != items[3].ToolUseID ||
		results[1].Text != "Read failed" || !results[1].IsError {
		t.Fatalf("tool results: %+v", results)
	}
	expanded := agyTestCall(t, n, "session.item", agyTestRow, map[string]any{"item_id": results[0].ID}).(proto.Item)
	if expanded.ID != results[0].ID || expanded.ToolUseID != items[2].ToolUseID || expanded.Text != "sample.txt\nsecond.txt\n" {
		t.Fatalf("expanded tool result: %+v", expanded)
	}
	items, page = agyTestItems(t, n, agyTestRow, map[string]any{"before_seq": 3})
	if len(items) != 1 || items[0].Text != "Check the files" || items[0].Role != "user" || page["has_more"] != false {
		t.Fatalf("oldest page: %+v", page)
	}
	items, page = agyTestItems(t, n, agyTestRow, map[string]any{"after_seq": 5})
	if len(items) != 1 || items[0].ID != full.ID || page["from_seq"] != 5 || page["to_seq"] != 7 {
		t.Fatalf("catch-up page: %+v", page)
	}
}

func TestAgyPaneConversationIsolation(t *testing.T) {
	n, f, home := herdrNode(t)
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", root)
	const user = `{"step_index":0,"type":"USER_INPUT","content":%q}` + "\n"
	agyTestFile(t, root, agyTestID, fmt.Sprintf(user, "first conversation"))
	agyTestFile(t, root, agyOtherTestID, fmt.Sprintf(user, "second conversation"))
	first := agyTestAgent(agyTestPane, home, agyTestID)
	const pane2, row2 = "w1:p2", "agy-w1-p2"
	valid := agyTestAgent(pane2, home, agyOtherTestID)
	seedAgents(t, n, f, first, valid)
	items, _ := agyTestItems(t, n, agyTestRow, nil)
	other, _ := agyTestItems(t, n, row2, nil)
	if len(items) != 1 || len(other) != 1 || items[0].Text != "first conversation" || other[0].Text != "second conversation" || items[0].ID == other[0].ID {
		t.Fatalf("same-directory panes: first %+v, second %+v", items, other)
	}
	assertMissingItem := func(id, itemID string) {
		t.Helper()
		if _, err := request(t, n, "session.item", map[string]any{"session_id": id, "item_id": itemID}); err == nil {
			t.Fatal("expanded an item outside the verified conversation")
		}
	}
	assertMissingItem(row2, items[0].ID)
	t.Run("snapshot_mismatch", func(t *testing.T) {
		n.mu.Lock()
		row := n.agents[row2]
		stale := *row.AgentSession
		stale.Value = "33333333-4444-4555-8666-777777777777"
		row.AgentSession = &stale
		// Leave the raw snapshot untouched so the merged row disagrees with its pane's report.
		n.agents[row2] = row
		n.mu.Unlock()
		got, _ := agyTestItems(t, n, row2, nil)
		if len(got) != 1 || got[0].Kind != "notice" {
			t.Fatalf("snapshot mismatch: %+v", got)
		}
		assertMissingItem(row2, items[0].ID)
		assertMissingItem(row2, other[0].ID)
	})
	for _, tc := range []struct{ name, kind, source, agent, id string }{
		{name: "missing"},
		{"path kind", "path", "herdr:antigravity_cli", "agy", agyOtherTestID},
		{"traversal", "id", "herdr:antigravity_cli", "agy", "../" + agyTestID},
		{"invalid UUID", "id", "herdr:antigravity_cli", "agy", "11111111"},
		{"other agent", "id", "herdr:antigravity_cli", "cursor", agyOtherTestID},
		{"other source", "id", "custom:persona", "agy", agyOtherTestID},
		{"duplicate ID", "id", "herdr:antigravity_cli", "agy", agyTestID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			second := agyTestAgent(pane2, home, "")
			if tc.id != "" {
				second["agent_session"] = map[string]any{"kind": tc.kind, "agent": tc.agent, "source": tc.source, "value": tc.id}
			}
			seedAgents(t, n, f, first, second)
			got, _ := agyTestItems(t, n, row2, nil)
			if len(got) != 1 || got[0].Kind != "notice" || got[0].TS.IsZero() || !strings.Contains(got[0].Text, "herdr integration install antigravity-cli") {
				t.Fatalf("terminal notice: %+v", got)
			}
			assertMissingItem(row2, items[0].ID)
		})
	}

	// A conversation directory symlink cannot redirect even to a sibling inside the data root.
	const linked = "33333333-4444-4555-8666-777777777777"
	if err := os.Symlink(agyTestID, filepath.Join(root, "brain", linked)); err != nil {
		t.Fatal(err)
	}
	seedAgents(t, n, f, first, agyTestAgent(pane2, home, linked))
	if got, _ := agyTestItems(t, n, row2, nil); len(got) != 0 {
		t.Fatalf("redirected conversation was read: %+v", got)
	}
}

// A watch from the end of the history, then a step that maps to no item, then a visible one: the empty step moves the
// cursor once, and the visible one comes as its own batch, nothing sent twice (review on #10364).
func TestAgyPaneWatchFromTheEndPastAnInvisibleStep(t *testing.T) {
	n, f, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, agyTestID))
	file := agyTestFile(t, n.agyDataRoot(), agyTestID, `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"Check the files"}`+"\n"+
		`{"step_index":1,"type":"PLANNER_RESPONSE","content":"Looking"}`+"\n")
	_, page := agyTestItems(t, n, agyTestRow, nil)
	hub := withHub(t, n)
	agyTestCall(t, n, "session.watch", agyTestRow, map[string]any{"since_seq": page["to_seq"]})
	t.Cleanup(func() { n.unwatch(agyTestRow) })
	n.mu.Lock()
	w := n.watches[agyTestRow]
	n.mu.Unlock()

	agyTestAppend(t, file, `{"step_index":2,"type":"USER_INPUT","source":"SYSTEM","content":"hidden"}`+"\n")
	for range 3 {
		n.pollAgyPane(w)
	}
	waitFor(t, "the invisible step's batch reaches the hub", func() bool { return len(hub.spans()) >= 1 })
	if got := hub.spans(); len(got) != 1 || got[0].n != 0 || got[0].fromSeq != 2 || got[0].toSeq != 3 {
		t.Fatalf("after the invisible step: %+v", got)
	}
	agyTestAppend(t, file, `{"step_index":3,"type":"PLANNER_RESPONSE","content":"Visible"}`+"\n")
	waitFor(t, "the visible step", func() bool { return len(hub.spans()) >= 2 })
	if got := hub.spans(); len(got) != 2 || got[1].n != 1 || got[1].fromSeq != 3 || got[1].toSeq != 4 || hub.got()[0].Text != "Visible" {
		t.Fatalf("after the visible step: %+v, items %+v", got, hub.got())
	}
}

func TestAgyPaneReplaysOnlyTheMatchingObservedWake(t *testing.T) {
	n, f, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, agyTestID))
	const fixtures = "../../../integrations/antigravity/test/fixtures/"
	raw, err := os.ReadFile(fixtures + "autonomous-wake.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(raw), "\n")
	background := strings.Replace(lines[5], `"step_index":5`, `"step_index":3`, 1)
	file := agyTestFile(t, n.agyDataRoot(), agyTestID, strings.Join(lines[:3], "")+background)
	prior, page := agyTestItems(t, n, agyTestRow, nil)
	if len(prior) != 3 || prior[2].Wake {
		t.Fatalf("ordinary adjacent replies: %+v", prior)
	}
	hub := withHub(t, n)
	agyTestCall(t, n, "session.watch", agyTestRow, map[string]any{"since_seq": page["to_seq"]})
	t.Cleanup(func() { n.unwatch(agyTestRow) })

	// This is the file the registered companion produces in the markerless ACP test.
	receipt, err := os.ReadFile(fixtures + "observed-wake.txt")
	if err != nil {
		t.Fatal(err)
	}
	agyTestWrite(t, filepath.Join(n.agyDataRoot(), "brain", agyTestID, agyWakeReceipts), string(receipt))
	waitFor(t, "the same native export gains its observed wake", func() bool { return len(hub.spans()) > 0 })
	rows, matchedPage := agyTestItems(t, n, agyTestRow, nil)
	if len(rows) != 4 || rows[1].ID != prior[1].ID || rows[3].ID != prior[2].ID || !rows[2].Wake ||
		rows[2].Seq != rows[3].Seq || matchedPage["history"] == page["history"] || hub.spans()[0].fromSeq != 0 {
		t.Fatalf("observed wake: %+v, span %+v", rows, hub.spans())
	}
	agyTestAppend(t, file, lines[2]+background)
	other, _ := agyTestItems(t, n, agyTestRow, nil)
	if len(other) != 3 || other[1].ID == prior[1].ID {
		t.Fatalf("receipt crossed a rewind: %+v", other)
	}
}

func assertAgyResumeReport(t *testing.T, f *fakeHerdr, index int, model, effort, conversation string) {
	t.Helper()
	wantID, wantCommand := agyTestRow, "agy --model "+model
	if effort != "" {
		wantCommand += " --effort " + effort
	}
	if conversation != "" {
		wantID = conversation
		wantCommand += " --conversation " + conversation
	}
	waitFor(t, "the agy resume report", func() bool { return len(f.trace("pane.report_agent_session")) > index })
	got := f.trace("pane.report_agent_session")
	if len(got) != index+1 || !strings.Contains(got[index], `"agent_session_id":"`+wantID+`"`) ||
		!strings.Contains(got[index], wantCommand+`"`) || (conversation == "" && strings.Contains(got[index], "--conversation")) {
		t.Fatalf("want report %d with session ID %s and command %q, got %v", index, wantID, wantCommand, got)
	}
}

func TestAgyPaneResumeAndTerminalControls(t *testing.T) {
	n, f, home := herdrNode(t)
	without := agyTestAgent(agyTestPane, home, "")
	seedAgents(t, n, f, without)
	const model = "test-model"
	n.noteWorkAgent(context.Background(), n.hosts.Get(host.Herdr).(*herdrHost), agyTestPane, agyTestRow, home, agyAgent, agyAgent, model, "", "")
	assertAgyResumeReport(t, f, 0, model, "", "")
	f.after(func(c herdrCall) {
		if c.Method == "agent.prompt" {
			f.setScreen(agyTestPane, "first second (alt+m to send immediately)")
		}
	})
	agyTestCall(t, n, "session.send", agyTestRow, map[string]any{"text": "first\nsecond"})
	agyTestCall(t, n, "session.interrupt", agyTestRow, nil)
	assertTrace(t, f.trace("agent.prompt", "pane.send_keys", "pane.send_input"), []string{
		call("agent.prompt", map[string]any{"target": agyTestPane, "text": "first\nsecond"}),
		sendKeys(agyTestPane, "escape"),
	})
	f.reply("session.snapshot", snapshot(nil, nil, []any{agyTestAgent(agyTestPane, home, agyTestID)}, nil))
	listSessions(t, n)
	assertAgyResumeReport(t, f, 1, model, "", agyTestID)
}

func TestAgyPaneResumeBackfillTracksConversation(t *testing.T) {
	n, f, home := herdrNode(t)
	first := agyTestAgent(agyTestPane, home, agyTestID)
	seedAgents(t, n, f, first)
	const model = "test-model"
	const effort = "high"
	n.noteWorkAgent(context.Background(), n.hosts.Get(host.Herdr).(*herdrHost), agyTestPane, agyTestRow, home, agyAgent, agyAgent, model, effort, "")
	assertAgyResumeReport(t, f, 0, model, effort, agyTestID)
	n.mu.Lock()
	kept := n.launches[agyTestPane].AgyConversation
	n.mu.Unlock()
	if kept != agyTestID { // what was reported is what the launch keeps (review on #10364)
		t.Fatalf("launch keeps conversation %q, reported %q", kept, agyTestID)
	}

	second := agyTestAgent(agyTestPane, home, agyOtherTestID)
	f.reply("session.snapshot", snapshot(nil, nil, []any{second}, nil))
	listSessions(t, n)
	assertAgyResumeReport(t, f, 1, model, effort, agyOtherTestID)

	duplicate := agyTestAgent("w1:p2", home, agyOtherTestID)
	f.reply("session.snapshot", snapshot(nil, nil, []any{second, duplicate}, nil))
	listSessions(t, n)
	assertAgyResumeReport(t, f, 2, model, effort, "")
}

func TestAgyPaneResumeRejectsAmbiguousReports(t *testing.T) {
	for _, tc := range []struct{ name, pane, id string }{
		{"conflicting_reports", agyTestPane, agyOtherTestID},
		{"duplicate_id", "w1:p2", agyTestID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, f, home := herdrNode(t)
			seedAgents(t, n, f, agyTestAgent(agyTestPane, home, agyTestID), agyTestAgent(tc.pane, home, tc.id))
			const model = "test-model"
			n.noteWorkAgent(context.Background(), n.hosts.Get(host.Herdr).(*herdrHost), agyTestPane, agyTestRow, home, agyAgent, agyAgent, model, "", "")
			assertAgyResumeReport(t, f, 0, model, "", "")
		})
	}
}

func TestAgyPaneKeepsCursorTerminalBehavior(t *testing.T) {
	n, f, home := herdrNode(t)
	cursor := agentOf(cursorAgent, agyTestID, proto.StatusIdle, agyTestPane, "w1:t1", "w1", home, "cursor")
	seedAgents(t, n, f, cursor)
	const cursorRow = "cursor-w1-p1"
	items, _ := agyTestItems(t, n, cursorRow, nil)
	if len(items) != 1 || items[0].ID != "cursor#0" || items[0].Kind != "notice" || strings.Contains(items[0].Text, "antigravity-cli") {
		t.Fatalf("cursor notice: %+v", items)
	}
	if _, err := request(t, n, "session.item", map[string]any{"session_id": cursorRow, "item_id": items[0].ID}); err == nil {
		t.Fatal("Cursor unexpectedly expanded a transcript item")
	}
	agyTestCall(t, n, "session.watch", cursorRow, nil)
	n.mu.Lock()
	w := n.watches[cursorRow]
	n.mu.Unlock()
	if w != nil {
		t.Fatal("Cursor unexpectedly started a transcript watch")
	}
	agyTestCall(t, n, "session.unwatch", cursorRow, nil)
	agyTestCall(t, n, "session.send", cursorRow, map[string]any{"text": "check the file"})
	agyTestCall(t, n, "session.interrupt", cursorRow, nil)
	assertTrace(t, f.trace("agent.prompt", "pane.send_keys", "pane.send_input"), []string{
		call("agent.prompt", map[string]any{"target": agyTestPane, "text": "check the file"}),
		sendKeys(agyTestPane, "escape"),
	})
	if got := workAgentResume(cursorAgent, "cursor-agent", agyTestID); got != "cursor-agent" {
		t.Fatalf("Cursor resume: %s", got)
	}
}

func TestAgySubagentDetection(t *testing.T) {
	const (
		parentID  = "11111111-2222-4333-8444-555555555555"
		subSys    = "aaaaaaaa-1111-4222-8333-444444444441"
		subNoSrc  = "aaaaaaaa-1111-4222-8333-444444444442"
		subCort   = "aaaaaaaa-1111-4222-8333-444444444443"
		wakeUserS = "aaaaaaaa-1111-4222-8333-444444444444"
		subOther  = "aaaaaaaa-1111-4222-8333-444444444445"
		errSys    = "aaaaaaaa-1111-4222-8333-444444444446"
		normExp   = "bbbbbbbb-1111-4222-8333-444444444441"
		normEmp   = "bbbbbbbb-1111-4222-8333-444444444442"
		fullSub   = "cccccccc-1111-4222-8333-444444444441"
		emptyID   = "dddddddd-1111-4222-8333-444444444441"
		partID    = "dddddddd-1111-4222-8333-444444444442"
		missID    = "dddddddd-1111-4222-8333-444444444443"
		unrID     = "dddddddd-1111-4222-8333-444444444444"
	)
	root := t.TempDir()
	agyTestFile(t, root, subSys, `{"step_index":0,"type":"SYSTEM_MESSAGE","source":"SYSTEM","content":"Subagent prompt"}`+"\n")
	agyTestFile(t, root, subNoSrc, `{"step_index":0,"type":"SYSTEM_MESSAGE","content":"Subagent prompt"}`+"\n")
	agyTestFile(t, root, subCort, `{"step_index":0,"type":"CORTEX_STEP_TYPE_SYSTEM_MESSAGE","source":"SYSTEM","content":"Subagent prompt"}`+"\n")
	agyTestFile(t, root, wakeUserS, `{"step_index":0,"type":"USER_INPUT","source":"SYSTEM","content":"Background wake input"}`+"\n"+
		`{"step_index":1,"type":"PLANNER_RESPONSE","content":"Wake handled"}`+"\n")
	agyTestFile(t, root, subOther, `{"step_index":0,"type":"USER_INPUT","source":"SUBAGENT","content":"Delegated prompt"}`+"\n")
	agyTestFile(t, root, errSys, `{"step_index":0,"type":"ERROR_MESSAGE","source":"SYSTEM","error":"temporary error"}`+"\n")
	agyTestFile(t, root, normExp, `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"User prompt"}`+"\n")
	agyTestFile(t, root, normEmp, `{"step_index":0,"type":"USER_INPUT","content":"Minimal fixture"}`+"\n")
	agyTestFile(t, root, emptyID, "")
	agyTestFile(t, root, partID, `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"still writing"}`)

	// Empty transcript.jsonl falls back to transcript_full.jsonl.
	agyTestFile(t, root, fullSub, "")
	fullPath := filepath.Join(root, "brain", fullSub, agyFullTranscriptPath)
	agyTestWrite(t, fullPath, `{"step_index":0,"type":"SYSTEM_MESSAGE","source":"SYSTEM","content":"Full subagent"}`+"\n")

	for _, tc := range []struct {
		name string
		id   string
		want agyTranscriptKind
	}{
		{"system_message_with_source", subSys, agyTranscriptSubagent},
		{"system_message_without_source", subNoSrc, agyTranscriptSubagent},
		{"cortex_system_message", subCort, agyTranscriptSubagent},
		{"user_input_system_source_is_normal", wakeUserS, agyTranscriptNormal},
		{"error_message_system_source_is_normal", errSys, agyTranscriptNormal},
		{"user_input_subagent_source", subOther, agyTranscriptSubagent},
		{"full_transcript_fallback", fullSub, agyTranscriptSubagent},
		{"normal_explicit_user", normExp, agyTranscriptNormal},
		{"normal_empty_source", normEmp, agyTranscriptNormal},
		{"empty_file", emptyID, agyTranscriptEmpty},
		{"partial_line_without_newline", partID, agyTranscriptEmpty},
		{"missing_transcript", missID, agyTranscriptMissing},
	} {
		t.Run("probe/"+tc.name, func(t *testing.T) {
			if got := probeAgyConversation(root, tc.id); got != tc.want {
				t.Fatalf("probeAgyConversation(%s) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}

	if os.Geteuid() != 0 {
		unrFile := agyTestFile(t, root, unrID, `{"step_index":0,"type":"USER_INPUT","content":"locked"}`+"\n")
		if err := os.Chmod(unrFile, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(unrFile, 0o600) })
		if got := probeAgyConversation(root, unrID); got != agyTranscriptUnreadable {
			t.Fatalf("probeAgyConversation(unreadable) = %v, want %v", got, agyTranscriptUnreadable)
		}
	}

	const (
		refInvoke   = "eeeeeeee-1111-4222-8333-444444444441"
		refOutOrder = "eeeeeeee-1111-4222-8333-444444444442"
		refTrunc    = "eeeeeeee-1111-4222-8333-444444444443"
		refSendMsg  = "eeeeeeee-1111-4222-8333-444444444444"
		refManage   = "eeeeeeee-1111-4222-8333-444444444445"
		mentioned   = "ffffffff-1111-4222-8333-444444444441"
		viewedFile  = "ffffffff-1111-4222-8333-444444444442"
	)
	parentTranscript := strings.Join([]string{
		fmt.Sprintf(`{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"Investigate %s"}`, mentioned),
		`{"step_index":1,"type":"PLANNER_RESPONSE","tool_calls":[{"name":"view_file","args":{"AbsolutePath":"notes.txt"}},{"name":"invoke_subagent","args":{"Prompt":"check"}}]}`,
		fmt.Sprintf(`{"step_index":2,"type":"GENERIC","content":"contains %s"}`, viewedFile),
		fmt.Sprintf(`{"step_index":3,"type":"GENERIC","content":"Subagent started: conversationId=%s"}`, refInvoke),
		// Parallel tool call where call 0 (run_command) has not landed yet and call 1 (invoke_subagent) arrives first at step 6.
		`{"step_index":4,"type":"PLANNER_RESPONSE","tool_calls":[{"name":"run_command","args":{"CommandLine":"sleep 5"}},{"name":"invoke_subagent","args":{"Prompt":"parallel"}}]}`,
		fmt.Sprintf(`{"step_index":6,"type":"GENERIC","content":"conversationId: %s"}`, strings.ToUpper(refOutOrder)),
		// Truncated invoke_subagent result whose UUID lives in step output.txt.
		`{"step_index":7,"type":"PLANNER_RESPONSE","tool_calls":[{"name":"invoke_subagent","args":{"Prompt":"truncated"}}]}`,
		`{"step_index":8,"type":"GENERIC","content":"truncated preview","truncated_fields":["content"]}`,
		// send_message and manage_subagents targeting subagents.
		fmt.Sprintf(`{"step_index":9,"type":"PLANNER_RESPONSE","tool_calls":[{"name":"send_message","args":{"Recipient":%q,"Message":"hi"}},{"name":"manage_subagents","args":{"Action":"kill","ConversationIds":[%q]}}]}`, refSendMsg, refManage),
	}, "\n") + "\n"
	agyTestFile(t, root, parentID, parentTranscript)
	step8Out := filepath.Join(root, "brain", parentID, ".system_generated", "steps", "8", "output.txt")
	if err := os.MkdirAll(filepath.Dir(step8Out), 0o700); err != nil {
		t.Fatal(err)
	}
	agyTestWrite(t, step8Out, "Created At: 2026-11-01\nOutput:\nconversationId: "+refTrunc+"\n")

	// Even if a referenced child transcript starts with USER_INPUT, the parent reference flags it as subagent.
	agyTestFile(t, root, refInvoke, `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"child"}`+"\n")
	agyTestFile(t, root, mentioned, `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"unrelated user conv"}`+"\n")
	agyTestFile(t, root, viewedFile, `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"unrelated viewed conv"}`+"\n")

	var n Node
	for _, tc := range []struct {
		name      string
		candidate string
		want      agyCandidateDecision
	}{
		{"subagent_header", subSys, agyCandidateSubagent},
		{"referenced_by_invoke_subagent", refInvoke, agyCandidateSubagent},
		{"referenced_out_of_order_parallel", refOutOrder, agyCandidateSubagent},
		{"referenced_in_truncated_output", refTrunc, agyCandidateSubagent},
		{"referenced_by_send_message", refSendMsg, agyCandidateSubagent},
		{"referenced_by_manage_subagents", refManage, agyCandidateSubagent},
		{"missing_during_invoke_subagent_race", missID, agyCandidatePending},
		{"empty_in_progress", emptyID, agyCandidatePending},
		{"partial_in_progress", partID, agyCandidatePending},
		{"unrelated_uuid_in_user_input", mentioned, agyCandidateNormal},
		{"unrelated_uuid_in_view_file_result", viewedFile, agyCandidateNormal},
		{"system_sourced_wake_user_input", wakeUserS, agyCandidateNormal},
		{"normal_new_conversation", normExp, agyCandidateNormal},
	} {
		t.Run("classify/"+tc.name, func(t *testing.T) {
			if got := n.classifyAgyCandidate(root, parentID, tc.candidate); got != tc.want {
				t.Fatalf("classifyAgyCandidate(%s) = %v, want %v", tc.candidate, got, tc.want)
			}
		})
	}
	if os.Geteuid() != 0 {
		if got := n.classifyAgyCandidate(root, parentID, unrID); got != agyCandidatePending {
			t.Fatalf("classifyAgyCandidate(unreadable) = %v, want %v", got, agyCandidatePending)
		}
	}
}

func TestAgyPaneWatchIgnoresSubagentConversation(t *testing.T) {
	n, f, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	const (
		parentID   = agyTestID
		subSysID   = "aaaaaaaa-2222-4333-8444-555555555551"
		subRefID   = "aaaaaaaa-2222-4333-8444-555555555552"
		subRaceID  = "aaaaaaaa-2222-4333-8444-555555555553"
		subEmptyID = "aaaaaaaa-2222-4333-8444-555555555554"
	)

	parentFile := agyTestFile(t, n.agyDataRoot(), parentID, ""+
		`{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","created_at":"2026-11-21T10:00:00Z","content":"Investigate issue"}`+"\n"+
		`{"step_index":1,"type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T10:00:01Z","model":"gemini-test","input_tokens":5000,"output_tokens":100,"content":"Spawning subagent.","tool_calls":[{"name":"invoke_subagent","args":{"Prompt":"inspect files"}}]}`+"\n")
	agyTestFile(t, n.agyDataRoot(), subSysID, ""+
		`{"step_index":0,"type":"SYSTEM_MESSAGE","source":"SYSTEM","created_at":"2026-11-21T10:00:02Z","content":"You are a subagent"}`+"\n"+
		`{"step_index":1,"type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T10:00:03Z","model":"gemini-sub","input_tokens":100,"output_tokens":20,"content":"Subagent working"}`+"\n")
	agyTestFile(t, n.agyDataRoot(), subEmptyID, `{"step_index":0,"type":"SYSTEM_MESSAGE"`)

	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, parentID))
	const model = "test-model"
	n.noteWorkAgent(context.Background(), n.hosts.Get(host.Herdr).(*herdrHost), agyTestPane, agyTestRow, home, agyAgent, agyAgent, model, "", "")
	assertAgyResumeReport(t, f, 0, model, "", parentID)

	initialItems, initialPage := agyTestItems(t, n, agyTestRow, nil)
	baseHistory := initialPage["history"].(string)
	if len(initialItems) != 3 || initialItems[0].Text != "Investigate issue" {
		t.Fatalf("initial parent items: %+v", initialItems)
	}

	hub := withHub(t, n)
	agyTestCall(t, n, "session.watch", agyTestRow, map[string]any{"since_seq": initialPage["to_seq"]})
	t.Cleanup(func() { n.unwatch(agyTestRow) })
	n.mu.Lock()
	w := n.watches[agyTestRow]
	n.mu.Unlock()

	// 1. Subagent PreInvocation overwrites herdr's reported conversation with subSysID.
	f.reply("session.snapshot", snapshot(nil, nil, []any{agyTestAgent(agyTestPane, home, subSysID)}, nil))
	rows := listSessions(t, n)
	for range 3 {
		n.pollAgyPane(w)
	}
	if spans := hub.spans(); len(spans) != 0 {
		t.Fatalf("subagent report triggered unexpected watch push/reset: %+v", spans)
	}
	items, page := agyTestItems(t, n, agyTestRow, nil)
	if page["history"] != baseHistory || len(items) != 3 || items[0].Text != "Investigate issue" {
		t.Fatalf("session.items switched away from parent during subagent: page=%+v items=%+v", page, items)
	}
	expanded := agyTestCall(t, n, "session.item", agyTestRow, map[string]any{"item_id": initialItems[0].ID}).(proto.Item)
	if expanded.Text != "Investigate issue" {
		t.Fatalf("session.item failed during subagent: %+v", expanded)
	}
	info := agyTestCall(t, n, "session.info", agyTestRow, nil).(map[string]any)
	if info["context_tokens"] != 5000 || info["output_tokens"] != 100 {
		t.Fatalf("session.info switched to subagent usage: %+v", info)
	}
	// listSessions and resume backfill must keep the parent conversation, not report subSysID.
	for _, row := range rows {
		if row.SessionID == agyTestRow && row.ContextTokens != 5000 {
			t.Fatalf("session list row switched to subagent usage: %+v", row)
		}
	}
	n.mu.Lock()
	keptResume := n.launches[agyTestPane].AgyConversation
	n.mu.Unlock()
	if keptResume != parentID {
		t.Fatalf("launch AgyConversation = %q, want %q", keptResume, parentID)
	}
	if got := f.trace("pane.report_agent_session"); len(got) != 1 {
		t.Fatalf("resume report unexpectedly overwritten during subagent: %v", got)
	}

	// 2. While herdr still reports subSysID, parent appends invoke_subagent result (referencing subRefID)
	// and a follow-up reply: watcher must stream parent's new steps incrementally with NO reset.
	agyTestAppend(t, parentFile, ""+
		fmt.Sprintf(`{"step_index":2,"type":"GENERIC","status":"DONE","created_at":"2026-11-21T10:00:04Z","content":"Launched subagent conversationId=%s"}`, subRefID)+"\n"+
		`{"step_index":3,"type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T10:00:05Z","content":"Parent continuing."}`+"\n")
	n.pollAgyPane(w)
	waitFor(t, "parent incremental update during subagent", func() bool { return len(hub.spans()) >= 1 })
	if spans := hub.spans(); len(spans) != 1 || spans[0].history != baseHistory || spans[0].fromSeq != 2 || spans[0].toSeq != 4 {
		t.Fatalf("parent update while subagent reported caused reset or wrong span: %+v", spans)
	}

	// 3. Subagent referenced by parent invoke_subagent (even if its transcript starts with USER_INPUT),
	// missing-transcript startup race, and partial in-progress transcript must all keep parent bound.
	agyTestFile(t, n.agyDataRoot(), subRefID, `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"child transcript"}`+"\n")
	for _, subID := range []string{subRefID, subRaceID, subEmptyID} {
		seedAgents(t, n, f, agyTestAgent(agyTestPane, home, subID))
		n.pollAgyPane(w)
		if spans := hub.spans(); len(spans) != 1 {
			t.Fatalf("subagent %s triggered watch push/reset: %+v", subID, spans)
		}
		if _, p := agyTestItems(t, n, agyTestRow, nil); p["history"] != baseHistory {
			t.Fatalf("subagent %s changed active history: got %q, want %q", subID, p["history"], baseHistory)
		}
	}

	// 4. Parent PreInvocation reports parentID again: no reset, same history.
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, parentID))
	n.pollAgyPane(w)
	if spans := hub.spans(); len(spans) != 1 {
		t.Fatalf("returning to parent triggered unexpected reset: %+v", spans)
	}
	if w.agy.conversation != parentID || w.agy.history != baseHistory {
		t.Fatalf("watch drifted from parent: conv=%q history=%q", w.agy.conversation, w.agy.history)
	}
}

func TestAgyPaneLegitimateConversationSwitches(t *testing.T) {
	n, f, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	const (
		subID    = "aaaaaaaa-3333-4444-8555-666666666661"
		parentID = agyTestID
		nextID   = agyOtherTestID
	)
	agyTestFile(t, n.agyDataRoot(), subID, `{"step_index":0,"type":"SYSTEM_MESSAGE","source":"SYSTEM","content":"Subagent first"}`+"\n")
	agyTestFile(t, n.agyDataRoot(), parentID, ""+
		`{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"Parent task"}`+"\n"+
		`{"step_index":1,"type":"PLANNER_RESPONSE","tool_calls":[{"name":"invoke_subagent","args":{"Prompt":"help"}}]}`+"\n")

	// 1. First bind must not bind to a subagent conversation.
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, subID))
	items, _ := agyTestItems(t, n, agyTestRow, nil)
	if len(items) != 1 || items[0].Kind != "notice" {
		t.Fatalf("initial subagent report should remain unbound notice, got: %+v", items)
	}

	// 2. First bind to the real parent conversation succeeds.
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, parentID))
	items, page := agyTestItems(t, n, agyTestRow, nil)
	if len(items) != 2 || items[0].Text != "Parent task" {
		t.Fatalf("first bind to parent failed: %+v", items)
	}
	parentHistory := page["history"].(string)

	hub := withHub(t, n)
	agyTestCall(t, n, "session.watch", agyTestRow, map[string]any{"since_seq": page["to_seq"]})
	t.Cleanup(func() { n.unwatch(agyTestRow) })
	n.mu.Lock()
	w := n.watches[agyTestRow]
	n.mu.Unlock()

	// Subagent report while bound is ignored.
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, subID))
	n.pollAgyPane(w)
	if spans := hub.spans(); len(spans) != 0 {
		t.Fatalf("subagent triggered push: %+v", spans)
	}

	// 3. Legitimate user switch (e.g. /clear -> new conversation): held while line 0 is incomplete,
	// then switches cleanly once step 0 USER_INPUT is complete.
	nextFile := agyTestFile(t, n.agyDataRoot(), nextID, `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"Fresh`)
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, nextID))
	n.pollAgyPane(w)
	if spans := hub.spans(); len(spans) != 0 {
		t.Fatalf("incomplete new transcript caused premature reset: %+v", spans)
	}
	if _, p := agyTestItems(t, n, agyTestRow, nil); p["history"] != parentHistory {
		t.Fatalf("incomplete new transcript replaced parent history: %q", p["history"])
	}

	agyTestWrite(t, nextFile, `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"Fresh conversation"}`+"\n")
	waitFor(t, "legitimate switch to new conversation", func() bool { return len(hub.spans()) >= 1 })
	spans := hub.spans()
	got := hub.got()
	wantNextHistory := agyStepsHistory(nextID).token
	if len(spans) != 1 || spans[0].fromSeq != 0 || spans[0].toSeq != 1 || spans[0].history != wantNextHistory || len(got) != 1 || got[0].Text != "Fresh conversation" {
		t.Fatalf("legitimate switch failed: spans=%+v items=%+v", spans, got)
	}

	// 4. Cross-pane ambiguity on the bound parent while reporting a subagent must not keep the ambiguous parent.
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, subID), agyTestAgent("w1:p2", home, nextID))
	items, _ = agyTestItems(t, n, agyTestRow, nil)
	if len(items) != 1 || items[0].Kind != "notice" {
		t.Fatalf("cross-pane ambiguity on bound parent was not cleared: %+v", items)
	}

	// 5. A conversation whose first step is a SYSTEM-sourced wake/background USER_INPUT must not be
	// misclassified as a subagent: both first bind (session.items, session.info, resume) and switching
	// from an already-bound parent conversation must succeed.
	const wakeID = "bbbbbbbb-3333-4444-8555-666666666662"
	agyTestFile(t, n.agyDataRoot(), wakeID, ""+
		`{"step_index":0,"type":"USER_INPUT","source":"SYSTEM","created_at":"2026-11-21T11:00:00Z","content":"Background wake prompt"}`+"\n"+
		`{"step_index":1,"type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T11:00:01Z","model":"gemini-wake","input_tokens":1200,"output_tokens":40,"content":"Handled background wake."}`+"\n")

	// First bind on an unbound pane (pane was cleared by ambiguity above).
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, wakeID))
	items, wakePage := agyTestItems(t, n, agyTestRow, nil)
	if len(items) != 1 || items[0].Text != "Handled background wake." || wakePage["history"] != agyStepsHistory(wakeID).token {
		t.Fatalf("first bind to SYSTEM-sourced USER_INPUT conversation failed: page=%+v items=%+v", wakePage, items)
	}
	info := agyTestCall(t, n, "session.info", agyTestRow, nil).(map[string]any)
	if info["context_tokens"] != 1200 || info["output_tokens"] != 40 {
		t.Fatalf("session.info for SYSTEM-sourced USER_INPUT conversation = %+v", info)
	}
	const resumeModel = "test-model"
	n.noteWorkAgent(context.Background(), n.hosts.Get(host.Herdr).(*herdrHost), agyTestPane, agyTestRow, home, agyAgent, agyAgent, resumeModel, "", "")
	assertAgyResumeReport(t, f, 0, resumeModel, "", wakeID)

	// Switch from a bound parent conversation (which previously invoked a subagent) to wakeID.
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, parentID))
	if _, p := agyTestItems(t, n, agyTestRow, nil); p["history"] != parentHistory {
		t.Fatalf("rebind to parent failed: %+v", p)
	}
	n.pollAgyPane(w)
	waitFor(t, "watch rebounds to parent", func() bool {
		spans := hub.spans()
		return len(spans) > 0 && spans[len(spans)-1].history == parentHistory
	})
	beforeSwitch := len(hub.spans())
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, wakeID))
	n.pollAgyPane(w)
	waitFor(t, "switch from parent to SYSTEM-sourced USER_INPUT conversation", func() bool { return len(hub.spans()) > beforeSwitch })
	lastSpan := hub.spans()[len(hub.spans())-1]
	lastItem := hub.got()[len(hub.got())-1]
	if lastSpan.fromSeq != 0 || lastSpan.history != agyStepsHistory(wakeID).token || lastItem.Text != "Handled background wake." {
		t.Fatalf("switch to SYSTEM-sourced USER_INPUT conversation failed: span=%+v item=%+v", lastSpan, lastItem)
	}
}

func TestAgyPaneUpdatedAtAndOrdering(t *testing.T) {
	n, herdr, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	older := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	fileTime := time.Date(2026, 11, 23, 11, 30, 0, 0, time.UTC)

	const offlineID = "agy-" + agyOtherTestID
	n.runtimes = newRuntimeRegistry()
	rt := n.runtimes.add(nil)
	rt.ready, rt.hello.Runtime = true, agyAgent

	findRow := func(rows []proto.Session, id string) (proto.Session, int) {
		for i, r := range rows {
			if r.SessionID == id {
				return r, i
			}
		}
		t.Fatalf("session %s not found in %d rows", id, len(rows))
		return proto.Session{}, -1
	}
	// The companion's rev counts steps (7 here); the pane's is always its transcript's size.
	companion := proto.Session{
		SessionID: "agy-" + agyTestID,
		Agent:     agyAgent,
		Cwd:       home,
		Status:    proto.StatusIdle,
		Model:     "gemini-test",
		UpdatedAt: newer,
		LastText:  "Ready to ship.",
		Rev:       7,
	}
	offline := proto.Session{SessionID: offlineID, Agent: agyAgent, Cwd: home, Status: proto.StatusOffline, UpdatedAt: older}

	// 1. A live agy herdr pane the companion lists takes its UpdatedAt and LastText and sorts ahead of older rows, but
	// not its Rev: with no transcript on this computer yet, the pane has none.
	n.runtimes.setSessions(rt, []proto.Session{companion, offline})
	herdr.reply("session.snapshot", snapshot(nil, nil, []any{agyTestAgent(agyTestPane, home, agyTestID)}, nil))
	rows := listSessions(t, n)
	paneRow, paneIdx := findRow(rows, agyTestRow)
	_, offlineIdx := findRow(rows, offlineID)
	if !paneRow.UpdatedAt.Equal(newer) || paneRow.LastText != "Ready to ship." || paneRow.Rev != 0 {
		t.Fatalf("companion merge = UpdatedAt=%v LastText=%q Rev=%d, want %v / Ready to ship. / 0", paneRow.UpdatedAt, paneRow.LastText, paneRow.Rev, newer)
	}
	if paneIdx >= offlineIdx {
		t.Fatalf("live agy pane ranked %d behind older offline row %d", paneIdx, offlineIdx)
	}

	// 2. A companion row with no UpdatedAt: the verified local transcript's ModTime, and its size as Rev, not the
	// companion's.
	transcriptFile := agyTestFile(t, n.agyDataRoot(), agyTestID, `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","content":"Check status"}`+"\n")
	if err := os.Chtimes(transcriptFile, fileTime, fileTime); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(transcriptFile)
	if err != nil {
		t.Fatal(err)
	}
	undated := companion
	undated.UpdatedAt, undated.LastText = time.Time{}, ""
	n.runtimes.setSessions(rt, []proto.Session{undated, offline})
	rows = listSessions(t, n)
	paneRow, paneIdx = findRow(rows, agyTestRow)
	_, offlineIdx = findRow(rows, offlineID)
	if !paneRow.UpdatedAt.Equal(fileTime) || paneRow.Rev != st.Size() {
		t.Fatalf("companion fallback = UpdatedAt=%v Rev=%d, want %v / %d", paneRow.UpdatedAt, paneRow.Rev, fileTime, st.Size())
	}
	if paneIdx >= offlineIdx {
		t.Fatalf("fallback agy pane ranked %d behind older offline row %d", paneIdx, offlineIdx)
	}

	// 3. Standalone herdr pane (no companion row) takes UpdatedAt and Rev from its local transcript.
	n.runtimes.setSessions(rt, []proto.Session{offline})
	rows = listSessions(t, n)
	paneRow, paneIdx = findRow(rows, agyTestRow)
	_, offlineIdx = findRow(rows, offlineID)
	if !paneRow.UpdatedAt.Equal(fileTime) || paneRow.Rev != st.Size() {
		t.Fatalf("standalone merge = UpdatedAt=%v Rev=%d, want %v / %d", paneRow.UpdatedAt, paneRow.Rev, fileTime, st.Size())
	}
	if paneIdx >= offlineIdx {
		t.Fatalf("standalone agy pane ranked %d behind older offline row %d", paneIdx, offlineIdx)
	}
	standaloneRev := paneRow.Rev

	// 4. The companion back after the transcript grew (3 → 1): the pane's rev moves on with the transcript and never
	// falls back to the companion's step count.
	agyTestAppend(t, transcriptFile, `{"step_index":1,"type":"PLANNER_RESPONSE","content":"Ready to ship."}`+"\n")
	grown, err := os.Stat(transcriptFile)
	if err != nil {
		t.Fatal(err)
	}
	n.runtimes.setSessions(rt, []proto.Session{companion, offline})
	rows = listSessions(t, n)
	paneRow, _ = findRow(rows, agyTestRow)
	if paneRow.Rev != grown.Size() || paneRow.Rev <= standaloneRev || !paneRow.UpdatedAt.Equal(newer) {
		t.Fatalf("companion back = Rev=%d UpdatedAt=%v, want %d (after %d) / %v", paneRow.Rev, paneRow.UpdatedAt, grown.Size(), standaloneRev, newer)
	}

	// 5. Unresolved pane with no companion row and no transcript keeps zero UpdatedAt and Rev.
	n.runtimes.setSessions(rt, []proto.Session{offline})
	herdr.reply("session.snapshot", snapshot(nil, nil, []any{agyTestAgent(agyTestPane, home, "")}, nil))
	rows = listSessions(t, n)
	paneRow, _ = findRow(rows, agyTestRow)
	if !paneRow.UpdatedAt.IsZero() || paneRow.Rev != 0 {
		t.Fatalf("unresolved pane = UpdatedAt=%v Rev=%d, want zero", paneRow.UpdatedAt, paneRow.Rev)
	}
}

func TestAgyPaneSendSavesImagesAndTagsClientMsgID(t *testing.T) {
	n, f, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	// Start as an unbound fresh agy pane (matching deliverLaunch: POST /sessions then POST /messages).
	seedAgents(t, n, f, agyTestAgent(agyTestPane, home, ""))
	f.reply("agent.prompt", map[string]any{})
	f.reply("session.snapshot", snapshot(nil, nil, []any{agyTestAgent(agyTestPane, home, "")}, nil))
	watchHub := withHub(t, n)

	_, initialPage := agyTestItems(t, n, agyTestRow, nil)
	agyTestCall(t, n, "session.watch", agyTestRow, map[string]any{"since_seq": initialPage["to_seq"]})
	t.Cleanup(func() { n.unwatch(agyTestRow) })

	const png1 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jB9sAAAAASUVORK5CYII="
	const png2 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	var prompted []string
	var transcriptPath string
	nextStep := 0
	currentConv := agyTestID
	appendNativeMedia := false
	f.after(func(c herdrCall) {
		if c.Method != "agent.prompt" {
			return
		}
		text := str(c.Params["text"])
		prompted = append(prompted, text)
		contentJSON, _ := json.Marshal("<USER_REQUEST>\n" + text + "\n</USER_REQUEST>")
		mediaField := `"media":null`
		if appendNativeMedia {
			mediaField = fmt.Sprintf(`"media":[{"mime_type":"image/png","data":%q}]`, png1)
		}
		line := fmt.Sprintf(`{"step_index":%d,"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","created_at":%q,"content":%s,%s}`+"\n",
			nextStep, time.Now().UTC().Format(time.RFC3339Nano), contentJSON, mediaField)
		nextStep++
		if transcriptPath == "" {
			transcriptPath = agyTestFile(t, n.agyDataRoot(), currentConv, line)
			f.reply("session.snapshot", snapshot(nil, nil, []any{agyTestAgent(agyTestPane, home, currentConv)}, nil))
		} else {
			agyTestAppend(t, transcriptPath, line)
		}
	})

	// 1. Initial launch send with text + 2 images + client_msg_id (plus idempotent retry of the same client_msg_id).
	sendParams := map[string]any{
		"text":          "Inspect these two screenshots",
		"client_msg_id": "launch-msg-1",
		"images": []proto.Image{
			{Mime: "image/png", Data: png1},
			{Mime: "image/png", Data: png2},
		},
	}
	agyTestCall(t, n, "session.send", agyTestRow, maps.Clone(sendParams))
	agyTestCall(t, n, "session.send", agyTestRow, maps.Clone(sendParams))
	if len(prompted) != 1 {
		t.Fatalf("prompted %d times for duplicate client_msg_id, want 1", len(prompted))
	}
	paths := agyAttachedImagePaths(prompted[0])
	if len(paths) != 2 {
		t.Fatalf("prompt missing 2 saved <attached_images> paths: %q", prompted[0])
	}
	for path := range paths {
		if st, err := os.Stat(path); err != nil || st.Size() == 0 {
			t.Fatalf("saved upload %q missing or empty: %v", path, err)
		}
	}

	items, _ := agyTestItems(t, n, agyTestRow, nil)
	if len(items) != 1 || items[0].Role != "user" || items[0].ClientMsgID != "launch-msg-1" || len(agyAttachedImagePaths(items[0].Text)) != 2 {
		t.Fatalf("session.items did not preserve images and client_msg_id: %+v", items)
	}
	full := agyTestCall(t, n, "session.item", agyTestRow, map[string]any{"item_id": items[0].ID}).(proto.Item)
	if full.ClientMsgID != "launch-msg-1" || len(agyAttachedImagePaths(full.Text)) != 2 {
		t.Fatalf("session.item lost client_msg_id or images: %+v", full)
	}
	waitFor(t, "watcher push carries client_msg_id and images", func() bool {
		for _, it := range watchHub.got() {
			if it.ID == items[0].ID && it.ClientMsgID == "launch-msg-1" && len(agyAttachedImagePaths(it.Text)) == 2 {
				return true
			}
		}
		return false
	})
	rec1, ok := n.echoes.record(agyTestRow, "launch-msg-1")
	if !ok || rec1.Line != echoLineKey(items[0].ID) {
		t.Fatalf("echo record not matched: %+v (ok=%v)", rec1, ok)
	}
	echoStatus := agyTestCall(t, n, proto.MethodSessionEchoStatus, agyTestRow, map[string]any{"client_msg_id": "launch-msg-1"}).(proto.SessionEchoStatus)
	if echoStatus.State != proto.EchoSeen {
		t.Fatalf("echo status = %s, want %s", echoStatus.State, proto.EchoSeen)
	}

	// 2. Mid-session image-only send where native media also carries an extra thumbnail: every item on the step gets ClientMsgID.
	agyTestAppend(t, transcriptPath, fmt.Sprintf(`{"step_index":%d,"type":"PLANNER_RESPONSE","status":"DONE","created_at":%q,"content":"Checked both."}`+"\n",
		nextStep, time.Now().UTC().Format(time.RFC3339Nano)))
	nextStep++
	appendNativeMedia = true
	agyTestCall(t, n, "session.send", agyTestRow, map[string]any{
		"text":          "",
		"client_msg_id": "mid-photo-only",
		"images":        []proto.Image{{Mime: "image/png", Data: png1}},
	})
	appendNativeMedia = false
	items, _ = agyTestItems(t, n, agyTestRow, nil)
	var midItems []proto.Item
	for _, it := range items {
		if it.Seq == 3 {
			midItems = append(midItems, it)
		}
	}
	if len(midItems) != 2 || midItems[0].Kind != proto.KindText || midItems[1].Kind != proto.KindImage ||
		midItems[0].ClientMsgID != "mid-photo-only" || midItems[1].ClientMsgID != "mid-photo-only" {
		t.Fatalf("mid-session image-only items did not tag both text and media: %+v", midItems)
	}
	rec2, ok := n.echoes.record(agyTestRow, "mid-photo-only")
	if !ok || rec2.Line != echoLineKey(midItems[0].ID) {
		t.Fatalf("second echo record not matched: %+v (ok=%v)", rec2, ok)
	}

	// 3. Restarted node restores session.echoes, re-tags agy pane items, and notifies hub on new agy sends.
	n2, f2, _ := herdrNode(t)
	n2.home = home
	echoEvents := withEchoHub(t, n2)
	seedAgents(t, n2, f2, agyTestAgent(agyTestPane, home, agyTestID))
	f2.reply("agent.prompt", map[string]any{})
	if _, err := request(t, n2, "session.echoes", map[string]any{"echoes": []echoRecord{rec1, rec2}}); err != nil {
		t.Fatal(err)
	}
	restoredItems, _ := agyTestItems(t, n2, agyTestRow, nil)
	if restoredItems[0].ClientMsgID != "launch-msg-1" || restoredItems[len(restoredItems)-1].ClientMsgID != "mid-photo-only" {
		t.Fatalf("restarted node did not re-tag agy pane items: %+v", restoredItems)
	}
	f2.after(func(c herdrCall) {
		if c.Method == "agent.prompt" {
			line := fmt.Sprintf(`{"step_index":%d,"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","created_at":%q,"content":"Follow-up on n2"}`+"\n",
				nextStep, time.Now().UTC().Format(time.RFC3339Nano))
			nextStep++
			agyTestAppend(t, transcriptPath, line)
		}
	})
	agyTestCall(t, n2, "session.send", agyTestRow, map[string]any{"text": "Follow-up on n2", "client_msg_id": "n2-msg"})
	_, _ = agyTestItems(t, n2, agyTestRow, nil)
	waitFor(t, "hub hears matched session.echo from agy pane", func() bool {
		for _, r := range echoEvents.got() {
			if r.ClientMsgID == "n2-msg" && r.Line != "" && r.AfterSeq == 3 {
				return true
			}
		}
		return false
	})

	// 4. Pane switches conversations (/clear -> agyOtherTestID at step 0) while a send's AfterSeq was recorded on agyTestID.
	currentConv = agyOtherTestID
	transcriptPath = ""
	nextStep = 0
	f.reply("session.snapshot", snapshot(nil, nil, []any{agyTestAgent(agyTestPane, home, agyOtherTestID)}, nil))
	listSessions(t, n)
	agyTestCall(t, n, "session.send", agyTestRow, map[string]any{
		"text":          "Fresh conversation after clear",
		"client_msg_id": "after-clear-msg",
	})
	switchedItems, _ := agyTestItems(t, n, agyTestRow, nil)
	if len(switchedItems) != 1 || switchedItems[0].Text != "Fresh conversation after clear" || switchedItems[0].ClientMsgID != "after-clear-msg" {
		t.Fatalf("send across /clear conversation switch lost client_msg_id: %+v", switchedItems)
	}
}
