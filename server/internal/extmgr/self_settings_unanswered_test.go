package extmgr

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A secret-input setting card (such as adding a mail account) is the user's to approve (#3722): a card that expired
// or was closed, or one someone else answered for the user, changes nothing and says nobody answered, never declined.
func TestSelfSettingsUnansweredCardIsNoDecline(t *testing.T) {
	h := newTestManager(t)
	fm := &selfSettingsMail{}
	h.mail = fm
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	yes, no := true, false
	for name, r := range map[string]proto.Resolution{
		"expired or closed": {Dismiss: true},
		"allowed for them":  {Allow: &yes, AnsweredBy: proto.AnsweredByAssistant, Values: map[string]string{"password": "pw"}},
		"denied for them":   {Allow: &no, AnsweredBy: proto.AnsweredByAssistant},
		"cancelled":         {Cancel: true},
	} {
		th := hubOf(h)
		th.mu.Lock()
		th.answer = func(proto.Interaction) proto.Resolution { return r }
		th.mu.Unlock()
		out, err := h.toolPersonaSettings(ctx, "server", "s1", json.RawMessage(`{"action":"add","key":"mail.accounts","value":{"email":"me@example.com"}}`))
		if err != nil || out != prompts.RenderSettingUnanswered() {
			t.Errorf("%s: %q %v", name, out, err)
		}
		if len(fm.added) != 0 {
			t.Errorf("%s: the mail account was added: %v", name, fm.added)
		}
	}
}
