The coding Agents on the user's computers (Claude Code, Codex, Antigravity, TRAE and runtime Agents), with their state: needs_you (waiting for an answer / approval), working, unread (finished, result not looked at), idle, offline (stopped). The default view lists three groups: Active (needs you, working, finished unread); Idle (live and at rest, newest first); Ended in the last 24 hours (stopped, newest first). Look at the last two before handing out follow-up work: an Agent that already did the related work on that computer and directory goes on in its own context. done lists work finished in the last 24 hours, ended every stopped Agent newest first, all everything with what goes on first and then the latest, and muted the muted agents that would otherwise appear in the live snapshot. Each line has machine, session id, project, title and last update, and below it the conclusion or the last thing the agent said. launcher= and model= say what it runs on (with effort= when known; do not reuse an Agent on a model the user retired), context= how full its context is, todo= the to-do it works for, or its last one (closed), and reuse= how follow-up work goes on with it: agent_send (live), agent_send resume=true (stopped; stop= says why), or the owner's Resume only (the owner stopped it). usage_limit (with resets_at when the reset is known) marks one its subscription's usage limit stopped (agent_switch moves such an agent to another launcher when the user asks). Known Claude Code switch states appear as ultracode=on/off; an omitted state is unknown. ultracode=on means every substantive task runs a multi-agent dynamic workflow, independently of thinking effort. Let it finish and wait for workflow results without nudging it. Much faster and more accurate than ssh + herdr. At the end: the launchers that failed their last health check, by machine (agent_start and task_plan move off them).

A line under an agent is something waiting for the user: a question lists its options and the interaction id to answer it with (interaction_answer), an approval is the user's to give in the app.

```json
{
  "type": "object",
  "properties": {
    "state": {
      "type": "string",
      "enum": ["active", "needs_you", "working", "unread", "idle", "ended", "done", "muted", "archived", "all"],
      "description": "default active = needs_you + working + unread, then idle and the ones that ended in the last 24 hours; ended = every stopped Agent, newest first; done = finished work in the last 24 hours; muted = muted agents in the live snapshot categories; archived: only the archived sessions (agent_unarchive restores them); the others leave archived ones out, all lists them marked archived"
    },
    "machine": {
      "type": "string"
    },
    "query": {
      "type": "string",
      "description": "filter by title / project / directory"
    }
  }
}
```
