package turnmodel

import (
	"regexp"
	"strings"
	"sync"

	"github.com/sunfayao/persona/server/internal/proto"
)

// usageLimitLine is the line Claude Code writes itself when a subscription's limit refused a request: "You've hit
// your session limit · resets 4:20pm (Asia/Shanghai)", "You've hit your weekly limit · resets Sep 28, 4pm (…)". The
// limit is named within a few words, before any full stop: "You've hit your target of 90%. Next, the rate limit…" is
// a reply. Compiled on first use, not whenever a program that links the contract starts (a hook, #1308). A contract
// depends on the standard library only, so not internal/lazyre.
var usageLimitLine = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`^You(?:'|’)ve hit your [^·\n.]{0,40}\blimit\b`)
})

// UsageLimitText: text is Claude Code's line for a subscription usage limit (only as the start of the text: a reply
// may quote one). transcript.UsageLimitText calls this, so the line has one definition; CutOff uses it too.
func UsageLimitText(text string) bool {
	return usageLimitLine().MatchString(strings.TrimSpace(text)) || GatewayQuotaText(text)
}

// GatewayQuotaText matches the gateway's own quota error, never a quotation in a normal answer.
func GatewayQuotaText(text string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "API Error:"))
	return strings.HasPrefix(t, "Request rejected (429) · upstream account quota usage limit has been reached; an account becomes available again at ")
}

// GatewayCapacityText names a temporary lack of upstream accounts, not a user stop or a billing error.
func GatewayCapacityText(text string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "API Error:"))
	return t == "Request rejected (429) · upstream account capacity is temporarily unavailable"
}

// apiErrorMarkers start what Claude Code answers when the model call itself failed (an error card in the app). Both
// sides read a turn by them: the hub's session lists say an agent's turn was cut off (proto.Session.TurnError), the
// assistant's scheduled runs and tasks act on it. "Not logged in · Please run /login" is what Claude Code answers when
// it has no credentials (a model of its own, "<synthetic>"): the turn did not run, the user must sign in.
// Its separator is in the marker, so a reply that talks about being logged out is a reply.
var apiErrorMarkers = []string{"API Error:", "There's an issue with the selected model", "Failed to authenticate",
	"OAuth token has expired", "Invalid API key", "Not logged in ·", "Request too large"}

// refusalMarkers start the other lines Claude Code writes itself instead of an answer when the request was refused
// before the model ran: a sign-in that is gone ("Please run /login · API Error: 403", "Login expired · Please run
// /login"); a subscription's usage limit is its own line (UsageLimitText). Newer versions mark every such
// line in their record (isApiErrorMessage, read by the node into proto.Session.TurnError); these read an older node's
// list. Only as the start of the answer, never inside one (a reply may quote them).
var refusalMarkers = []string{"Please run /login", "Login expired ·", "OAuth token revoked ·", "Login reached its time limit ·"}

// LegacyTurnCutOff reports whether an agent's answer is the model API error that ended its turn (it starts with one, apiErrorMarkers or
// refusalMarkers, or is Claude Code's usage-limit line).
func LegacyTurnCutOff(answer string) bool {
	answer = strings.TrimSpace(answer)
	const sealedPrefix = "this conversation was compacted natively by "
	if len(answer) >= len(sealedPrefix) && strings.EqualFold(answer[:len(sealedPrefix)], sealedPrefix) &&
		proto.UnrecoverableTurnErrorCode(answer) != "" {
		return true
	}
	return startsWithAny(answer, apiErrorMarkers) || startsWithAny(answer, refusalMarkers) || UsageLimitText(answer)
}

func startsWithAny(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// LegacyAPIErrorText detects a model API error in a legacy answer or work line.
func LegacyAPIErrorText(text string) bool {
	for _, marker := range apiErrorMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// ReportsTurnFailures checks whether a row can identify failed model turns.
func ReportsTurnFailures(session proto.Session) bool {
	if session.Facets != nil && session.Facets.Turn != nil {
		return true
	}
	return session.Agent == "" || session.Agent == "codex"
}
