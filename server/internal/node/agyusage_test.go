package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestAgyPaneSessionTokensAndInfo(t *testing.T) {
	n, herdr, home := herdrNode(t)
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	n.setCatalog([]proto.ManagedLauncher{{
		Name:   "my-agy",
		Agent:  agyAgent,
		Model:  "gemini-4-carbon",
		Models: []proto.ProviderModel{{ID: "gemini-4-carbon", ContextWindow: 1_000_000}},
	}})
	n.state.put(agyTestRow, sessionState{Launcher: "my-agy", Cwd: home, Model: "gemini-4-carbon-high", StartedAt: time.Now()})
	seedAgents(t, n, herdr, agyTestAgent(agyTestPane, home, agyTestID))

	agyTestFile(t, n.agyDataRoot(), agyTestID, ""+
		`{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","created_at":"2026-11-21T10:00:00Z","content":"Check"}`+"\n"+
		`{"step_index":1,"type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T10:00:01Z","model":"gemini-4-carbon-high","thinking":"Looking","content":"Checking now","input_tokens":10000,"cache_read_tokens":20000,"output_tokens":150,"tool_calls":[{"name":"run_command","args":{"CommandLine":"ls"}}]}`+"\n"+
		`{"step_index":2,"type":"GENERIC","status":"DONE","created_at":"2026-11-21T10:00:02Z","content":"ok"}`+"\n"+
		`{"step_index":3,"type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T10:00:03Z","model":"gemini-4-carbon-high","content":"All good.","input_tokens":4500,"cache_read_tokens":30000,"cache_write_tokens":500,"output_tokens":250}`+"\n")

	rows := listSessions(t, n)
	var row proto.Session
	for _, r := range rows {
		if r.SessionID == agyTestRow {
			row = r
		}
	}
	if row.Model != "gemini-4-carbon-high" || row.ContextTokens != 34_500 || row.ContextWindow != 1_000_000 || row.OutputTokens != 400 {
		t.Fatalf("pane row = model:%q ctx:%d/%d out:%d", row.Model, row.ContextTokens, row.ContextWindow, row.OutputTokens)
	}
	if bd := row.ContextBreakdown(); bd == nil || bd.Source != proto.ContextBreakdownEstimated || bd.Free == nil || *bd.Free != 965_500 {
		t.Fatalf("pane row context breakdown = %+v, want estimated with free=965500", bd)
	}

	items, _ := agyTestItems(t, n, agyTestRow, nil)
	var withUsage []proto.Item
	for _, it := range items {
		if len(it.Usage) > 0 {
			withUsage = append(withUsage, it)
		}
	}
	if len(withUsage) != 2 {
		t.Fatalf("items with usage = %+v", withUsage)
	}
	if !reflect.DeepEqual(withUsage[0].Usage, []proto.TokenUsage{{Model: "gemini-4-carbon-high", Input: 10000, CacheRead: 20000, Output: 150}}) {
		t.Fatalf("first reply usage = %+v", withUsage[0].Usage)
	}
	if !reflect.DeepEqual(withUsage[1].Usage, []proto.TokenUsage{{Model: "gemini-4-carbon-high", Input: 4500, CacheRead: 30000, CacheWrite: 500, Output: 250}}) {
		t.Fatalf("second reply usage = %+v", withUsage[1].Usage)
	}

	info := agyTestCall(t, n, "session.info", agyTestRow, nil).(map[string]any)
	if info["usage_unknown"] != nil || info["context_tokens"] != 34_500 || info["output_tokens"] != 400 || info["turns"] != 1 || info["compactions"] != 0 || info["model"] != "gemini-4-carbon-high" {
		t.Fatalf("pane session.info = %+v", info)
	}

	// Unknown model without catalog keeps ContextTokens and OutputTokens while using the same native context window.
	agyTestFile(t, n.agyDataRoot(), agyOtherTestID, ""+
		`{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","created_at":"2026-11-21T10:00:00Z","content":"Hi"}`+"\n"+
		`{"step_index":1,"type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T10:00:01Z","model":"unknown-model-high","content":"Hello","input_tokens":8000,"output_tokens":90}`+"\n")
	seedAgents(t, n, herdr, agyTestAgent(agyTestPane, home, agyOtherTestID))
	n.state.put(agyTestRow, sessionState{Launcher: "agy", Cwd: home, StartedAt: time.Now()})
	rows = listSessions(t, n)
	row = proto.Session{}
	for _, r := range rows {
		if r.SessionID == agyTestRow {
			row = r
		}
	}
	if row.ContextTokens != 8000 || row.ContextWindow != 1_000_000 || row.OutputTokens != 90 {
		t.Fatalf("unknown model row = ctx:%d/%d out:%d, want 8000/1000000 out 90", row.ContextTokens, row.ContextWindow, row.OutputTokens)
	}
}

