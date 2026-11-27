package traexbg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixtures are a TRAE CLI 0.217 rollout cut down to one turn (made-up paths, ids and text) and the task snapshots
// the CLI writes beside it: the turn ran `git status` in the foreground, started `make test` in the background and
// ended; notification.jsonl is what the CLI appends when the background command ends.

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

// conversation lays out a rollout and its background-tasks directory in a temporary directory, from the fixtures.
type conversation struct {
	t       *testing.T
	rollout string
	tasks   string
}

func newConversation(t *testing.T) *conversation {
	t.Helper()
	dir := t.TempDir()
	base := filepath.Join(dir, "rollout-2026-11-27T03-20-00-01a00000-0000-7000-8000-00000000000a")
	c := &conversation{t: t, rollout: base + ".jsonl", tasks: filepath.Join(base+".artifacts", "background-tasks")}
	if err := os.MkdirAll(c.tasks, 0o755); err != nil {
		t.Fatal(err)
	}
	c.append(fixture(t, "rollout.jsonl"))
	c.snapshot("shell-20001", "shell-20001.completed.snapshot.json")
	return c
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func (c *conversation) append(text string) {
	c.t.Helper()
	f, err := os.OpenFile(c.rollout, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		c.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		c.t.Fatal(err)
	}
}

// snapshot writes the fixture as the snapshot of task name ("shell-12252").
func (c *conversation) snapshot(name, fixtureName string) {
	c.t.Helper()
	if err := os.WriteFile(filepath.Join(c.tasks, name+".snapshot.json"), []byte(fixture(c.t, fixtureName)), 0o644); err != nil {
		c.t.Fatal(err)
	}
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestTurnEndedWithBackgroundCommandRunning(t *testing.T) {
	c := newConversation(t)
	c.snapshot("shell-12252", "shell-12252.running.snapshot.json")
	r := New(Dependencies{Clock: &fakeClock{now: at("2026-11-27T03:25:00Z")}})
	got := r.Running(c.rollout)
	if len(got) != 1 {
		t.Fatalf("running = %+v, want the background test run alone", got)
	}
	task := got[0]
	if task.ID != "12252" || task.Kind != "shell" || task.Description != "Run the test suite" || task.Command != "make test" {
		t.Fatalf("task = %+v", task)
	}
	if !task.StartedAt.Equal(at("2026-11-27T03:24:14.185Z")) {
		t.Fatalf("started at %v, want the command's start", task.StartedAt)
	}
	if task.OutputFile != filepath.Join(c.tasks, "shell-12252.output.txt") {
		t.Fatalf("output file %q", task.OutputFile)
	}
}

func TestBackgroundCommandFinished(t *testing.T) {
	c := newConversation(t)
	c.snapshot("shell-12252", "shell-12252.running.snapshot.json")
	r := New(Dependencies{Clock: &fakeClock{now: at("2026-11-27T03:25:00Z")}})
	if got := r.Running(c.rollout); len(got) != 1 {
		t.Fatalf("running = %+v", got)
	}
	c.snapshot("shell-12252", "shell-12252.completed.snapshot.json")
	c.append(fixture(t, "notification.jsonl"))
	if got := r.Running(c.rollout); got != nil {
		t.Fatalf("a finished command is still listed: %+v", got)
	}
}

// Every command gets a snapshot, foreground ones too: a running one counts only once the rollout says it went to the
// background, which a long command can do part way (TRAE CLI moves it there when it runs past its wait).
func TestForegroundCommandIsNotBackgroundWork(t *testing.T) {
	c := newConversation(t)
	c.snapshot("shell-12252", "shell-12252.completed.snapshot.json")
	running := strings.NewReplacer(`"12252"`, `"30003"`, "task_output-12252", "task_output-30003").
		Replace(fixture(t, "shell-12252.running.snapshot.json"))
	if err := os.WriteFile(filepath.Join(c.tasks, "shell-30003.snapshot.json"), []byte(running), 0o644); err != nil {
		t.Fatal(err)
	}
	r := New(Dependencies{Clock: &fakeClock{now: at("2026-11-27T03:25:00Z")}})
	if got := r.Running(c.rollout); got != nil {
		t.Fatalf("a foreground command is listed: %+v", got)
	}
	line := strings.NewReplacer(`"process_id":"12252"`, `"process_id":"30003"`, `"make test"`, `"go test ./..."`).
		Replace(backgroundedLine(t))
	half := len(line) / 2
	c.append(line[:half]) // the CLI is still writing the line
	if got := r.Running(c.rollout); got != nil {
		t.Fatalf("listed from half a line: %+v", got)
	}
	c.append(line[half:])
	got := r.Running(c.rollout)
	if len(got) != 1 || got[0].ID != "30003" || got[0].Command != "go test ./..." {
		t.Fatalf("a command moved to the background is not listed: %+v", got)
	}
}

func TestSnapshotLeftRunningByAGoneCLIExpires(t *testing.T) {
	c := newConversation(t)
	c.snapshot("shell-12252", "shell-12252.running.snapshot.json")
	clock := &fakeClock{now: at("2026-11-27T03:25:00Z")}
	r := New(Dependencies{Clock: clock})
	if got := r.Running(c.rollout); len(got) != 1 {
		t.Fatalf("running = %+v", got)
	}
	clock.now = clock.now.Add(maxAge)
	if got := r.Running(c.rollout); got != nil {
		t.Fatalf("a day-old running snapshot is listed: %+v", got)
	}
}

func TestNoBackgroundTasksDirectory(t *testing.T) {
	c := newConversation(t)
	if err := os.RemoveAll(filepath.Dir(c.tasks)); err != nil {
		t.Fatal(err)
	}
	r := New(Dependencies{Clock: &fakeClock{now: at("2026-11-27T03:25:00Z")}})
	if got := r.Running(c.rollout); got != nil {
		t.Fatalf("running = %+v", got)
	}
	if got := r.Running(""); got != nil {
		t.Fatalf("no rollout: %+v", got)
	}
}

// The first look at a long rollout reads only its end, from a line's start; lines longer than the read buffer are
// read whole, lines longer than maxLine skipped.
func TestFirstScanStartsMidRollout(t *testing.T) {
	c := newConversation(t)
	c.snapshot("shell-12252", "shell-12252.running.snapshot.json")
	if err := os.Remove(c.rollout); err != nil {
		t.Fatal(err)
	}
	line := func(n int) string {
		return `{"type":"event_msg","payload":{"type":"token_count","note":"` + strings.Repeat("x", n) + `"}}` + "\n"
	}
	filler := line(100 << 10)
	c.append(strings.Repeat(filler, scanReach/len(filler)+2))
	c.append(line(maxLine + 1))
	c.append(fixture(t, "rollout.jsonl"))
	r := New(Dependencies{Clock: &fakeClock{now: at("2026-11-27T03:25:00Z")}})
	if got := r.Running(c.rollout); len(got) != 1 || got[0].ID != "12252" {
		t.Fatalf("running = %+v", got)
	}
}

func backgroundedLine(t *testing.T) string {
	t.Helper()
	for _, line := range strings.SplitAfter(fixture(t, "rollout.jsonl"), "\n") {
		if strings.Contains(line, `"status":"backgrounded"`) {
			return line
		}
	}
	t.Fatal("the fixture has no backgrounded command")
	return ""
}
