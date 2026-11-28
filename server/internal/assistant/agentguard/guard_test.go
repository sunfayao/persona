package agentguard

import (
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestPlainResumeOnlyForTheOwnersMainTurn(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ main, owner, want bool }{{true, true, true}, {true, false, false}, {false, true, false}, {false, false, false}} {
		if got := PlainResume(c.main, c.owner); got != c.want {
			t.Errorf("PlainResume(main=%v, owner=%v) = %v, want %v", c.main, c.owner, got, c.want)
		}
	}
	if got := NoTaskRefusal("Agent a", true); !strings.Contains(got, "Give task") || !strings.Contains(got, "Nothing was started or sent") {
		t.Errorf("main refusal = %q", got)
	}
	if got := NoTaskRefusal("Agent a", false); strings.Contains(got, "Give task") {
		t.Errorf("side conversation refusal offers task: %q", got)
	}
}

func TestArchiveRefusal(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	owner := func(v bool) func() bool { return func() bool { return v } }
	cases := []struct {
		name string
		f    ArchiveFacts
		want string // substring of the refusal; "" allowed
	}{
		{"idle and unlinked", ArchiveFacts{Now: now}, ""},
		{"recent owner input", ArchiveFacts{Now: now, LastOwnerInput: now.Add(-time.Hour)}, "last 24 hours"},
		{"old owner input", ArchiveFacts{Now: now, LastOwnerInput: now.Add(-25 * time.Hour)}, ""},
		{"open to-do", ArchiveFacts{Now: now, OpenTodo: "todo_1"}, "open to-do todo_1"},
		{"both", ArchiveFacts{Now: now, LastOwnerInput: now, OpenTodo: "todo_1"}, "and it is linked to open to-do todo_1"},
		{"owner turn passes", ArchiveFacts{Now: now, LastOwnerInput: now, OpenTodo: "todo_1", OwnerTurn: owner(true)}, ""},
		{"other turn refused", ArchiveFacts{Now: now, OpenTodo: "todo_1", OwnerTurn: owner(false)}, "needs the owner to ask"},
	}
	for _, c := range cases {
		got := ArchiveRefusal("Agent a", c.f)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: refusal %q, want %q", c.name, got, c.want)
		}
	}
	asked := false
	ArchiveRefusal("Agent a", ArchiveFacts{Now: now, OwnerTurn: func() bool { asked = true; return false }})
	if asked {
		t.Error("the owner-turn probe ran for a session nothing guards")
	}
}

func TestCompactable(t *testing.T) {
	t.Parallel()
	if !Compactable(proto.Session{Agent: "claude"}) || Compactable(proto.Session{Agent: "codex", SpaceKind: codexAppSpace}) {
		t.Error("Compactable: claude terminal yes, Codex App no")
	}
	if got := NotCompactable(proto.Session{Agent: "codex", SpaceKind: codexAppSpace}); !strings.Contains(got, "Codex App") {
		t.Errorf("NotCompactable = %q", got)
	}
}
