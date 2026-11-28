package node

import (
	"github.com/sunfayao/persona/server/internal/proto"
	"reflect"
	"testing"
	"time"
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
	if row.Model != "gemini-4-carbon-high" || row.ContextTokens != 34_500 || row.ContextWindow != 512_000 || row.OutputTokens != 400 {
		t.Fatalf("pane row = model:%q ctx:%d/%d out:%d", row.Model, row.ContextTokens, row.ContextWindow, row.OutputTokens)
	}
	if bd := row.ContextBreakdown(); bd == nil || bd.Source != proto.ContextBreakdownEstimated || bd.Free == nil || *bd.Free != 477_500 {
		t.Fatalf("pane row context breakdown = %+v, want estimated with free=477500", bd)
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
	if row.ContextTokens != 8000 || row.ContextWindow != 512_000 || row.OutputTokens != 90 {
		t.Fatalf("unknown model row = ctx:%d/%d out:%d, want 8000/512000 out 90", row.ContextTokens, row.ContextWindow, row.OutputTokens)
	}
}
