package node

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// Claude Code's question after a turn stopped on a usage limit is answered with its first choice only when that
// choice is the selected one; other screens get nothing (#6237).
func TestLimitQuestionKeys(t *testing.T) {
	q := screenFixture(t, "claude-limit-question.txt")
	if got := limitQuestionKeys(q); !slices.Equal(got, []string{"enter"}) {
		t.Fatalf("on the question: %q", got)
	}
	moved := strings.Replace(strings.Replace(q, "❯ 1.", "  1.", 1), "  2. Wait here", "❯ 2. Wait here", 1)
	if got := limitQuestionKeys(moved); got != nil {
		t.Errorf("with another choice selected: %q", got)
	}
	if got := limitQuestionKeys(screenFixture(t, "claude-2.1.312-empty-162.txt")); got != nil {
		t.Errorf("on an empty input box: %q", got)
	}
}

// Any other blocked screen, here the same question with another choice selected (someone at the computer), is left
// alone: the switch is refused as busy with nothing pressed.
func TestSwitchNowLeavesOtherQuestions(t *testing.T) {
	n, f, _, _, _ := switchNode(t, proto.StatusBlocked, nil)
	q := screenFixture(t, "claude-limit-question.txt")
	f.setScreen("w1:p1", strings.Replace(strings.Replace(q, "❯ 1.", "  1.", 1), "  3. Switch", "❯ 3. Switch", 1))
	n.state.setPendingSwitch(sidA, &proto.PendingSwitch{Launcher: "work", At: time.Now()})
	f.reset()
	err := tellNow(t, n, sidA, "go on")
	if code, _ := refusalOf(t, err); code != proto.CodeSwitchBusy {
		t.Fatalf("refused with %q (%v)", code, err)
	}
	if got := drove(f); len(got) != 0 {
		t.Errorf("drove the terminal: %q", got)
	}
}
