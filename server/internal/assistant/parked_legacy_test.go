package assistant

import (
	"testing"
	"time"
)

func TestLegacyOwnerVerificationStillEligible(t *testing.T) {
	task := Todo{Supervision: &Supervision{State: reviewNeedsUser, Reviews: []TaskReview{{
		Verdict: reviewParked, WaitingOn: parkOwnerPriority,
	}}}}
	for _, conclusion := range []bool{false, true} {
		a := acceptAsk{AskedAt: time.Now(), Conclusion: conclusion, Outcome: outcomeDone}
		if acceptAskStale(&a, task) {
			t.Fatal("the legacy decision fence also blocked an unrelated verification ask")
		}
	}
}
