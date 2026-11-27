# Starting an Agent with `agent_start`

The tool's description holds what every call needs. This page holds the rest: launchers, worktrees, secrets and what Persona refuses. A refusal says what to change; never send the same call again unchanged.

## When to use it

- Start an Agent directly only when the owner asks for that. Planned work goes through `task_plan` ([Planning tasks](task-plan.md)), which gives it a to-do, a brief and supervision; an Agent from `agent_start` has no to-do until `agent_supervise` adopts it.
- Check `machines_list` for the computer, and `agents_list` for an Agent that already has the context: follow-up on the same computer and directory goes to it with `agent_send`.
- The answer gives the new session id.

## Launchers and models

- The launcher decides the Agent. Claude Code's starts Claude Code. pi or cursor (or a launcher listed as that agent) starts pi or the Cursor CLI in a herdr pane with the model you give. agy starts the Antigravity CLI on the computer's companion when it is connected, otherwise in a pane.
- `launcher`: a launcher listed for that computer; an unknown name is refused with the choices. Empty takes the default, then the fallback, Claude Code first, and the answer says which.
- Claude-family launchers, and Paseo providers with Auto Accept, always start in bypassPermissions. agy runs in Allow all by default.
- `effort`: Claude Code, Codex and agy take it; pi and cursor ignore it; TRAE CLI and omp refuse one you name, so leave it out for them.
- `tier`: an Agent preset from the snapshot's Model profile line, chosen by purpose, resolved before approval and shown on the card. An explicit `launcher`, `model`, `effort` or `speed` overrides it; omit them for the tier's or the purpose's default.
- `speed`: `standard` (or omitted) is the launcher's default; Codex takes `fast` and `ultrafast`, Claude Code `fast` only.
- `host`: `agy` for the Antigravity companion, or an enabled engine.
- When the launcher failed its last health check on that computer, the Agent starts on a computer where it works (a directory under the home moves to that home) and the answer says so. When no other computer has it healthy, it starts there anyway and the answer says that too.

## A worktree of its own

- Give `worktree.branch` when the work needs a branch and checkout of its own, to work in parallel with what is already open in that repository. Persona makes the worktree through the computer's engine first (herdr's or Orca's own, so it is listed there and in Persona, under the workspace `cwd` is in) and starts the Agent in it.
- `cwd` is then a directory of the repository to branch from. An existing local branch is checked out; a new one starts at the HEAD of the workspace `cwd` is in. A branch already checked out elsewhere is refused.
- Never tell the Agent to run `git worktree add` itself: a worktree made that way is invisible to Orca and to Persona.

## Secrets

- `secret_env` lists each environment variable the Agent should have (at most 8), with the secret-provider reference its value comes from, such as `op://vault/item/field`, never the secret itself.
- The owner approves the hand-off on a confirmation card in Persona before anything starts. The values reach only that Agent's environment, for this run, and you never see them. The answer lists each variable as granted, declined, expired or failed.
- Only Claude Code Agents (plain `claude`, or a provider Persona manages) on a computer this manager connects take secrets, and only while that computer's node is online, current and resident.
- Tell the Agent the variables exist; never ask it to print them.

## ultracode

- Give `ultracode: true` for a large task: one that spans many files or needs checking from several angles (design, tests, review). Claude Code then runs every substantial step as a multi-agent workflow: slower and costlier, so leave it off for small work. Do not write the word "ultracode" into the prompt instead.
- Only Claude Code 2.1.284 or later takes it. Another Agent, an older Claude Code or a node too old for it starts without it, and the answer says so. Once Claude Code confirms it, `agents_list` shows `ultracode=on`.

## Attachments

`attachments` are absolute paths of pictures and files the owner gave in this conversation (a Claude Code path or a Persona attachment). They are copied to the Agent and appended to its first message with the mentions updated; any other path is refused.

## Refusals to follow

An organization policy may keep some conversations read-only: a call from such a conversation is refused with the policy's reason. Do not retry; follow the refusal and tell the owner.
