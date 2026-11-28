package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/agentguard"
	"github.com/sunfayao/persona/server/internal/assistant/ownerfence"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// The assistant's resume of a stopped agent (#8099). A stopped agent is read-only (node stopped.go, #4754): no message
// wakes it, and Persona's own follow-ups (rework, relayed answers, digests) are still never delivered to it. What the
// assistant may do is resume it on purpose, as the user's Resume on its terminal page does, when the next work needs
// that agent's context (it has just found the cause and should now fix it, rework, the user asked for "the one with
// the context"): agent_send with resume true and resume_why. The resume is
//
//   - the same node call as the user's Resume: session.start with resume_session_id and a node-side recovery guard
//     (node node.go fillResume: the session's own launcher, directory, model and permission mode; the shell it left
//     when that is still there, else a new tab), the text as its first message;
//   - supervised: the agent is on an open task of this conversation already, or the call gives task (the main
//     conversation), which makes it one as agent_send with a task does (wakeResumed); only the main conversation in a
//     turn the owner's own message started may wake the owner's session with no task (agentguard.PlainResume);
//   - visible: the phone's confirmation and the task's card say which agent was resumed and why (resume_note);
//   - compacted before the text goes in when its context is nearly full (#11137): resumed without a first message,
//     then the text sent with compact_if_full, so the node runs the runtime's own /compact and sends once it is done
//     (node compact_before_send.go). Only a failed compaction, or a runtime Persona cannot compact, falls back to a
//     replacement, saying why;
//   - refused for user/unknown stops, offline computers, or the node's own start refusals. The main conversation's own
//     agent_send, which acts for the owner, also resumes an Agent that ended without an owner's Stop (told to quit:
//     exit; ended on its own: unknown) when its node takes that (capabilities.delegated_resume): session.start with
//     resume_delegated, which the node rechecks under the same lock (node stop_provenance.go). An owner's Stop (user)
//     stays the owner's Resume's, and so does every stop a lead, a scheduled run or an automatic follow-up meets.

// resumeAllowed checks a resume before anything is asked or started: a reason, the computer reachable, room left in
// the agent's context, and a task of self's it will be on (one it is on, or task, which the send's checks go on with).
// Only recorded non-user stops: the automatic flows that resume (rework, routed PR events, inbox forwards) use it.
func (s *Service) resumeAllowed(ctx context.Context, self string, am Device, c proto.Session, why string, task *sendTask) error {
	_, err := s.resumeCheck(ctx, self, am, c, why, task, false)
	return err
}

// resumeCheck is resumeAllowed for agent_send. forOwner: the call is the main conversation's, which acts for the
// owner and may also resume an Agent that ended without an owner's Stop (exit, unknown) when its node takes that;
// delegated then says the resume needs session.start's resume_delegated.
func (s *Service) resumeCheck(ctx context.Context, self string, am Device, c proto.Session, why string, task *sendTask,
	forOwner bool) (delegated bool, err error) {
	cause := proto.StopUnknown
	nodeTakes := false
	if c.Capabilities != nil {
		cause = c.Capabilities.StopCause
		nodeTakes = c.Capabilities.DelegatedResume
	}
	if c.Archived {
		// Archive snapshots may predate the stop or a later user close. Only the node's saved cause authorizes resume.
		var info struct {
			StopCause proto.StopCause `json:"stop_cause"`
		}
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := s.call(cctx, am.ID, "session.info", map[string]any{"session_id": c.SessionID}, &info)
		cancel()
		if err != nil {
			return false, resumeFailed(c, am, fmt.Errorf("could not verify the node's stop cause: %w", err))
		}
		cause = info.StopCause
	}
	if !cause.AllowsSupervisorResume() {
		if !forOwner || !cause.AllowsDelegatedResume() || !nodeTakes {
			s.noteManualResume(am.ID, c.SessionID) // the next decision on its task links its terminal (decision_terminal.go)
			return false, errors.New(agentguard.ResumeRefused(agentLabel(c), am.Name, cause, forOwner, nodeTakes))
		}
		delegated = true // an Agent that ended without an owner's Stop, resumed for the owner (node resume_delegated)
	}
	if why == "" {
		return false, fmt.Errorf("resume_why is required: say why this agent's own context is needed (the user sees it). Nothing was started or sent")
	}
	if d, ok := s.device(ctx, am.ID); !ok || !d.Reachable {
		return false, resumeFailed(c, am, machineOffline(am.Name))
	}
	if contextFull(c) && !agentguard.Compactable(c) {
		return false, resumeFailed(c, am, fmt.Errorf("its context is nearly full (%d of %d tokens) and %s, so it cannot be compacted "+
			"before your text goes in", c.ContextTokens, c.ContextWindow, agentguard.NotCompactable(c)))
	}
	if task != nil {
		return delegated, nil // sendTaskAllowed checks it next, as for any hand-off
	}
	if err := s.ensureTodosLoaded(ctx); err != nil {
		return false, err
	}
	t, ok := s.openTodoOfAgent(am.ID, c.SessionID)
	if !ok {
		// The owner's own turn may wake the owner's session with no task: nothing is made, nothing is supervised.
		if a, _ := ownerfence.ActionOf(ctx); agentguard.PlainResume(s.isMain(ctx, self), a.Owner.Seq != 0) {
			return delegated, nil
		}
		return false, errors.New(agentguard.NoTaskRefusal(agentLabel(c), s.isMain(ctx, self)))
	}
	if err := othersTask(c, t, self); err != nil {
		return false, fmt.Errorf("%w; it is not yours to resume. Nothing was started or sent", err)
	}
	return delegated, nil
}

