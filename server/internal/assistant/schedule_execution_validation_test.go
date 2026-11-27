package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestTaskExecutionManagedEligibilityBeforeSend(t *testing.T) {
	for _, target := range []string{"compatible", "work", "managed-codex", "managed-runtime"} {
		t.Run(target, func(t *testing.T) {
			f := newFakeHub().withMachine()
			f.sessions["remote"] = nil
			if target == "work" {
				f.hello.Assistant.WorkMachines = []string{"remote"}
			}
			s := newTestService(t, f)
			agent := "claude"
			switch target {
			case "managed-codex":
				agent = "codex"
			case "managed-runtime":
				agent = "dsh"
			}
			putLaunchers(t, s, storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "managed", Agent: agent, BaseURL: "https://provider.example"}, Share: shareAll, ScopeMachines: []string{"remote"}})
			main := ModelBinding{Provider: "launcher.managed", Model: "selected-model", Effort: "high"}
			setModel(t, s, "assistant", main)
			if b, err := s.taskModelBaseline(context.Background(), "remote"); err != nil || b != main {
				t.Fatalf("fixture did not retain the managed baseline: %+v %v", b, err)
			}
			setTasks(s, Task{ID: "job", Prompt: "Check files", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true, ExecutionMachineID: "remote"})
			_, err := runNow(t, s, "job")
			if target == "compatible" || target == "managed-codex" {
				if err != nil {
					t.Fatal(err)
				}
				if p := f.lastSend(t); p["launcher"] != "managed" || p["model"] != "selected-model" || p["effort"] != "high" {
					t.Fatalf("managed binding was replaced: %v", p)
				}
			} else if err == nil || f.sent() != 0 {
				t.Fatalf("ineligible managed target sent: error=%v sends=%d", err, f.sent())
			}
			if got, err := s.resolveModel(context.Background(), "assistant"); err != nil || got != main {
				t.Fatalf("task changed the assistant default: %+v %v", got, err)
			}
		})
	}
}

func TestTaskExecutionSavedLauncherCatalogAuthority(t *testing.T) {
	for _, catalog := range []string{"empty", "removed", "incompatible", "unknown", "omitted", "null", "unknown-managed", "legacy", "configured"} {
		t.Run(catalog, func(t *testing.T) {
			f := newFakeHub().withMachine()
			switch catalog {
			case "empty", "configured", "unknown-managed":
				f.launcherList = []proto.Launcher{}
			case "removed":
				f.launcherList = []proto.Launcher{{Name: "other", Source: "zshrc"}}
			case "incompatible":
				f.launcherList = []proto.Launcher{{Name: "saved", Source: "dsh", Agent: "dsh", Assistant: false}}
			case "legacy":
				f.launcherList = []proto.Launcher{{Name: "saved"}} // older nodes omit source and agent
			}
			if catalog == "configured" {
				f.hello.Models.Launchers = map[string][]ConfiguredLauncher{"server": {{Launcher: proto.Launcher{Name: "saved"}}}}
			}
			if catalog == "omitted" || catalog == "null" {
				f.answerNode(DeviceLaunchersList, func(string, json.RawMessage) (any, error) {
					if catalog == "null" {
						return map[string]any{"launchers": nil}, nil
					}
					return map[string]any{}, nil
				})
			}
			st := &memStore{}
			s := newServiceOn(t, f, st)
			setTasks(s, Task{ID: "job", Prompt: "Check files", Repeat: repeatInterval, EveryMinutes: 30, Enabled: true})
			setModel(t, s, "task.job", ModelBinding{Provider: "launcher.saved"})
			if catalog == "unknown-managed" {
				st.failKey(launchersManagedKey, errors.New("provider catalog unavailable"))
			}
			_, err := runNow(t, s, "job")
			allowed := catalog == "unknown" || catalog == "omitted" || catalog == "null" || catalog == "unknown-managed" || catalog == "legacy" || catalog == "configured"
			if allowed {
				if err != nil {
					t.Fatal(err)
				}
				if p := f.lastSend(t); p["launcher"] != "saved" {
					t.Fatalf("saved launcher was substituted: %v", p)
				}
			} else if err == nil || f.sent() != 0 {
				t.Fatalf("authoritative unavailable launcher sent: error=%v sends=%d", err, f.sent())
			}
			if !allowed {
				r := request(s, "PUT", "/api/v1/assistant/tasks/job", `{"title":"Edited","prompt":"Check files","repeat":"interval","every_minutes":30,"enabled":true,"launcher":"saved","model":"new-model"}`)
				if r.Code != http.StatusBadRequest || f.sent() != 0 {
					t.Fatalf("unavailable launcher edit: status=%d sends=%d body=%s", r.Code, f.sent(), r.Body.String())
				}
				if got, err := s.resolveModel(context.Background(), "task.job"); err != nil || got.Provider != "launcher.saved" || got.Model != "" {
					t.Fatalf("rejected edit changed the saved binding: %+v %v", got, err)
				}
			}
		})
	}
}
