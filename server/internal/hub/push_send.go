package hub

import (
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/i18n"
)

// pushPolicyWithSource keeps the destination the push already carries. Every production alert payload uses the same
// machine_id / session_id pair; a decision also has a todo object. The related-key conventions stay in deliverPushFrom:
// a free-form key from notify_user must not become a typed source merely by looking like one.
func pushPolicyWithSource(policy pushPolicy, payload map[string]any) pushPolicy {
	if policy.todoID == "" {
		policy.todoID = todoID(payload["todo"])
	}
	machine, _ := payload["machine_id"].(string)
	session, _ := payload["session_id"].(string)
	if policy.machineID == "" {
		policy.machineID = machine
	}
	if policy.sessionID == "" {
		policy.sessionID = session
	}
	return policy
}

// grade applies the grading policy to a push (level, related key and the held-push record's text) and records it.
// heldReason is "" when the push may go out, else why the grading filter held it (push_grading.go): a caller that
// answers the model (the assistant's notify_user) tells it the notice reached the briefing instead of a device.
func (p *Pusher) grade(manager string, policy pushPolicy, build localized) (heldReason string) {
	if p == nil {
		return ""
	}
	if p.pushGradingOn() { // off: nothing below runs and the push goes exactly as before
		policy.manager = manager
		payload := build(i18n.Default)
		policy = pushPolicyWithSource(policy, payload)
		if policy.title == "" || policy.body == "" {
			// The held-push record's text, from the alert when the caller gave none. A payload need not have one (an
			// extension's push_localized may carry no aps at all).
			aps, _ := payload["aps"].(map[string]any)
			alert, _ := aps["alert"].(map[string]any)
			if t, _ := alert["title"].(string); policy.title == "" {
				policy.title = t
			}
			if b, _ := alert["body"].(string); policy.body == "" {
				policy.body = b
			}
		}
		if allow, reason := p.applyPushGrading(policy); !allow {
			// Held back for every device (WebSocket notify and Web Push, gated by callers via heldReason);
			// state lists and held-push briefings keep updating.
			return reason
		}
	} else {
		// The switch is off, so applyPushGrading never runs; the daily delivery counts still record one logical push.
		policy.manager = manager
		level := policy.level
		if level == "" {
			level = assistantwire.LevelP1
		}
		p.recordSent(policy, level, p.now())
	}
	return ""
}

// localized builds a push (or WebSocket notification) in a language.
type localized func(i18n.Lang) map[string]any
