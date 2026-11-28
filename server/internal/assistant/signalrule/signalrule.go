// Package signalrule holds the pure decisions about supervision signals that the assistant root acts on: which digest
// goes straight into a running conversation, when a report no longer needs telling, when the card-less review clock
// starts, and when a due estimate is extended without waking its supervisor. It takes plain values, keeps no state and
// imports neither the assistant root nor a sibling domain; the kind names are the wire names of the signals.
package signalrule

import (
	"strings"
	"time"
)

// SentUp says whether a supervision signal is what a supervisor Agent sends up to the conversation above it: its
// completion (a review whose report is "completion"), a decision it needs, or cards it escalated. Such a digest goes
// into the conversation's own input queue even while it works; every other digest is routine and waits in the hub's
// relay queue, where it is held for the owner, re-checked and merged.
func SentUp(kind, report string) bool {
	switch kind {
	case "decide", "escalated":
		return true
	case "review":
		return report == "completion"
	}
	return false
}

// ReportAnswered says whether a report made at reportAt needs no telling any more: its supervisor recorded a review
// of the to-do (reviewedAt, the latest one, zero for none) after it.
func ReportAnswered(reportAt, reviewedAt time.Time) bool {
	return !reviewedAt.IsZero() && reviewedAt.After(reportAt)
}

// CardlessStart is when the settle clock of a card-less review round starts: when the round's review signal reached
// its supervisor (toldAt, nil when no record says), else the round's own start (base). open says that review signal
// still waits (gathered, queued or not yet told), and then there is no clock: the supervisor cannot be late with a
// review it has not read.
func CardlessStart(base time.Time, toldAt *time.Time, open bool) (time.Time, bool) {
	if open {
		return time.Time{}, false
	}
	if toldAt != nil && toldAt.After(base) {
		return *toldAt, true
	}
	return base, true
}

// MaxAutoExtensions is how many times a due estimate is extended on its own between two estimates a supervisor or the
// owner set.
const MaxAutoExtensions = 2

// ProgressWindow is how recent real progress must be for a due estimate of expect minutes to be extended.
func ProgressWindow(expect int) time.Duration {
	return max(10*time.Minute, time.Duration(expect)*time.Minute/4)
}

// Extension is the estimate after an automatic extension, or false when the estimate is not extended: the Agent made
// no recent progress, or done extensions happened already. It grows by half, and by at least ten minutes.
func Extension(expect, done int, progressed bool) (int, bool) {
	if !progressed || done >= MaxAutoExtensions || expect <= 0 {
		return expect, false
	}
	return expect + max(10, expect/2), true
}

// Repeats says whether text only says again what said already told: either holds the other, ignoring spacing.
func Repeats(text, said string) bool {
	a, b := strings.Join(strings.Fields(text), " "), strings.Join(strings.Fields(said), " ")
	return a != "" && b != "" && (strings.Contains(a, b) || strings.Contains(b, a))
}