// contextFull: what the model saw in c's latest reply fills proto.ContextNearlyFullShare of its window (both known).
func contextFull(c proto.Session) bool {
	return proto.ContextNearlyFull(int(c.ContextTokens), int(c.ContextWindow))
}

// compactFailed is a send whose compaction of a nearly full context failed (the node's compact_failed): nothing was
// sent; where the agent stands and what to do instead. resumed: the agent was resumed for it. The node says when the
// agent quit meanwhile (agent_quit): then it is stopped again, recorded as compact_failed, which the assistant may
// resume (#11441); the receipt never says it rests when it does not run.
func compactFailed(c proto.Session, am Device, err error, resumed bool) error {
	quit := agentQuit(err)
	state := agentguard.CompactFailedState(resumed, quit)
	return fmt.Errorf("could not compact %s on %s before your text: %w. %s; nothing was sent. Send again to try once "+
		"more, or, if it cannot be compacted, start a replacement (read this one with agent_read and give it what it "+
		"needs): for its task, task_plan with continues (the task's todo id) and continues_why; for work on no task, "+
		"agent_start in its directory", agentLabel(c), am.Name, err, state)
}

// agentQuit: a node's compact_failed says the agent quit while it was compacted (refusal data agent_quit).
func agentQuit(err error) bool {
	var re *rpc.Error
	if !errors.As(err, &re) || len(re.Data) == 0 {
		return false
	}
	var d struct {
		AgentQuit bool `json:"agent_quit"`
	}
	return json.Unmarshal(re.Data, &d) == nil && d.AgentQuit
}

// compactSendWait bounds a send that may compact first: a compaction takes minutes (node compactBeforeSendDeadline).
const compactSendWait = 15 * time.Minute

// resumeFailed is a resume that did not happen, with why and what to do instead.
func resumeFailed(c proto.Session, am Device, err error) error {
	return fmt.Errorf("could not resume %s on %s: %w. Nothing was sent; start a replacement instead (read this one with "+
		"agent_read and give it what it needs): for its task, task_plan with continues (the task's todo id) and continues_why; "+
		"for work on no task, agent_start in its directory", agentLabel(c), am.Name, err)
}

// resumeParams are the node's session.start params for resuming c with text as its first message: the user's Resume
// (the node fills in what the session ran with) plus the prompt and an eligibility recheck on that node; delegated:
// the main conversation's resume for the owner of an Agent that ended without an owner's Stop (resumeCheck).
func resumeParams(c proto.Session, text string, delegated bool) map[string]any {
	p := map[string]any{"resume_session_id": c.SessionID, "prompt": text, "resume_automatic": true}
	if delegated {
		p["resume_delegated"] = true
	}
	return p
}

