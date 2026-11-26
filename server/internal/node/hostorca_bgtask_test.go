package node

import (
	"strings"
	"testing"
	"time"
)

// Claude Code with background tasks (a stuck Bash moved to the background) waits in its input box and takes text,
// while Orca's terminal.agentStatus says isRunningAgent false. Our scan sees the claude in front of the pane's own
// pty: the message goes in (#5463: refused as not_delivered, with nothing logged).
func TestOrcaSendsWhileClaudeHasBackgroundTasks(t *testing.T) {
	withSubmitGrace(t, 50*time.Millisecond)
	n, o, _ := orcaSession(t)
	o.SetScreen("term_1", "❯ \n")
	o.SetAgentStatus("term_1", false, "") // Orca's answer while Claude has background tasks
	if _, err := request(t, n, "session.send", map[string]any{"session_id": sidA, "text": "hello"}); err != nil {
		code, data := refusalOf(t, err)
		t.Fatalf("refused: code=%s data=%v trace=%v", code, data, o.Trace())
	}
	if w := o.Writes(); len(w) == 0 || !strings.Contains(w[0].Text, "hello") {
		t.Fatalf("writes %+v", w)
	}
}
