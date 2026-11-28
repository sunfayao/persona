package agentguard

import (
	"fmt"

	"github.com/sunfayao/persona/server/internal/proto"
)

// StoppedRefusal is agent_send's answer for the stopped Agent called label sent a plain message: nothing was sent, and
// how to choose.
func StoppedRefusal(label string) string {
	return fmt.Sprintf("%s has stopped: its conversation and terminal stay, and a plain message does not start it again. "+
		"Nothing was sent. Choose how to go on: when the next work depends on what this agent already knows (it has just "+
		"investigated this and should now fix it, rework of its own work, the user asked for the agent with the context), "+
		"send again with resume true and resume_why, which resumes this same session with its context on its own "+
		"launcher and permission mode (also give task, unless it is on an open task of yours). "+
		"A nearly full context is no reason to start over: the resume compacts it with the agent's own /compact before your "+
		"text goes in. For unrelated work, or when it went off track, start a replacement and give it what "+
		"it needs (read this one with agent_read): for its task, task_plan with continues (the task's todo id) and "+
		"continues_why, which keeps the task's todo and brief; for work on no task, agent_start in its directory. "+
		"When you cannot tell, ask the owner (ask_user, or task_review needs_user): reuse this agent, or start a new one", label)
}

// ResumeRefused is why stopped Agent label on machine may not be resumed by this caller (forOwner: the main
// conversation's agent_send; nodeTakes: its node takes a resume for the owner), and what to do instead. Nothing was
// started or sent.
func ResumeRefused(label, machine string, cause proto.StopCause, forOwner, nodeTakes bool) string {
	switch {
	case cause == proto.StopUser:
		return fmt.Sprintf("%s was stopped by the owner (stop cause is user): only the owner's Resume on its terminal page brings "+
			"it back. Ask the owner to press Resume there (give that instruction separately from the choices on a decision "+
			"card), or start a replacement for the work. Nothing was started or sent", label)
	case cause.AllowsDelegatedResume() && !forOwner:
		return fmt.Sprintf("%s ended without the owner's Stop (stop cause is %s): only the main conversation's own agent_send may "+
			"resume it for the owner, or the owner with Resume on its terminal page. Nothing was started or sent", label, cause)
	case cause.AllowsDelegatedResume() && !nodeTakes:
		return fmt.Sprintf("%s ended without the owner's Stop (stop cause is %s), but the node on %s is older than resuming such an "+
			"Agent for the owner: ask the owner to press Resume on its terminal page, or to update that computer's node "+
			"(machine_redeploy), or start a replacement for the work. Nothing was started or sent", label, cause, machine)
	}
	return fmt.Sprintf("%s cannot be resumed by the assistant: stop cause is %s. Only the owner's Resume on its terminal page "+
		"brings it back; give that instruction separately from the choices on a decision card. Nothing was started or sent", label, cause)
}

// CompactFailedState is where the Agent stands after a compaction of its nearly full context failed: resumed says it
// was resumed for the send, quit that the node saw it quit meanwhile (the receipt never says it rests when it does not
// run).
func CompactFailedState(resumed, quit bool) string {
	switch {
	case resumed && quit:
		return "It was resumed, but its process quit again (see what its terminal shows above); it is stopped as " +
			"compact_failed, so you may resume it again once that is dealt with"
	case resumed:
		return "It was resumed and runs with its context as it was"
	case quit:
		return "Its process quit (see what its terminal shows above); it is stopped as compact_failed, so you may " +
			"resume it once that is dealt with"
	}
	return "It keeps running with its context as it was"
}