// resumeAndSend resumes stopped agent c of am with params' text as its first message (resumeParams) and says so in
// conversation self: on task's card when the send made or found one, else on the open task of self's it is on.
// handed is what the receipt says of the attachments. The text is noted before it goes, as any agent_send's
// (noteAgentSend, #7539).
func (s *Service) resumeAndSend(ctx context.Context, self string, am Device, c proto.Session, delegated bool, params map[string]any, why, told, handed string,
	task *handedTask) (string, error) {
	text, _ := params["text"].(string)
	wait := 5 * time.Minute // a resume can take minutes
	if contextFull(c) {
		wait += compactSendWait // and then a compaction (#11137)
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), wait)
	defer cancel()
	sendNote, err := s.noteAgentSend(sctx, self, am.ID, c.SessionID, "", text) // more from its task's conversation reopens an accepted task (#6726, #7539)
	if err != nil {
		return "", task.sendFailed(sctx, s, err)
	}
	var res struct {
		Session proto.Session `json:"session"`
	}
	full := contextFull(c) // resumeAllowed let only a compactable one through
	startParams := resumeParams(c, text, delegated)
	if full {
		// Resumed without a first message, then the text sent with compact_if_full: the node compacts it with its own
		// /compact first and sends once that is done (#11137).
		delete(startParams, "prompt")
	} else if id, ok := params["client_msg_id"]; ok {
		startParams["client_msg_id"] = id
	}
	err = s.call(sctx, am.ID, DeviceSessionStart, startParams, &res)
	if err != nil {
		if full && sendUnknown(err) {
			// This start carried no prompt. Keep supervision of the possibly resumed agent, but no turn is
			// attributed to text that the second call never sent.
			sendNote.sent(sctx, rpc.NotSent(err))
			if task != nil && task.made {
				s.taskCard(sctx, task.self, task.todo.ID, task.wake)
			}
			return "", fmt.Errorf("%w (the agent may have resumed without a first message; check with agent_read before resuming again)", err)
		}
		sendNote.sent(sctx, err)
		if sendUnknown(err) {
			return "", task.sendFailed(sctx, s, err)
		}
		return "", task.sendFailed(sctx, s, resumeFailed(c, am, err))
	}
	compacted := ""
	if full {
		send := maps.Clone(params)
		send["compact_if_full"] = true
		var sent struct {
			Compacted bool `json:"compacted"`
		}
		err = s.call(sctx, am.ID, DeviceSessionSend, send, &sent)
		sendNote.sent(sctx, err)
		if err != nil {
			if sendUnknown(err) {
				return "", task.sendFailed(sctx, s, err)
			}
			if errCode(err) == proto.CodeCompactFailed {
				return "", task.sendFailed(sctx, s, compactFailed(c, am, err, true))
			}
			return "", task.sendFailed(sctx, s, fmt.Errorf("resumed %s on %s, but the text did not go in: %w", agentLabel(c), am.Name, err))
		}
		s.logf("resume %s on %s: context %d of %d, compacted before the text: %v", short(c.SessionID), am.ID, c.ContextTokens, c.ContextWindow, sent.Compacted)
		if sent.Compacted {
			compacted = fmt.Sprintf(" Its context was nearly full (%d of %d tokens), so it compacted it with its own /compact first.", c.ContextTokens, c.ContextWindow)
		}
	} else {
		sendNote.sent(sctx, err)
	}
	s.logf("resume %s on %s for %s: %s", short(c.SessionID), am.ID, self, why)
	note := s.resumeNote(sctx, c, am, why)
	said := ""
	if task != nil {
		said = task.sent(sctx, s, told)
		s.noteResume(sctx, self, task.todo.ID, am.ID, c.SessionID, note)
		s.ensureSupervisor(sctx, task.todo.ID) // made or kept by adoptAgent; never left without a supervisor
	} else if t, ok := s.openTodoOfAgent(am.ID, c.SessionID); !ok {
		said = agentguard.PlainResumeReceipt // allowed only for the owner's own turn (resumeCheck)
	} else {
		s.ensureSupervisor(sctx, t.ID) // its task goes on with a supervisor, as a successor's does (tasks_successor.go)
		s.releaseHold(sctx, t.ID)      // its supervisor sent it on: follow-ups go again (decision.go)
		if _, err := s.appendFeed(sctx, self, feedRouted, t.ID, map[string]string{"text": told, "from": routedFromAssistant,
			"machine_id": am.ID, "session_id": c.SessionID}); err != nil {
			s.logf("task %s: routed feed: %v", t.ID, err)
		}
		s.noteResume(sctx, self, t.ID, am.ID, c.SessionID, note)
		said = fmt.Sprintf(" It works for your task %s (%q).", t.ID, t.Title)
	}
	return fmt.Sprintf("Resumed %s on %s (same session, with its context, on its own launcher and permission mode) and sent.",
		agentLabel(c), am.Name) + compacted + handed + said + sendNote.reopenWords(), nil
}

// resumeNote is the line the user reads about a resume: which agent, where, and why.
func (s *Service) resumeNote(ctx context.Context, c proto.Session, am Device, why string) string {
	return i18n.T(s.lang(ctx), "assistant-tasks.resume_note", i18n.Args{"agent": agentLabel(c), "machine": am.Name, "why": why})
}

// noteResume puts the resume's note on task id's card in conversation self, as Persona's, naming the run it resumed
// (machine, sid; #10465).
func (s *Service) noteResume(ctx context.Context, self, id, machine, sid, note string) {
	if _, err := s.appendFeed(ctx, self, feedRouted, id, map[string]string{"text": note, "from": routedFromAssistant,
		"machine_id": machine, "session_id": sid}); err != nil {
		s.logf("task %s: resume feed: %v", id, err)
	}
}

// cleanWhy is resume_why as kept: one line, clipped.
func cleanWhy(why string) string {
	return clip(oneLine(strings.TrimSpace(why)), 300)
}
