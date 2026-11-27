package node

import (
	"os"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestArchivedCodexResumeRefusesFailedUnarchive(t *testing.T) {
	withCodexLaunchers(t, map[string][]string{"cx": {"codex"}})
	for _, tc := range []struct {
		name string
		fail bool
		out  string
	}{
		{"failed", true, "unarchive failed"},
		{"answered without restoring", false, ""},
		{"unsupported", true, "error: unrecognized subcommand 'unarchive'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, f, cwd := codexHistoryNode(t)
			provenanceHerdr(n)
			listSessions(t, n)
			n.state.put(startThread, sessionState{Launcher: "cx", Cwd: cwd, StopCause: proto.StopTaskCompleted})
			moveCodexRollout(t, n.codexHome(), startThread, true)
			command := stubCodexArchiveCommand(t)
			command.move, command.fail, command.out = false, tc.fail, tc.out
			f.reset()
			resumeFile(f, cwd)
			_, err := request(t, n, "session.start", map[string]any{
				"resume_session_id": startThread, "resume_automatic": true, "prompt": "Continue",
			})
			if code, _ := refusalOf(t, err); code != nativeArchiveFailed {
				t.Fatalf("did not fail closed on native unarchive: %v", err)
			}
			if !archiveIn(t, n.codexHome(), startThread) {
				t.Fatal("a failed unarchive changed the archived thread")
			}
			if calls := f.trace("tab.create", "workspace.create", "pane.send_input"); len(calls) != 0 {
				t.Fatalf("started a terminal despite failed unarchive: %v", calls)
			}
		})
	}
}

func TestArchivedCodexResumeChecksStopCauseBeforeUnarchive(t *testing.T) {
	withCodexLaunchers(t, map[string][]string{"cx": {"codex"}})
	for _, cause := range []proto.StopCause{"", proto.StopUnknown, proto.StopUser, "future"} {
		t.Run(string(cause), func(t *testing.T) {
			n, f, cwd := codexHistoryNode(t)
			provenanceHerdr(n)
			listSessions(t, n)
			n.state.put(startThread, sessionState{Launcher: "cx", Cwd: cwd, StopCause: cause})
			moveCodexRollout(t, n.codexHome(), startThread, true)
			command := stubCodexArchiveCommand(t)
			f.reset()
			_, err := request(t, n, "session.start", map[string]any{
				"resume_session_id": startThread, "resume_automatic": true, "prompt": "Continue",
			})
			if code, _ := refusalOf(t, err); code != "resume_not_allowed" {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if len(command.runs) != 0 || !archiveIn(t, n.codexHome(), startThread) {
				t.Fatalf("a refused resume touched the native archive: %v", command.runs)
			}
			if len(f.trace("tab.create", "pane.send_input")) != 0 {
				t.Fatal("a refused resume opened a terminal")
			}
		})
	}
}

func TestArchivedCodexResumeChecksHoldersBeforeUnarchive(t *testing.T) {
	withCodexLaunchers(t, map[string][]string{"cx": {"codex"}})
	n, f, cwd := codexHistoryNode(t)
	provenanceHerdr(n)
	listSessions(t, n)
	n.state.put(startThread, sessionState{Launcher: "cx", Cwd: cwd, StopCause: proto.StopTaskCompleted})
	moveCodexRollout(t, n.codexHome(), startThread, true)
	command := stubCodexArchiveCommand(t)
	n.codices.mu.Lock()
	n.codices.checked = func() ([]codexProcess, bool) { return []codexProcess{{pid: os.Getpid()}}, true }
	n.codices.mu.Unlock()
	held, err := os.Open(n.codexPath(startThread))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	f.reset()
	_, err = request(t, n, "session.start", map[string]any{
		"resume_session_id": startThread, "resume_automatic": true, "prompt": "Continue",
	})
	if code, _ := refusalOf(t, err); code != codexResumeRefused {
		t.Fatalf("resumed a thread held open: %v", err)
	}
	if len(command.runs) != 0 || !archiveIn(t, n.codexHome(), startThread) {
		t.Fatal("unarchived a thread held by another process")
	}
	if len(f.trace("tab.create", "pane.send_input")) != 0 {
		t.Fatal("started a second writer")
	}
}
