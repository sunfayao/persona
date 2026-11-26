package node

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

func TestTraexLiveModelRequiresNativeReadback(t *testing.T) {
	const alias = "trae-custom"
	const next = "fixture-next"
	cmd := "/model " + next
	oldStep, oldConfirm := codexStep, codexConfirm
	codexStep, codexConfirm = 10*time.Millisecond, 700*time.Millisecond
	t.Cleanup(func() { codexStep, codexConfirm = oldStep, oldConfirm })
	for _, tc := range []struct {
		name, screen, pick, wantCode string
		change, acknowledge, unknown bool
	}{
		{name: "same writer confirms", screen: traeEmpty, pick: next, change: true, acknowledge: true},
		{name: "native model unavailable", screen: traeEmpty, pick: next, change: true, wantCode: "traex_model_unavailable"},
		{name: "acknowledged keys alone", screen: traeEmpty, pick: next, change: true, unknown: true},
		{name: "terminal draft", screen: "────────────────────\n❯ Keep this draft\n────────────────────", pick: next, wantCode: "input_draft"},
		{name: "native busy", screen: traeBusy + traeEmpty, pick: next, wantCode: "traex_model_busy"},
		{name: "native picker", screen: traeEmpty + "\nSelect Model and Effort", pick: next, wantCode: "traex_model_terminal"},
		{name: "outside the launcher's catalog", screen: traeEmpty, pick: "unlisted", wantCode: "traex_model_unavailable"},
		{name: "startup effort variant", screen: traeEmpty, pick: next + "@high", wantCode: "traex_model_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, f, home := herdrNode(t)
			traexHistoryFixture(t, n, nil)
			db := codexAppDBPath(n.traexHome())
			traexHistoryRevisionSQL(t, db, `ALTER TABLE threads ADD COLUMN model TEXT;
				ALTER TABLE threads ADD COLUMN reasoning_effort TEXT;
				ALTER TABLE threads ADD COLUMN model_provider TEXT;
				UPDATE threads SET model = 'fixture-current', model_provider = 'custom.fixture-current' WHERE id = '`+threadMain+`';`)
			invalidateCodexPaginated(n.traexHome())
			initial := proto.Session{SessionID: threadMain, Capabilities: &proto.SessionCapabilities{}}
			n.traexModelRow(&initial)
			if initial.Model != "fixture-current" || initial.Capabilities.Model != nil {
				t.Fatalf("an unknown custom provider was published as switchable: %+v", initial)
			}
			n.state.put(threadMain, sessionState{Launcher: alias})
			n.traexModelRow(&initial)
			if initial.Capabilities.Model == nil || !*initial.Capabilities.Model {
				t.Fatal("a recorded custom launcher lost the model capability")
			}
			t.Setenv("HOME", home)
			t.Setenv("SHELL", "/bin/bash")
			dir := filepath.Join(home, "bin")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			if err := os.WriteFile(filepath.Join(dir, alias), []byte(`#!/bin/sh
[ "$1" = models ] && [ "$2" = --json ] || exit 1
printf '%s' '[{"name":"Next model","config_name":"fixture-next"},{"name":"High startup variant","config_name":"fixture-next@high"}]'
`), 0o700); err != nil {
				t.Fatal(err)
			}

			writeTraexOwner(t, n.home, threadMain, os.Getpid())
			cacheTraexOwnerPane(t, os.Getpid())
			f.setScreen(traexOwnerPane, tc.screen)
			f.reply("pane.process_info", foreground([2]string{"traecli", "traecli"}))
			n.agents = map[string]host.Agent{threadMain: {Agent: traexAgent, PaneID: traexOwnerPane, AgentStatus: proto.StatusIdle}}
			f.after(func(c herdrCall) {
				switch c.Method {
				case "pane.send_text":
					f.setScreen(traexOwnerPane, "────────────────────\n❯ "+cmd+"\n────────────────────")
				case "pane.send_keys":
					if tc.acknowledge {
						traexHistoryRevisionSQL(t, db, `UPDATE threads SET model = 'fixture-next' WHERE id = '`+threadMain+`'`)
						f.setScreen(traexOwnerPane, traeEmpty)
					} else if !tc.unknown {
						f.setScreen(traexOwnerPane, "■ Model 'fixture-next' is unavailable. Use /model to see available models.\n"+traeEmpty)
					} else {
						f.setScreen(traexOwnerPane, traeEmpty)
					}
				}
			})
			_, err := request(t, n, "session.modes", map[string]any{"session_id": threadMain, "model": tc.pick})
			if tc.wantCode != "" {
				if got, _ := refusalOf(t, err); got != tc.wantCode {
					t.Fatalf("refusal = %q, want %q: %v", got, tc.wantCode, err)
				}
			} else if tc.unknown {
				if !maybeDelivered(err) {
					t.Fatalf("unconfirmed native setting was reported as applied: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if tc.change {
				assertTrace(t, f.trace("pane.send_keys"), []string{sendKeys(traexOwnerPane, "enter")})
			} else {
				assertTrace(t, f.trace("pane.send_text"), nil)
				assertTrace(t, f.trace("pane.send_keys"), nil)
			}
			want := "fixture-current"
			if tc.acknowledge {
				want = next
			}
			if got := n.traexNativeModel(context.Background(), threadMain); got != want {
				t.Fatalf("native model = %q, want %q", got, want)
			}
			if len(f.trace("tab.create")) != 0 || len(f.trace("pane.send_input")) != 0 {
				t.Fatal("switch started a process or opened another terminal")
			}
		})
	}
}

func TestTraexModelListParsesEffortsAndContextWindow(t *testing.T) {
	traexModelWindowCache.Lock()
	traexModelWindowCache.byID = map[string]int{}
	traexModelWindowCache.Unlock()

	catalog := []byte(`[
		{"name":"Relay Pro","config_name":"relay-pro","context_window":950000},
		{"name":"Relay Pro · High","config_name":"relay-pro@high"},
		{"name":"Relay Pro · XHigh","config_name":"relay-pro@xhigh"},
		{"name":"Relay Lite","config_name":"relay-lite","_meta":{"trae":{"contextWindow":256000}}}
	]`)
	models, err := traexModelList(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 4 {
		t.Fatalf("got %d models: %+v", len(models), models)
	}
	if !slices.Equal(models[0].Efforts, []string{"high", "xhigh"}) {
		t.Fatalf("base model efforts = %v, want [high xhigh]", models[0].Efforts)
	}
	if models[1].DefaultEffort != "high" || !slices.Equal(models[1].Efforts, []string{"high", "xhigh"}) {
		t.Fatalf("variant model = %+v", models[1])
	}
	if w := lookupTraexModelWindow("relay-pro"); w != 950_000 {
		t.Fatalf("lookupTraexModelWindow(relay-pro) = %d, want 950000", w)
	}
	if w := lookupTraexModelWindow("relay-lite"); w != 256_000 {
		t.Fatalf("lookupTraexModelWindow(relay-lite) = %d, want 256000", w)
	}
}

func TestTraexContextWindowBreakdownAndEffortParity(t *testing.T) {
	n := remoteTestNode(t)
	traexHistoryFixture(t, n, []historyTurn{{
		thread: threadMain, id: "turn-1", status: "completed", ordinal: 1,
		items: [][2]string{userItem("u1", "Analyze the pipeline"), answerItem("a1", "Done.")},
	}})
	rolloutDir := filepath.Join(n.traexHome(), "sessions", "2026", "11", "26")
	if err := os.MkdirAll(rolloutDir, 0o700); err != nil {
		t.Fatal(err)
	}
	rolloutFile := filepath.Join(rolloutDir, "rollout-2026-11-26T10-00-00-"+threadMain+".jsonl")
	rollout := "" +
		`{"timestamp":"2026-11-26T10:00:00Z","type":"session_meta","payload":{"id":"` + threadMain + `","cwd":"/work/app","cli_version":"0.220.0","originator":"trae_cli"}}` + "\n" +
		`{"timestamp":"2026-11-26T10:00:01Z","type":"turn_context","payload":{"model":"relay-pro-max","collaboration_mode":{"mode":"default","settings":{"model":"relay-pro-max","reasoning_effort":"xhigh"}}}}` + "\n" +
		`{"timestamp":"2026-11-26T10:00:02Z","type":"event_msg","payload":{"type":"item_completed","turn_id":"turn-1","item":{"type":"UserMessage","id":"u1","content":[{"type":"text","text":"Analyze the pipeline"}]}}}` + "\n" +
		`{"timestamp":"2026-11-26T10:00:03Z","type":"event_msg","payload":{"type":"item_completed","turn_id":"turn-1","item":{"type":"CommandExecution","id":"c1","command":["ls"],"status":"completed","aggregated_output":"file1\nfile2"}}}` + "\n" +
		`{"timestamp":"2026-11-26T10:00:04Z","type":"event_msg","payload":{"type":"item_completed","turn_id":"turn-1","item":{"type":"AgentMessage","id":"a1","content":[{"type":"text","text":"Done."}]}}}` + "\n" +
		`{"timestamp":"2026-11-26T10:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":500000,"cached_input_tokens":400000,"output_tokens":1234,"total_tokens":501234},"last_token_usage":{"input_tokens":340000,"output_tokens":450,"total_tokens":340450},"model_context_window":950000,"auto_compact_token_limit":909040}}}` + "\n"
	if err := os.WriteFile(rolloutFile, []byte(rollout), 0o600); err != nil {
		t.Fatal(err)
	}
	db := codexAppDBPath(n.traexHome())
	traexHistoryRevisionSQL(t, db, `ALTER TABLE threads ADD COLUMN model TEXT;
		ALTER TABLE threads ADD COLUMN reasoning_effort TEXT;
		ALTER TABLE threads ADD COLUMN model_provider TEXT;
		UPDATE threads SET model = 'relay-pro-max', reasoning_effort = 'xhigh', rollout_path = '`+rolloutFile+`' WHERE id = '`+threadMain+`';`)
	invalidateCodexPaginated(n.traexHome())

	// Materialize paginated history so traexPath points at the synthesized file while rollout holds token_count/turn_context.
	f, err := n.file(threadMain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Refresh(); err != nil {
		t.Fatal(err)
	}

	rows := n.traexSessions(map[string]bool{})
	if len(rows) != 1 {
		t.Fatalf("traexSessions = %+v", rows)
	}
	row := rows[0]
	if row.Model != "relay-pro-max" || row.Effort != "xhigh" || row.ContextTokens != 340_000 || row.ContextWindow != 950_000 || row.OutputTokens != 1234 {
		t.Fatalf("traex row = model:%q effort:%q ctx:%d/%d out:%d", row.Model, row.Effort, row.ContextTokens, row.ContextWindow, row.OutputTokens)
	}
	bd := row.ContextBreakdown()
	if bd == nil || bd.Source != proto.ContextBreakdownEstimated || bd.Free == nil || *bd.Free != 610_000 || bd.AutoCompactThreshold == nil || *bd.AutoCompactThreshold != 909_040 {
		t.Fatalf("traex breakdown = %+v, want estimated free=610000 auto_compact=909040", bd)
	}

	info, err := n.sessionInfo(context.Background(), threadMain)
	if err != nil {
		t.Fatal(err)
	}
	if info["model"] != "relay-pro-max" || info["effort"] != "xhigh" || info["context_tokens"] != 340_000 || info["output_tokens"] != 1234 || info["version"] != "0.220.0" {
		t.Fatalf("traex sessionInfo = %+v", info)
	}
}
