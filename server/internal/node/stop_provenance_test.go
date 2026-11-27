package node

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func provenanceHerdr(n *Node) {
	n.eng.name = proto.EngineHerdr
	n.eng.set = &proto.EngineChoice{Hosts: []string{proto.EngineHerdr}, Default: proto.EngineHerdr}
}

func TestRestartStatusDoesNotAuthorizeUnknownStopCause(t *testing.T) {
	for _, cause := range []proto.StopCause{proto.StopUnknown, proto.StopUser, proto.StopIdle, proto.StopRestart, proto.StopCause("future")} {
		t.Run(cause.String(), func(t *testing.T) {
			n, _, home := herdrNode(t)
			provenanceHerdr(n)
			cwd := mkdir(t, filepath.Join(home, "project"))
			n.state.put(sidA, sessionState{Launcher: "claude", Cwd: cwd, Pane: "w1:p1", AppMode: "plan",
				StartedAt: n.bootAt.Add(-time.Hour), StopCause: cause})
			rows := []proto.Session{{SessionID: sidA, Agent: "claude", Host: proto.EngineHerdr, Status: proto.StatusOffline}}
			n.markInterrupted(rows, map[string]bool{})
			n.markStopped(&rows[0])
			if rows[0].Status != proto.StatusInterrupted || rows[0].Capabilities == nil || rows[0].Capabilities.StopCause != cause {
				t.Fatalf("restart display invented provenance: %+v", rows[0].Capabilities)
			}
			if st, _ := n.state.get(sidA); st.StopCause != cause || st.AppMode != "plan" {
				t.Fatalf("restart classification changed authorization: %+v", st)
			}
		})
	}
}

func TestRunningObservationRevokesOldResumeEligibility(t *testing.T) {
	n, _, _ := herdrNode(t)
	for _, cause := range []proto.StopCause{proto.StopIdle, proto.StopTaskCompleted, proto.StopRestart, proto.StopUser} {
		n.state.put(sidA, sessionState{Launcher: "claude", Cwd: "/work/project", AppMode: "plan", StopCause: cause})
		n.observedRunning(sidA)
		st, _ := n.state.get(sidA)
		if st.StopCause != proto.StopUnknown || st.AppMode != "plan" {
			t.Fatalf("running observation left stale eligibility: %+v", st)
		}
	}
}

func TestRunningObservationCannotEraseAnInFlightUserClose(t *testing.T) {
	n, _, _ := herdrNode(t)
	n.state.put(sidA, sessionState{Launcher: "claude", Cwd: "/work/project", StopCause: proto.StopUser})
	release := n.recovery.lock(sidA)
	n.observedRunning(sidA)
	cause := n.stopCause(sidA)
	release()
	if cause != proto.StopUser {
		t.Fatal("the still-visible process erased the user's close intent")
	}
}

func TestGuardedResumeRejectsStaleRowsAndPermissionOverrides(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cause    proto.StopCause
		mode     string
		launcher string
	}{
		{"user stopped", proto.StopUser, "", ""},
		{"unknown", proto.StopUnknown, "", ""},
		{"future cause", proto.StopCause("future"), "", ""},
		{"permission override", proto.StopIdle, "bypassPermissions", ""},
		{"launcher override", proto.StopIdle, "", "another-launcher"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, f, _ := keptAgent(t)
			provenanceHerdr(n)
			n.claudeStart, n.claudeReady = 20*time.Millisecond, 20*time.Millisecond
			n.state.stopped(sidA, tc.cause)
			_, err := request(t, n, "session.start", map[string]any{
				"resume_session_id": sidA, "resume_automatic": true, "prompt": "Continue the assigned task",
				"permission_mode": tc.mode, "launcher": tc.launcher,
			})
			if err == nil {
				t.Fatal("guarded resume succeeded")
			}
			if code, _ := refusalOf(t, err); code != "resume_not_allowed" {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if calls := f.trace("pane.send_input", "tab.create", "workspace.create"); len(calls) != 0 {
				t.Fatalf("a refused resume touched the terminal: %v", calls)
			}
		})
	}

	t.Run("owner closed after the assistant listed the row", func(t *testing.T) {
		n, f, _ := keptAgent(t)
		provenanceHerdr(n)
		n.claudeStart, n.claudeReady = 20*time.Millisecond, 20*time.Millisecond
		n.state.setPane(sidA, "w1:p1")
		n.state.stopped(sidA, proto.StopIdle)
		cached := proto.Session{SessionID: sidA, Agent: "claude", Host: proto.EngineHerdr, Status: proto.StatusOffline,
			Capabilities: &proto.SessionCapabilities{Read: true, Terminal: true, StopCause: n.stopCause(sidA)}}
		if err := n.close(t.Context(), sidA); err != nil {
			t.Fatal(err)
		}
		f.reset()
		_, err := request(t, n, "session.start", map[string]any{
			"resume_session_id": sidA, "resume_automatic": true, "prompt": "Continue",
		})
		if err == nil {
			t.Fatal("a cached eligible row undid the user's close")
		}
		if code, _ := refusalOf(t, err); code != "resume_not_allowed" || cached.Capabilities.StopCause != proto.StopIdle {
			t.Fatalf("did not independently recheck node state: %v", err)
		}
		if len(f.trace("pane.send_input", "tab.create")) != 0 {
			t.Fatal("stale resume reached the terminal")
		}
	})
}

