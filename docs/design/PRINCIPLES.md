# Persona design principles

Persona is an Agent product for everyone. Assistant is for everyday people and must stay approachable; Agents is for people who collaborate directly with tools such as Claude Code.

**Persona is a personal Agent product for everyone, not only software developers.** Defaults are simple and reliable, complex capabilities open up gradually through settings, and with the user's permission an Agent can help manage that complexity.

- **Assistant** is for everyday people, with no programming or Agent-tool knowledge required.
- **Agents** is for people who want to collaborate directly with Claude Code and other Agents.
- Every design decision starts with **who it serves**. Assistant and other everyday-facing interfaces stay simple and understandable; developer terminology, engineering details and accumulating options must not make them harder to use. Specialist workflows belong in Agents or advanced settings, with details disclosed only when needed.

**Audience test**: answer in one sentence who a design serves. If the answer is "developers", it belongs in Agents or advanced settings, not in an everyday-facing experience. For an everyday-facing flow, check that someone with no programming or Agent-tool knowledge can understand the task and next action without choosing a runtime or repository. For specialist work, name its home in Agents or advanced settings and say why the detail is needed there.

These seven principles decide product questions before any visual rule applies. They cover UI, settings and defaults, and how numbers and states are counted, in the web app and in the hub. Each one has a few rules and a "How to check" that names its evidence. Visual and interaction detail lives in `docs/design/DESIGN_SYSTEM.md`.

## 1. Defaults first

Common tasks work out of the box, with no setup, and the defaults suit most people.

