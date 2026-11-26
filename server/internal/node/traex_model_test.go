package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/proto"
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
