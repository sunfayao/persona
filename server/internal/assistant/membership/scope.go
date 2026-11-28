package membership

import (
	"slices"
	"strings"
)

// Scope says what a supervision signal kind is about.
type Scope int

const (
	// ToDo: the to-do as a whole. One signal of the kind stands for it, whoever works on it; its id is the to-do's and
	// the kind's.
	ToDo Scope = iota
	// Session: one Agent's session. When the to-do has several working members its id carries the session, so the
	// signals of two members live side by side in the queue, in expiry and in merging; a to-do with one member keeps
	// the id it always had.
	Session
)

type entry struct {
	Kind  string
	Scope Scope
}

// scopes is every kind of supervision signal, in the order of the assistant's sig* constants, and what it is about.
// A new kind has to choose here: the table's test lists them all.
var scopes = []entry{
	{"review", ToDo},              // the to-do's work looks finished
	{"owner_choice", ToDo},        // an owner decision no Agent could receive
	{"idle", Session},             // an Agent stopped with its part not finished
	{"unjudged", ToDo},            // the judge could not say whether the to-do is finished
	{"needs_you", Session},        // one Agent waits for an answer
	{"timed_out", ToDo},           // the to-do's review state ran out of time
	{"stalled", Session},          // one working Agent moved nothing for long
	{"no_progress", Session},      // one working Agent only waits and repeats
	{"all_stopped", ToDo},         // every working member stopped
	{"interrupted", Session},      // the model service cut one Agent's turn off
	{"failed", ToDo},              // an Agent did not start: no session exists to name
	{"started", ToDo},             // a start after the supervisor's retry
	{"pr_conflict", ToDo},         // the to-do's PR
	{"draft_pr", ToDo},            // the to-do's PR
	{"pr_event", ToDo},            // the to-do's PR
	{"report", Session},           // what one Agent reported
	{"escalated", ToDo},           // what the supervisor needs the owner for
	{"orphaned", ToDo},            // nobody works on the to-do
	{"ask", Session},              // one Agent's card
	{"decide", ToDo},              // a decision for the conversation above
	{"decision_reply", ToDo},      // the owner answered the to-do's decision
	{"check", ToDo},               // a schedule's news of the to-do
	{"todo_ready", ToDo},          // a dependent to-do can start
	{"pr_merged", ToDo},           // the to-do's PR
	{"parent_closeout", ToDo},     // the pieces of a request are closed
	{"recheck", ToDo},             // a decision waited over a day
	{"undecidable", ToDo},         // a decision nobody can answer
	{"owner_cannot_answer", ToDo}, // the owner's answer to a decision
	{"parked_due", ToDo},          // a parked to-do's time came
	{"park_decided", ToDo},        // a decision ended the parking
	{"parked_summary", ToDo},      // the daily summary of a parked to-do
	{"needs_user_external_info", ToDo},
	{"inbox", ToDo},           // Inbox news of the to-do
	{"delivery_hold", ToDo},   // an owner hold on delivery
	{"cardless_wait", ToDo},   // a review the owner cannot answer
	{"overdue", ToDo},         // the to-do's own brief clock
	{"estimate_due", Session}, // a member's estimate is due (a to-do with one member has the to-do's own)
	{"handoff_result", ToDo},  // the result of a handoff
}

// Kinds are every kind of supervision signal the table knows.
func Kinds() []string {
	out := make([]string, len(scopes))
	for i, s := range scopes {
		out[i] = s.Kind
	}
	return out
}

// ScopeOf is what a signal of kind is about; false for a kind the table does not know (it is then the to-do's).
func ScopeOf(kind string) (Scope, bool) {
	i := slices.IndexFunc(scopes, func(s entry) bool { return s.Kind == kind })
	if i < 0 {
		return ToDo, false
	}
	return scopes[i].Scope, true
}

// SessionBound says a signal of kind is about one Agent's session.
func SessionBound(kind string) bool {
	s, _ := ScopeOf(kind)
	return s == Session
}

// Separator joins a signal id and the session it is about.
const Separator = "#"

// SignalID is id as a signal of kind about session carries it: with several members the session is part of it, with
// one the id is what it always was. A signal that names no session is the to-do's.
func SignalID(id, kind, session string, several bool) string {
	if several && session != "" && SessionBound(kind) {
		return id + Separator + session
	}
	return id
}

// OfKind says signal id is one of kind about to-do todo, whether or not it names a session: todo|kind, or that
// followed by the separator and a session.
func OfKind(id, todo, kind string) bool {
	base := todo + "|" + kind
	return id == base || strings.HasPrefix(id, base+Separator)
}
