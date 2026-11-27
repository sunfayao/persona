package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Nothing to resume with: no file, a deleted one, or one outside the agent's sessions directory. Nothing is started.
func TestOmpResumeRefusals(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	n, f, home, _ := piHerdrNode(t)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	// A session listed from disk whose file is then deleted.
	id := "01b10000-0000-7000-8000-000000000002"
	file := writeOmpSession(t, ompSessionsDir(home), "-tmp-y", id, "gone", t.TempDir(), time.Now())
	_ = listSessions(t, n) // the scan caches the file
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	_, err := request(t, n, "session.send", map[string]any{"session_id": id, "text": "hi", "client_msg_id": "m-gone"})
	if code, _ := refusalOf(t, err); code != "omp_resume_unknown" {
		t.Fatalf("code %q (%v)", code, err)
	}
	if got := f.trace("tab.create"); len(got) != 0 {
		t.Fatalf("a tab was opened: %v", got)
	}

	// A file outside the agent's sessions directory, or with an apostrophe, is never run.
	root := filepath.Join(ompAgentDir(home), "sessions")
	for _, p := range []string{
		filepath.Join(root, "..", "x.jsonl"),
		filepath.Join(root, "a'b.jsonl"),
		filepath.Join(root, "a.txt"),
		"/etc/passwd",
	} {
		if ompResumePath(home, p) {
			t.Errorf("%s was taken as a resume", p)
		}
	}
	if !ompResumePath(home, filepath.Join(root, "-w", "t_uuid.jsonl")) {
		t.Error("a session file under the agent's sessions directory was refused")
	}
}

// herdr's own restore of the pane: an omp gets `omp --resume <file>` once its file is known, and no command at all
// (never `omp resume <pane>`) when it is not.
func TestOmpHerdrResumeReport(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "")
	n, _, home, _ := piHerdrNode(t)
	id := "01b10000-0000-7000-8000-000000000005"
	file := filepath.Join(ompAgentDir(home), "sessions", "-w", "t_"+id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"type":"session","id":"`+id+`","cwd":"/w"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := agentLaunch{Agent: ompAgent, Launcher: ompAgent, ID: id}
	unknown := agentLaunch{Agent: ompAgent, Launcher: ompAgent, ID: "01b10000-0000-7000-8000-000000000006"}
	if argv, ok := n.agentResumeArgv(unknown, unknown.ID); ok {
		t.Fatalf("without a known file herdr was given %q", argv)
	}
	n.ompFiles = map[string]string{id: file}
	argv, ok := n.agentResumeArgv(spec, id)
	if !ok || argv[0] != "eval" || !strings.Contains(argv[1], "omp --resume "+file) {
		t.Fatalf("resume_argv %q %v", argv, ok)
	}
	if strings.Contains(argv[1], "omp resume") {
		t.Fatalf("the pane id was used as a session: %q", argv)
	}
}
