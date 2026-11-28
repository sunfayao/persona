package signalrule

import (
	"slices"
	"strings"

	"github.com/sunfayao/persona/server/internal/transcript"
)

// The kinds of tool call the no_progress watch tells apart.
const (
	CallNeutral = iota
	CallWait
	CallProgress
)

// ActivityCall is what kind of call an activity line (transcript.Activity) says: its fixed line, or its verb.
func ActivityCall(line string) int {
	key, _ := transcript.ActivityKey(line)
	if key == "" {
		if verb, _, ok := strings.Cut(line, " "); ok {
			key, _ = transcript.ActivityKey(verb)
		}
	}
	waits := []string{"act_waiting", "act_waiting_task", "act_ci"}
	progress := []string{"act_edit", "act_write", "act_commit", "act_push", "act_pr_create", "act_pr_merge", "act_release", "act_deploy"}
	switch {
	case slices.Contains(waits, key):
		return CallWait
	case slices.Contains(progress, key):
		return CallProgress
	}
	return CallNeutral
}
