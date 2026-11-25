package assistant

import (
	"strings"
	"testing"
)

func TestTaskAssignmentAndFrameworkInstructions(t *testing.T) {
	s, f, _ := taskHandoffTestFixture(t, "claude")
	f.mu.Lock()
	f.devices = []Device{{ID: "server", Online: true, Reachable: true}, {ID: "box", Online: true, Reachable: true, TaskInstructions: true}}
	f.mu.Unlock()
	closeoutOn(t, s)
	prompt := taskHandoffTestStart(t, s, f, "task_plan")
	instructions, _ := startsOf(f)[0]["task_instructions"].(string)
	if !strings.HasPrefix(prompt, "Inspect the regression") || strings.Contains(prompt, "<closeout") || instructions == "" || strings.Count(instructions, "<closeout status=") != 1 {
		t.Fatalf("assignment=%q instructions=%q", prompt, instructions)
	}
	t.Logf("assignment_bytes=%d framework_bytes=%d", len(prompt), len(instructions))
}

func TestStructuredDeliveryKeepsDispositionGateAndOwner(t *testing.T) {
	for _, state := range []string{deliveryLive, deliveryPR, deliveryMerged, deliveryOff, deliveryNone} {
		text := `<closeout status="blocked" merged="no" deployed="no">` + "\ndelivery: " + state + "\nmissing: deployment approval\nunblock: user\n</closeout>"
		d, ok := deliveryMark(text)
		if !ok || d.State != state || !deliveryClosable(state) && (d.Gate != "deployment approval" || d.Unblock != unblockUser) {
			t.Errorf("%s: %+v, %v", state, d, ok)
		}
	}
}

