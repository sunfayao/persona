# Scheduling with `schedule_followup` and `schedule_routine`

The tools' descriptions hold what every call needs. This page holds the rest: timing rules, where a run goes, pushes and the Inbox, and the fields that change a scheduled task. A refusal says what to change; never send the same call again unchanged.

## Which tool

- `schedule_followup`: a one-off reminder or follow-up ("remind me at 15:00", "check again in an hour", "tell me when X finishes"), or work repeated on an interval ("every 30 minutes").
- `schedule_routine`: work at a time of day, week or month ("every morning at 8", "weekdays at 18:00", "the 1st of each month").
- Get the owner's consent before creating or changing recurring work. `list_followups` lists both kinds with their ids.

## When it runs

- Times are in the owner's time zone. Check the next runs listed in the reply.
- `at` (follow-ups): an RFC 3339 time or a relative `30m`, `2h` or `1d`. With `every` it is the first run, by default one interval from now.
- `every` (follow-ups): from 5 minutes to 7 days. Set it once and never schedule the next run yourself: Persona runs a repeating task again on its own, and a run that schedules its next run under the same title or prompt is folded into the task.
- `time` and `weekdays` (routines): `HH:MM` on the 24-hour clock, with weekdays 1 = Monday … 7 = Sunday; omit them for every day.
- `cron`, instead of `at`/`every` or `time`/`weekdays`: five fields, minute hour day-of-month month day-of-week, such as `*/30 9-18 * * 1-5` or `0 10 1 * *`, or `@hourly`, `@daily`, `@weekly`, `@monthly` or `@yearly`. Runs are at least 5 minutes apart. Day 0 and day 7 are both Sunday, and when both day fields are restricted a day matching either runs. A seconds field, `@every` and `TZ=` are refused: for "every N minutes" use a follow-up's `every`.
- `until`: an optional end, an RFC 3339 time or `30m`, `2h` or `1d` from now, never a bare clock time. After it Persona turns the task off and records a skipped run instead of running it. Omit it to keep the current end; `null` clears it. A one-off ignores it.

## Where a run goes

- A follow-up's prompt comes back to this conversation at that time; with `new_conversation: true` it starts a conversation of its own. Each run of a routine opens its own conversation.
- `fresh_each_run: true` starts a new conversation for every recurring run, with the task's state note carried from run to run.
- Write the prompt self-contained: the run reads it without this conversation's context.
- A scheduled wake resets the conversation's message hop chain, at most once every 10 minutes.

## Pushes, the Inbox and reports

- `push`: `every` (the default for a new task) pushes each run's answer to the owner; `asked` pushes only what a run sends with `notify_user`, for a task that watches something or a standing loop (a patrol, a daily round). A follow-up with `asked` that continues this conversation does not push or send its runs' answers to channels; one asked from Main chat runs in its own conversation instead. Omit it to keep the current value.
- `inbox`, independent of `push`: `off` (the default for a new task); `worthy`, where a run posts a row with `inbox_post` only when it finds something worth a look, for a task that watches something; or `every`, where every run ends with a row Persona makes, for a briefing the owner follows daily. Set it only when the owner asked for Inbox rows. Omit it to keep the current value.
- `report` (routines): a daily report. Each run opens with Persona's north-star lines (today's interruptions and availability, the change against yesterday until the same time, and their source), and the report starts with them. Omit it to keep the current value.

## Launcher, model and effort

- Runs use Main chat's launcher, model and effort. `launcher` gives a task its own, by a name on the computer where its runs start; empty clears it and follows Main chat again; omit it to keep the current one. A follow-up with its own launcher needs `new_conversation: true`: one that continues this conversation runs on this conversation's launcher.
- `model` is for that launcher (empty: the launcher's default); `effort` is thinking effort for Claude launchers (empty clears it). Without a `launcher`, either one pins the launcher the task runs on now. Omit them to keep the current values.
- The permission mode stays the owner's choice.

## Changing or pausing a routine

- Pass `id` from `list_followups` and only the fields to change; the others stay as they are. `enabled: false` pauses the routine and `true` resumes it.
- `run_if` is an owner-approved shell check on the routine's computer before each scheduled run: exit 0 runs, exit 1 skips, and any other failure follows `on_error` (`run` by default, or `skip`), within `timeout_s` (1 to 60 seconds, 30 by default). The owner approves the command on a card whenever you give `run_if`; without that approval the routine is not changed. `null` removes the check; omit it to keep it.

## Recurring work on a supervised to-do

- For approved recurring work that advances a supervised to-do, pass its explicit `todo_id`; never guess it from a title. This conversation must supervise that to-do. Leaving `todo_id` out keeps the association, `""` clears it, and a one-off follow-up refuses it.
- From the second run Persona checks for progress: new Agent answers, tool calls (not waits or repeats), criterion changes or progress reports. After a long stall it asks the owner to Change approach or Keep waiting, even with `push: asked`.
