// Package snapshotfit shapes the assistant's live snapshot for one conversation: which tasks it lists (Scope), what the
// background supervisor leaves out (Background), and how it is cut to fit one message (Fit).
package snapshotfit

import (
	"strings"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/textfmt"
)

// OpenTodoLimit, TaskLimit: how many rows of the open to-do index and of the task details one conversation's snapshot
// shows. The cached snapshot keeps them all, so a conversation's own come first whatever the others have (Scope).
const (
	OpenTodoLimit = 100
	TaskLimit     = 12
)

// Budget: characters of rendered snapshot one message may carry (#3203). The block stays in the conversation, so its
// size is what every message adds to the context until the next compaction; the node sends only the sections that
// changed, but after a compaction or a restart it sends all of them again.
const Budget = 6000

// Background is snap for the background supervisor (#7315): without what only the main conversation acts on,
// its plan cards waiting for Start (only it withdraws them, toolTaskPlanWithdraw), the owner's unread chats and the tools' notes to review. Their tools are not among a round's (supervise_round.go roundToolNames), and its supervision needs none
// of them.
func Background(snap prompts.Snapshot) prompts.Snapshot {
	snap.Plans, snap.PlansTotal, snap.Chats, snap.ToolReview = nil, 0, nil, nil
	return snap
}

// Scope is snap for a conversation (scope "" for all, snapshotScope): its task lists cut to what one message
// shows, after leaving out, for a scoped one, the tasks that are neither handed out nor supervised by it, so another
// conversation's tasks never crowd its own out. The lists are new: snap shares its own with the cached snapshot.
func Scope(snap prompts.Snapshot, scope string) prompts.Snapshot {
	// A personal to-do has neither an origin nor a supervisor, so only an unscoped snapshot lists it.
	mine := func(o prompts.SnapshotOwner) bool { return scope == "" || o.Origin == scope || o.Supervisor == scope }
	snap.Scoped = scope != ""
	snap.Tasks, _ = firstOf(snap.Tasks, TaskLimit, func(t prompts.SnapshotTask) bool { return mine(t.Owner) })
	snap.Approvals, _ = firstOf(snap.Approvals, ApprovalLimit, func(a prompts.SnapshotApproval) bool { return mine(a.Owner) })
	var accepts int
	snap.Unaccepted, accepts = firstOf(snap.Unaccepted, AcceptLimit, func(a prompts.SnapshotAccept) bool { return mine(a.Owner) })
	if snap.Scoped {
		snap.UnacceptedTotal = accepts
		snap.ToolReview = nil // the main conversation reviews the tools' notes
	}
	shown := map[string]bool{} // a scoped index lists only the open ones its details do not
	for _, t := range snap.Tasks {
		shown[t.ID] = snap.Scoped
	}
	snap.OpenTodos, _ = firstOf(snap.OpenTodos, OpenTodoLimit, func(t prompts.SnapshotOpenTodo) bool { return mine(t.Owner) && !shown[t.ID] })
	return snap
}

// ApprovalLimit, AcceptLimit: how many tasks waiting for the user's nod, and for the user to verify,
// one conversation's snapshot lists (the oldest; the count says how many more wait to be verified).
const (
	ApprovalLimit = 10
	AcceptLimit   = 8
)

// firstOf is a new list of the first limit items of list that keep takes, and how many it takes in all.
func firstOf[T any](list []T, limit int, keep func(T) bool) ([]T, int) {
	var out []T
	n := 0
	for _, x := range list {
		if keep(x) {
			if n++; len(out) < limit {
				out = append(out, x)
			}
		}
	}
	return out, n
}

// Omits are the sections Fit leaves out, the least needed first. Never left out: the agents waiting
// for the user (needs you, awaiting a reply), the plan cards waiting for "Start" and the failed scheduled runs (no
// tool lists them), the calendar and the chats that @ the user.
var Omits = []func(*prompts.SnapshotOmit){
	func(o *prompts.SnapshotOmit) { o.Waiting = true },
	func(o *prompts.SnapshotOmit) { o.Routines = true },
	func(o *prompts.SnapshotOmit) { o.Following = true },
	func(o *prompts.SnapshotOmit) { o.Followups = true },
	func(o *prompts.SnapshotOmit) { o.OtherChats = true },
	func(o *prompts.SnapshotOmit) { o.Asked = true },
	func(o *prompts.SnapshotOmit) { o.Unaccepted = true },
	func(o *prompts.SnapshotOmit) { o.OpenTodos = true },
	func(o *prompts.SnapshotOmit) { o.Tasks = true },
	func(o *prompts.SnapshotOmit) { o.Unread = true },
	func(o *prompts.SnapshotOmit) { o.Approvals = true },
}

// Fit is s rendered within Budget: as it is when it fits, else with sections left out in
// Omits' order until it does (each keeps its heading and count: prompts.SnapshotOmit), else cut short. It
// changes nothing but s.Omit: s shares its lists with the cached snapshot other messages render at the same time,
// and the free text in them was clipped where the snapshot was taken.
func Fit(s prompts.Snapshot) string {
	text := prompts.RenderSnapshot(s)
	for _, omit := range Omits {
		if utf8.RuneCountInString(text) <= Budget {
			return text
		}
		omit(&s.Omit)
		text = prompts.RenderSnapshot(s)
	}
	if utf8.RuneCountInString(text) <= Budget {
		return text
	}
	return cut(text)
}

// cut is text cut short, with the line saying so, within Budget: for what no section can be left out
// of. The agents waiting for the user come first in the template, so they are what stays; the cut is at a line break
// unless that would lose more than half the block (one agent list is one line).
func cut(text string) string {
	tail := "\n" + prompts.SnapshotCut()
	head := textfmt.Cut(text, Budget-utf8.RuneCountInString(tail))
	if i := strings.LastIndexByte(head, '\n'); i > len(head)/2 {
		head = head[:i]
	}
	return head + tail
}
