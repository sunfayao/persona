The coding Agents on the user's computers (Claude Code, Codex, Antigravity, TRAE and runtime Agents) and their state: needs_you (waiting for an answer or approval), working, unread (finished, result not looked at), idle, offline (stopped). Much faster and more accurate than ssh + herdr. The default lists Active (needs you, working, finished unread), then Idle and Ended in the last 24 hours, newest first: look at those two before handing out follow-up work, since an Agent that did the related work on that computer and directory goes on in its own context (reuse= says how). Each line has machine, session id, project, title and last update, then its conclusion or last words. Do not reuse an Agent on a model the user retired (model=). Let one with ultracode=on finish its multi-agent workflow without nudging it. A line under an Agent waits for the user: answer a question with interaction_answer and its interaction id; an approval is the user's to give in the app. `kind: "conversations"` lists your own Persona conversations instead, newest activity first (id, title, last activity, main/running state, gist; this one marked; `query`, `include_archived`, `limit`): find the user's conversation there before reading or messaging it.

Details: read .claude/skills/persona-work/references/agents-and-machines.md in your working directory.

```json
{
  "type": "object",
  "properties": {
    "state": {
      "type": "string",
      "enum": ["active", "needs_you", "working", "unread", "idle", "ended", "done", "muted", "archived", "all"],
      "description": "default active (then idle and those that ended in the last 24 hours); ended = every stopped Agent; done = work finished in the last 24 hours; muted = the muted ones; archived = only archived sessions (agent_archive with restore brings one back), which the others leave out; all = everything, archived ones marked"
    },
    "machine": {
      "type": "string"
    },
    "query": {
      "type": "string",
      "description": "filter by title / project / directory; conversations: by title or last message"
    },
    "kind": {
      "type": "string",
      "enum": ["agents", "conversations"],
      "description": "agents (default), or your Persona conversations"
    },
    "include_archived": {
      "type": "boolean",
      "description": "conversations: archived ones too, marked"
    },
    "limit": {
      "type": "integer",
      "description": "conversations: default 30, max 100"
    }
  }
}
```
