package node

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A TRAE CLI conversation whose turn ended while a command it started in the background still runs lists that
// command in Background, as a Claude's row does, so the manager shows it working in the background; once the CLI
// marks the command ended, the row has no background work and reads as finished again. The rollout and snapshots
// are the redacted fixtures of node/traexbg.
func TestTraexRowListsBackgroundCommands(t *testing.T) {
	n := remoteTestNode(t)
	writeMetaRollout(t, n.traexHome(), threadMain, `{"id":"`+threadMain+`","cwd":"/work/app","source":"cli"}`, time.Now())
	rollout := rolloutPath(n.traexHome(), threadMain)
	fixture := func(name string) string {
		b, err := os.ReadFile(filepath.Join("traexbg", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	appendRollout := func(text string) {
		f, err := os.OpenFile(rollout, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteString(text)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	tasks := filepath.Join(strings.TrimSuffix(rollout, ".jsonl")+".artifacts", "background-tasks")
	if err := os.MkdirAll(tasks, 0o755); err != nil {
		t.Fatal(err)
	}
	// The snapshot as the CLI writes it now: one written over a day ago is from a CLI that went away.
	snapshot := func(name string) {
		stamp := time.Now().UTC().Format(time.RFC3339Nano)
		text := strings.NewReplacer("2026-11-27T03:24:14.185032731Z", stamp, "2026-11-27T03:31:06.604423933Z", stamp).Replace(fixture(name))
		if err := os.WriteFile(filepath.Join(tasks, "shell-12252.snapshot.json"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, turn, _ := strings.Cut(fixture("rollout.jsonl"), "\n") // its own session_meta is the one written above
	appendRollout(turn)
	snapshot("shell-12252.running.snapshot.json")
	n.agents[threadMain] = host.Agent{Agent: traexAgent, PaneID: "w1:p2", Cwd: "/work/app", AgentStatus: proto.StatusDone}
	row := func() proto.Session {
		for _, s := range n.sessionList(context.Background(), false) {
			if s.SessionID == threadMain {
				return s
			}
		}
		t.Fatal("the live conversation is missing")
		return proto.Session{}
	}

	s := row()
	if s.Status != proto.StatusDone || len(s.Background) != 1 {
		t.Fatalf("turn ended, background command running: status %s, background %+v", s.Status, s.Background)
	}
	if b := s.Background[0]; b.ID != "12252" || b.Kind != "shell" || b.Description != "Run the test suite" || b.Command != "make test" {
		t.Fatalf("background command: %+v", b)
	}

	snapshot("shell-12252.completed.snapshot.json")
	appendRollout(fixture("notification.jsonl"))
	if s := row(); len(s.Background) != 0 {
		t.Fatalf("background command finished, still listed: %+v", s.Background)
	}
}