func TestAgyMainCompactWindowAndRuntimeUsageWindow(t *testing.T) {
	t.Setenv("AGY_CLI_AUTO_COMPACT_OVERRIDE", "")
	t.Setenv("ANTIGRAVITY_CLI_CONFIG_DIR", "")
	n, hub := runtimeNode(t)
	hub.mu.Lock()
	hub.answer = func(method string, _ json.RawMessage) (any, error, bool) {
		return map[string]string{"text": "- Example snapshot"}, nil, method == "assistant.context"
	}
	hub.mu.Unlock()
	hello := assistantRuntimeHello()
	hello["runtime"], hello["name"], hello["launcher"] = agyRuntimeID, "Antigravity", agyAgent
	host := dialRuntime(t, n, hello)
	const convID = "22222222-3333-4444-8555-666666666666"
	const sid = "agy-" + convID
	cwd := filepath.Join(n.home, ".persona", "assistant")
	host.mu.Lock()
	host.answer = func(method string, _ json.RawMessage) (any, error) {
		if method == "session.start" {
			return map[string]any{"session": map[string]any{
				"session_id": sid, "agy_conversation": convID, "cwd": cwd, "status": proto.StatusIdle,
				"model": "gemini-4-neon-xhigh", "launcher": agyAgent, "context_window": 1_048_576,
			}}, nil
		}
		return nil, nil
	}
	host.mu.Unlock()

	if _, err := nodeCall(t, n, "assistant.send", map[string]any{
		"launcher": agyAgent, "cwd": cwd, "text": "hello", "is_main": true, "main_compact_window": 1_000_000,
	}); err != nil {
		t.Fatal(err)
	}
	starts := host.got("session.start")
	var startReq map[string]any
	if len(starts) != 1 || json.Unmarshal(starts[0], &startReq) != nil || startReq["is_main"] != true || startReq["main_compact_window"] != float64(1_000_000) {
		t.Fatalf("runtime session.start = %s, want is_main=true main_compact_window=1000000", starts)
	}
	sends := host.got("session.send")
	var sendReq map[string]any
	if len(sends) != 1 || json.Unmarshal(sends[0], &sendReq) != nil || sendReq["is_main"] != true || sendReq["main_compact_window"] != float64(1_000_000) {
		t.Fatalf("runtime session.send = %s, want is_main=true main_compact_window=1000000", sends)
	}
	if st, ok := n.state.get(sid); !ok || !st.IsMain || st.MainCompactWindow != 1_000_000 {
		t.Fatalf("state = %+v (ok=%v), want IsMain=true MainCompactWindow=1000000", st, ok)
	}

	agyTestFile(t, n.agyDataRoot(), convID, ""+
		`{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","created_at":"2026-11-28T10:00:00Z","content":"hello"}`+"\n"+
		`{"step_index":1,"type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-28T10:00:01Z","model":"gemini-4-neon-xhigh","content":"hi","input_tokens":50000,"cache_read_tokens":200000,"output_tokens":300}`+"\n")
	if err := host.peer.Notify("runtime.sessions", map[string]any{"sessions": []map[string]any{{
		"session_id": sid, "agy_conversation": convID, "cwd": cwd, "status": proto.StatusDone,
		"model": "gemini-4-neon-xhigh", "launcher": agyAgent, "context_window": 1_048_576,
	}}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "runtime row updated with 1M window", func() bool {
		got, _, ok := n.runtimes.rowOf(sid)
		return ok && got.ContextWindow == 1_048_576
	})

	row := listed(t, n)[sid]
	if row.ContextTokens != 250_000 || row.ContextWindow != 1_048_576 {
		t.Fatalf("listed row = %d/%d, want 250000/1048576", row.ContextTokens, row.ContextWindow)
	}
	if row.Facets == nil || row.Facets.Context == nil || row.Facets.Context.Window != 1_048_576 || row.Facets.Context.AutoCompactAt != 1_000_000 || row.Facets.Context.Source != proto.ContextSourceReported {
		t.Fatalf("listed facets.context = %+v", row.Facets)
	}
	if bd := row.ContextBreakdown(); bd == nil || bd.AutoCompactThreshold == nil || *bd.AutoCompactThreshold != 1_000_000 || bd.Free == nil || *bd.Free != 798_576 {
		t.Fatalf("listed breakdown = %+v", bd)
	}

	rawInfo, err := nodeCall(t, n, "session.info", map[string]any{"session_id": sid})
	if err != nil {
		t.Fatal(err)
	}
	var info map[string]any
	if err := json.Unmarshal(rawInfo, &info); err != nil {
		t.Fatal(err)
	}
	if info["context_window"] != float64(1_048_576) {
		t.Fatalf("session.info context_window = %v, want 1048576", info["context_window"])
	}

	// Non-main session respects AGY_CLI_AUTO_COMPACT_OVERRIDE and falls back to settings.json contextWindow.
	n.state.put(sid, sessionState{Cwd: cwd, Launcher: agyAgent})
	if err := os.WriteFile(filepath.Join(n.agyDataRoot(), "settings.json"), []byte(`{"contextWindow":"900K"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	row = listed(t, n)[sid]
	if row.Facets.Context.AutoCompactAt != 900_000 {
		t.Fatalf("settings.json contextWindow auto_compact_at = %d, want 900000", row.Facets.Context.AutoCompactAt)
	}
	t.Setenv("AGY_CLI_AUTO_COMPACT_OVERRIDE", "800K")
	row = listed(t, n)[sid]
	if row.Facets.Context.AutoCompactAt != 800_000 {
		t.Fatalf("env override auto_compact_at = %d, want 800000", row.Facets.Context.AutoCompactAt)
	}
}
