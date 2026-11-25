package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// dispatchPolicyHarness is an assistant with five conversations: the main one, a leader, the background supervisor,
// the scheduled task's own conversation and an ordinary side conversation. All ids are placeholders.
type dispatchPolicyHarness struct {
	*spawnHarness
	mainID   string
	leaderID string
	bgID     string
	patrolID string
	sideID   string
}

func TestDispatchPolicyValidation(t *testing.T) {
	now := time.Now().UTC()
	rule := func(class string, deny ...string) DispatchRule {
		return DispatchRule{ConversationClass: class, Deny: deny}
	}
	cases := []struct {
		name          string
		policy        DispatchPolicy
		allowNoExpiry bool
		wantErr       bool
	}{
		{"owner policy without expiry", DispatchPolicy{Rules: []DispatchRule{rule(dispatchClassScheduled, dispatchActionTaskPlan)}}, true, false},
		{"extension policy must expire", DispatchPolicy{Rules: []DispatchRule{rule(dispatchClassScheduled, dispatchActionTaskPlan)}}, false, true},
		{"expiry within the hour", DispatchPolicy{Rules: []DispatchRule{rule(dispatchClassOther, dispatchActionSendToMain)}, ExpiresAt: ptr(now.Add(30 * time.Minute))}, false, false},
		{"extension expiry over the hour", DispatchPolicy{Rules: []DispatchRule{rule(dispatchClassOther, dispatchActionSendToMain)}, ExpiresAt: ptr(now.Add(2 * time.Hour))}, false, true},
		{"owner expiry over the hour is allowed", DispatchPolicy{Rules: []DispatchRule{rule(dispatchClassOther, dispatchActionSendToMain)}, ExpiresAt: ptr(now.Add(2 * time.Hour))}, true, false},
		{"expiry past", DispatchPolicy{Rules: []DispatchRule{rule(dispatchClassOther, dispatchActionSendToMain)}, ExpiresAt: ptr(now.Add(-time.Minute))}, false, true},
		{"unknown class", DispatchPolicy{Rules: []DispatchRule{rule("side", dispatchActionTaskPlan)}}, true, true},
		{"unknown action", DispatchPolicy{Rules: []DispatchRule{rule(dispatchClassMain, "delete_everything")}}, true, true},
		{"empty deny", DispatchPolicy{Rules: []DispatchRule{rule(dispatchClassMain)}}, true, true},
		{"duplicate class", DispatchPolicy{Rules: []DispatchRule{rule(dispatchClassMain, dispatchActionTaskPlan), rule(dispatchClassMain, dispatchActionAgentStart)}}, true, true},
		{"no rules", DispatchPolicy{}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.validate(tc.allowNoExpiry, now)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validate: %v, wantErr %v", err, tc.wantErr)
			}
		})
	}

	long := DispatchPolicy{Rules: []DispatchRule{rule(dispatchClassScheduled, dispatchActionTaskPlan)}}
	long.Rules[0].Message = strings.Repeat("字", dispatchPolicyMaxMessageRunes+1)
	if err := long.validate(true, now); err == nil {
		t.Fatal("an over-long rule message was accepted")
	}
}
