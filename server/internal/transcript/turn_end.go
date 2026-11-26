package transcript

import (
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

type TurnClose struct {
	Key string
	At  time.Time
}

// TurnEnd finds the end of the latest turn in items (a session's last turn as its node pages it); false while that
// turn is open, or when the transcript has no end to go by. A turn ends where the runtime says so: Codex's
// task_complete and Claude Code's turn_duration (both a turn_duration item), or the user's stop (the interrupted
// notice, Codex's turn_aborted). Anything after that end that the agent wrote or that starts a turn (a message, a
// wake, a background task's notice, an injected line) belongs to a turn still going, or to one that left no end: it is
// not the turn that ended. A notice of the transcript's own after the end (a compaction) changes nothing.
func TurnEnd(items []proto.Item) (TurnClose, bool) {
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		switch {
		case it.Kind == "turn_duration":
			key := it.ID
			if d := it.TurnDuration; d != nil && d.TurnID != "" {
				key = d.TurnID
			}
			return TurnClose{Key: key, At: it.TS}, key != ""
		case it.Kind == proto.KindNotice && it.Text == InterruptedNotice:
			return TurnClose{Key: it.ID, At: it.TS}, it.ID != ""
		case it.Kind == proto.KindText, it.Kind == proto.KindToolUse, it.Kind == proto.KindToolResult, it.Kind == proto.KindThinking,
			it.Kind == proto.KindImage, it.Role == "user", it.Wake, it.Task != nil, it.Injected != nil:
			return TurnClose{}, false
		}
	}
	return TurnClose{}, false
}