func TestClosingStoppedTerminalsRecordsUserIntent(t *testing.T) {
	for _, action := range []string{"pane", "finished terminals", "exit"} {
		t.Run(action, func(t *testing.T) {
			n, _, _ := keptAgent(t)
			provenanceHerdr(n)
			n.state.stopped(sidA, proto.StopIdle)
			switch action {
			case "pane":
				if err := n.closePane(t.Context(), "w1:p1"); err != nil {
					t.Fatal(err)
				}
			case "finished terminals":
				if _, err := request(t, n, "panes.close_finished", map[string]any{"preview": true}); err != nil {
					t.Fatal(err)
				}
				if n.stopCause(sidA) != proto.StopIdle {
					t.Fatal("preview changed stop provenance")
				}
				if _, err := request(t, n, "panes.close_finished", map[string]any{"panes": "w1:p1"}); err != nil {
					t.Fatal(err)
				}
			case "exit":
				if err := n.exitAgent(t.Context(), sidA); err != nil {
					t.Fatal(err)
				}
			}
			want := proto.StopUser
			if action == "exit" {
				want = proto.StopExit // the Agent was told to quit: an end of its session, never resume-eligible (#10355)
			}
			if got := n.stopCause(sidA); got != want || got.AllowsSupervisorResume() {
				t.Fatalf("user closure: stop cause %s, want %s", got, want)
			}
		})
	}
}

func TestOwnerResumeStillUsesItsExplicitPathAfterUserStop(t *testing.T) {
	n, f, _ := keptAgent(t)
	provenanceHerdr(n)
	n.state.stopped(sidA, proto.StopUser)
	n.claudeStart, n.claudeReady = 20*time.Millisecond, 20*time.Millisecond
	// The scripted shell does not launch Claude; reaching its resume command is the boundary under test.
	_, _ = request(t, n, "session.start", map[string]any{"resume_session_id": sidA})
	var command string
	for _, call := range f.calls() {
		if call.Method == "pane.send_input" {
			command += str(call.Params["text"])
		}
	}
	if !strings.Contains(command, "--resume "+sidA) {
		t.Fatal("owner Resume did not reach the existing terminal path")
	}
}

func TestResumeConsumesItsPreviousStopEvenWhenStartFails(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "owner", true: "supervisor"}[automatic], func(t *testing.T) {
			n, f, _ := keptAgent(t)
			provenanceHerdr(n)
			n.claudeStart, n.claudeReady = 20*time.Millisecond, 20*time.Millisecond
			n.state.stopped(sidA, proto.StopIdle)
			_, _ = request(t, n, "session.start", map[string]any{
				"resume_session_id": sidA, "resume_automatic": automatic,
			})
			if n.stopCause(sidA) != proto.StopUnknown {
				t.Fatal("a failed/uncertain start retained the previous idle stop's authorization")
			}
			f.reset()
			_, err := request(t, n, "session.start", map[string]any{
				"resume_session_id": sidA, "resume_automatic": true,
			})
			if err == nil {
				t.Fatal("an uncertain previous start was resumed automatically")
			}
			if code, _ := refusalOf(t, err); code != "resume_not_allowed" {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if len(f.trace("pane.send_input", "tab.create", "workspace.create")) != 0 {
				t.Fatal("the second resume touched the terminal")
			}
		})
	}
}