- A new install with only a hub paired can do the frequent tasks: chat with Persona, start an Agent, answer its question, approve a plan or tool.
- A default is stable and fits in one plain sentence. It never changes silently between versions; a change is announced and the old behavior stays reachable.
- A default the user already set elsewhere wins over a new built-in one (for example the permission mode in the computer's own Agent settings).

**How to check**: state the default in one plain sentence; the change works on a fresh install with untouched settings (a screenshot or a test on a clean profile or the mock hub).

## 2. Progressive disclosure

Complex capabilities sit behind settings or an advanced entry and open on request.

- New advanced options default to off and live in Settings or an advanced group, not on the main chat or Agents screens.
- Detail opens on request: tool execution stays one summary line; expert terms and raw values (model IDs, ports, paths, hub, node, MCP) appear only in the advanced area that needs them, or come with a plain explanation (`DESIGN_SYSTEM.md` section 5).
- A new look is an opt-in style; the default style stays pixel-identical.

**How to check**: with the feature off, before and after screenshots of the primary screens are identical; name where the option lives and that it is off by default.

## 3. User control

The user can see, change and undo every behavior; what Persona shows can be explained.

- Every behavior-changing setting is visible in Settings with its current value; no hidden flag whose only switch is a chat command or a file.
- Reversible actions offer Undo; irreversible ones confirm first, with a destructive-styled button and Cancel (`DESIGN_SYSTEM.md` section 4.3). An undo or rollback the user made sticks: no background job silently redoes it.
- Background work stops things and keeps the terminal available. The user can close it by hand, turn on a close setting, or authorize closing the original tab of a particular task when it is accepted.
- When Persona or an Agent changes a setting for the user, it first says in plain words what changes, where and from when, waits for confirmation, records the change and offers a rollback. Every count or status can say what it counts.

**How to check**: for each new setting, name where it is shown and how it is changed back; if an Agent can change it, show the confirmation text and the rollback path (a test or a screenshot). Each new number has a one-line definition in the docs or in the UI.

## 4. Consistency first

Reuse the existing interaction, visual and data patterns; a new pattern needs a written reason.

- Pages use design-system components and tokens only (`DESIGN_SYSTEM.md` sections 2 and 3); the same action looks and sits the same on every screen.
- One label, one definition: a number or state shown under the same name ("Not done", "Needs you") is computed by the same rule on every screen, ideally by one shared function.
- Moving or adding an entry point follows the place similar entries already use; a new component, pattern or definition goes into `DESIGN_SYSTEM.md` (or the module doc) with why the existing one did not fit.

**How to check**: list the components, patterns and definitions the change reused; anything new links its reason in the updated doc. For counts: the same data shows the same number in every place that names it (a test or side-by-side screenshots).

## 5. Model and product are separate

Speed, model, persona and features are four separate settings; changing one never changes another.

- Model choice and speed live in model settings and profiles; product features never hard-code a model or rely on one model's quirks.
- The avatar, theme and interface style are independent; the avatar does not pick a model, and switching models does not change the avatar or the UI.
- Feature switches decide what Persona can do, not which model does it; switching profiles does not turn features on or off.

**How to check**: name which of the four the change touches; a test or manual check shows that switching any other one leaves the change's behavior unchanged.

## 6. Rules are checkable

A principle that no one checks is a wish: every rule here ends in evidence someone can verify.

- A change that touches UI, settings or defaults, or how something is counted, names the principles it touches, each as a checkable statement ("off by default; primary screens unchanged"), and marks each item of the "UI change self-check" in `DESIGN_SYSTEM.md` done, not applicable, or deviated with a reason.
- A violation found later becomes a case below, and, where it can, a test or check that catches it next time.

**How to check**: each item marked done carries its evidence: a screenshot, a test name, or a command and its output.

## 7. Fast, local-first and stateful

Never leave the user waiting without feedback; make the work itself faster, not just its indicator.

- Show local feedback immediately, using local state before network work. A message starts as Sending and becomes
  Processing when the hub receives it, without waiting for scheduling, process startup or the model's first output.
- Receipt is not delivery or execution. Reconcile optimistic state with authoritative outcomes; show queued work,
  uncertain delivery and failure honestly, preserve recoverable input, and never retry an uncertain send as a new one.
- Measure the full path and remove avoidable waits without changing the user's model, permissions or answer quality.

**How to check**: measure send-to-feedback and send-to-execution separately, before and after in an isolated environment.
A received message shows Processing within one second in that environment; attach light/dark screenshots and tests for
failure rollback, delayed execution and stale updates. Name any unmeasured production or model boundary.

## Cases

Examples, so the principles are concrete.

| What happened | Principle | Lesson |
| --- | --- | --- |
| The web sidebar's New chat was moved out of the Persona chats section to its own row under search, then moved back into that section, the list the new chat joins. | 4 Consistency | Make an entry easier to find (label, visibility) in the place similar entries already use; moving it is a new pattern that needs a reason. |
| The main chat's Progress said 86 while the To-dos page's "Not done" said 32: Progress counted every to-do without a done time, the To-dos page left out shelved and accepted ones. Both were then made to use one rule. | 4 Consistency, 3 User control | The same idea needs one definition, and a number has to be explainable; two counts of "open work" that disagree make both untrustworthy. |
| A manual rollback of an extension was reinstalled by auto-upgrade about a minute later; the rolled-back version is now recorded as skipped. | 3 User control | An undo or rollback the user made must stick; background automation never silently reverses it. |
| A finished task's Agent opened in the web app showed "No Terminal": the hub had closed its tab when the task was done, and the node closed any Persona tab left with a bare shell after ten minutes. The Agent is now stopped and its terminal kept, and a guard test pins every place that may close one. | 3 User control | Automation may stop work, but closing what the user may still want to see (a terminal, a tab) is the user's call. |
| Agents started from the main chat always ran in "Ask every step", ignoring the permission mode the user had set in the computer's Agent settings; they now follow that setting. | 1 Defaults first, 4 Consistency | A built-in default must not override a choice the user already made, and the same start should behave the same from every entry point. |

## Using the principles

An agent that changes UI, settings, defaults or a count states which principles the change touches and shows evidence for each: a screenshot, a test name, or command output.
