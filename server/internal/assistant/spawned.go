package assistant

import (
	"cmp"
	"context"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Failed assistant turns. An assistant turn that failed (the node's turn error: is_error, or an answer that is only an
// auth or upstream error) is logged with its conversation and launcher, never its words, which may carry a credential.

const (
	// taskEndTool is the relay kind of a task's end told to the conversation that planned it (taskEndedNotice).
	taskEndTool = "task_end"
	// prFollowupTool is the relay kind of a post-closure delivery-PR event told to the closed to-do's original
	// supervisor conversation (pr_followup.go). Like task_end it is a record, not a push of its own and no hop.
	prFollowupTool = "pr_followup"
)

// assistantTurnSettled: a turn of machine's session sid ended and stayed still. For an assistant conversation, a
// failed turn is logged.
func (s *Service) assistantTurnSettled(ctx context.Context, machine, sid string) {
	sess, ok := s.decoratedSession(machine, sid)
	if !ok { // not in the node's held list (yet): as its machine lists it
		sess, ok = s.session(ctx, machine, sid)
	}
	if !ok || !sess.Assistant || sess.Status == proto.StatusWorking || sess.Starting {
		return
	}
	// The node's word, else its last words when they are the error (an older node, fleet.go).
	sess.TurnError, sess.TurnErrorKind = latestTurnError(sess)
	s.rememberMainReadinessSuccess(ctx, machine, sess)
	failed := strings.TrimSpace(sess.TurnError) != ""
	if failed {
		// The conversation and launcher, and what kind of error: never its words, which may quote a credential.
		s.logf("assistant %s: turn failed (launcher %s, kind %s, code %s)", sid, cmp.Or(sess.Launcher, "claude"),
			cmp.Or(sess.TurnErrorKind, "-"), cmp.Or(sess.TurnErrorCode, turnErrorCode(sess.TurnError), "-"))
	}
	s.recoverMissingReply(ctx, machine, sess)
	if !failed {
		s.inboxPostEvery(ctx, machine, sid) // a task set to put every run in the Inbox (inbox_post.go)
	}
}

// relayOrQueue puts msg into assistant conversation to: now, or queued behind its turn (drainRelays sends it then).
func (s *Service) relayOrQueue(ctx context.Context, to string, msg relayMsg) {
	machine := s.settings(ctx).MachineID
	c, ok := s.session(ctx, machine, to)
	if !ok || c.Status == proto.StatusWorking || c.Starting || s.relayWaits(to, msg) {
		if s.queueRelay(to, msg) > 0 {
			s.publish(ctx) // shown waiting above its composer
			if ok && c.Status != proto.StatusWorking && !c.Starting {
				s.drainRelays(machine, []proto.Session{c})
			}
		}
		return
	}
	if err := s.tryDirectRelay(ctx, machine, to, &msg); err != nil {
		if s.refusedDirect(ctx, machine, to, msg, err) {
			return
		}
		s.logf("relay %s to %s: %v; queued", msg.Tool, short(to), err)
		if s.queueRelay(to, msg) > 0 {
			s.publish(ctx)
		}
	}
}

// taskEndedNotice tells the conversation that planned task t, when the main conversation supervised it for that
// conversation (a side conversation's task_plan, #4669), that t ended: accepted or cancelled, with its link. The main
// conversation reviewed it; this is the planning conversation's record, so it is no hop and is not pushed.
func (s *Service) taskEndedNotice(ctx context.Context, t Todo, accepted bool, note string) {
	v := t.Supervision
	if t.Origin == "" || v == nil || v.Supervisor == "" || v.Supervisor == t.Origin || s.isMain(ctx, t.Origin) {
		return
	}
	verdict := reviewCancelled
	if accepted {
		verdict = reviewAccepted
	}
	text := prompts.RenderTaskEnded(prompts.TaskEnded{ID: t.ID, Title: t.Title, Link: todoLink(s.webBase(ctx), t.ID), Verdict: verdict,
		Note: clip(oneLine(note), 200)})
	s.relayOrQueue(ctx, t.Origin, relayMsg{From: t.ID, FromTitle: "任务「" + t.Title + "」", Tool: taskEndTool, Text: text, At: time.Now().UTC()}) // i18n-ignore: the queue's label, as the task notices'
}
