package assistant

import (
	"github.com/nwverse/persona/server/internal/proto"
)

// finishOnce drives one finished turn of sess (supAgent on mac) and decides it at once.
func (e *supervisedEnv) finishOnce(sess proto.Session) {
	e.t.Helper()
	e.holdDone()
	driveAgentDone(e.fleetTest, sess)
	e.finishNow()
}
