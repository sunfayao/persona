package turnmodel

import (
	"github.com/sunfayao/persona/server/internal/proto"
	"strings"
)

// FinishedText is the words the finished push carries for s's turn, chosen as hub/machine.go notifyFinished chooses
// them: the work line it ended on (done or stuck), else its last words; "" when it said nothing (the push then says a
// localised "done", which is never a cut-off). NormalFinish judges a cut-off on them.
func FinishedText(s proto.Session) string {
	if w := s.Work; w != nil && w.Line != "" && (w.Phase == proto.WorkDone || w.Phase == proto.WorkBlocked) {
		return w.Line
	}
	return s.LastText
}

// NormalFinish: s's turn ended normally, as the finished push would tell it: no model error, no cut-off answer, not
// stuck on something. A quiet task's conversation (PushAsked) pushes nothing then; an error, a cut-off or a stuck
// turn is told as ever.
func NormalFinish(s proto.Session) bool {
	return strings.TrimSpace(s.TurnError) == "" && !((s.Facets == nil || s.Facets.Turn == nil) && LegacyTurnCutOff(FinishedText(s))) && !(s.Work != nil && s.Work.Phase == proto.WorkBlocked)
}

// QuietTaskFinish: s, the own conversation of a task that pushes only what its runs send, may go unpushed now. Only
// when it runs on an agent whose failed turn is told on the session (reportsFailures), so that NormalFinish can tell
// "nothing new" from an error. Never the main conversation (main: its session id), which the user may have made of
// the task's. And only a normal finish (NormalFinish).
func QuietTaskFinish(s proto.Session, main string) bool {
	return ReportsTurnFailures(s) && (main == "" || s.SessionID != main) && NormalFinish(s)
}

// SupervisedFinish: s, a conversation whose current turn a supervision message started (SessionView.Supervising), may go
// unpushed now: a normal finish (NormalFinish); an error, a cut-off or a stuck turn is pushed as ever.
func SupervisedFinish(s proto.Session, supervising bool) bool {
	return supervising && ReportsTurnFailures(s) && NormalFinish(s)
}
