package node

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sunfayao/persona/server/internal/node/harness/codex"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A Guardian approval review is Codex's own check of a tool call before it runs. It writes a rollout next to its chat,
// but it is no Agent: neither the list nor its chat's sub-Agents have it, while a thread_spawn sub-Agent stays (#5486).
func TestCodexListLeavesOutGuardianReviews(t *testing.T) {
	for _, tc := range []struct{ name, originator, source string }{
		{"Codex App", "Codex Desktop", "vscode"},
		{"Codex CLI", "codex-tui", "cli"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := remoteTestNode(t)
			home := codex.CanonicalHome(t.TempDir(), n.home)
			chat, spawned, review := "0e000000-0000-4000-8000-000000000011", "0e000000-0000-4000-8000-000000000012", "0e000000-0000-4000-8000-000000000013"
			metas := map[string]map[string]any{
				chat: {"id": chat, "originator": tc.originator, "cwd": "/work/project", "source": tc.source},
				spawned: {"id": spawned, "originator": tc.originator, "cwd": "/work/project", "session_id": chat, "thread_source": "subagent",
					"source": map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": chat, "depth": 1}}}},
				// As Codex writes it: no parent_thread_id, the chat in session_id and forked_from_id.
				review: {"id": review, "originator": tc.originator, "cwd": "/work/project", "session_id": chat, "forked_from_id": chat,
					"thread_source": "subagent", "source": map[string]any{"subagent": map[string]any{"other": "guardian"}}},
			}
			for id, payload := range metas {
				raw, err := json.Marshal(map[string]any{"type": "session_meta", "payload": payload})
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(home, id+".jsonl")
				if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				n.registerCodex(home, id, path)
			}
			if o := readCodexOrigin(filepath.Join(home, review+".jsonl")); !o.Guardian || !o.Subagent {
				t.Fatalf("guardian origin = %+v", o)
			}
			if o := readCodexOrigin(filepath.Join(home, spawned+".jsonl")); o.Guardian || o.Parent != chat {
				t.Fatalf("thread_spawn origin = %+v", o)
			}
			list := n.codexSessions(map[string]bool{})
			byThread := map[string]proto.Session{}
			for _, s := range list {
				byThread[s.SourceThreadID] = s
			}
			if _, listed := byThread[review]; listed || len(list) != 2 {
				t.Fatalf("%d rows, the guardian review listed: %v", len(list), listed)
			}
			if s, root := byThread[spawned], byThread[chat]; !s.IsSubagent || s.RootSessionID != root.SessionID || root.IsSubagent {
				t.Fatalf("sub-Agent %+v under chat %+v", s, root)
			}
		})
	}
}

func TestCodexHistoryAttachesToAlreadySeenTerminalParent(t *testing.T) {
	n := remoteTestNode(t)
	home := t.TempDir()
	parent := "0e000000-0000-4000-8000-000000000071"
	child := "0e000000-0000-4000-8000-000000000072"
	parentID := n.registerCodex(home, parent, "")
	path := filepath.Join(home, "child.jsonl")
	raw := `{"type":"session_meta","payload":{"originator":"codex-tui","parent_thread_id":"` + parent + `","cwd":"/work/child"}}` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	n.registerCodex(home, child, path)
	history := n.codexSessions(map[string]bool{parentID: true})
	if len(history) != 1 || history[0].ParentSessionID != parentID {
		t.Fatalf("history=%+v", history)
	}
	list := append([]proto.Session{{SessionID: parentID, Agent: "codex", SpaceID: "terminal-project", SpaceName: "Project", RepoRoot: "/work/project"}}, history...)
	codex.ResolveHierarchy(list)
	if list[1].RootSessionID != parentID || list[1].SpaceID != list[0].SpaceID || list[1].RepoRoot != list[0].RepoRoot {
		t.Fatalf("terminal child=%+v", list[1])
	}
}
