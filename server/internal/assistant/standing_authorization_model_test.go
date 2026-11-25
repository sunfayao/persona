package assistant

import (
	"context"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

func TestStandingAuthorizationForcePushNeedsDeletionGrant(t *testing.T) {
	for _, plan := range []string{"Force-push the rebased branch to replace the remote history.", "force push the branch", "Rewrite history, then push the branch.", "git reset --hard HEAD~1 then push", "改写历史并覆盖远端。", "git push --force-with-lease origin feature", "强推变基后的分支覆盖远端历史。"} { // i18n-ignore: synthetic card
		ix := proto.Interaction{Kind: proto.IxPlan, Plan: plan}
		m := &standingJudgeTestModel{reply: standingJudgeReply(t, []string{"push"}, nil)}
		a, err := assessStandingWith(t.Context(), m, ix, "Update the task branch.", "/code/app")
		if err != nil {
			t.Fatal(err)
		}
		grant := routeStandingGrant()
		grant.Actions = []string{"push"}
		if standingAssessmentAllowed(grant, a, ix) {
			t.Fatal("ordinary push authorization allowed remote history replacement")
		}
		grant.Actions = append(grant.Actions, "delete")
		grant.Reversible = false
		if standingAssessmentAllowed(grant, a, ix) {
			t.Fatal("irreversible history replacement was authorized")
		}
		grant.Reversible = true
		if !standingAssessmentAllowed(grant, a, ix) {
			t.Fatal("explicit reversible history replacement was rejected")
		}
	}
}

type standingTimeoutModel struct{ calls int }
