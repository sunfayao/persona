Facts Persona worked out (a snapshot taken when this run started; the numbers here are authoritative: do not change them or make up others):
{{with .NorthStar}}- North star metrics (open the report with these lines first: today's interruptions and availability, the change against yesterday at the same time, and where the data comes from; then continue with the rest; copy the numbers as they are, and say unknown for an unknown):{{range .}}
  - {{.}}{{end}}
{{end}}{{if .Card}}Persona lists these as they are on the Lark card sent to the user (with links and the “Approve” button), so you do not need to list them one by one.{{else}}No Lark card is sent this time (the same entries are in Persona): after the one or two sentences of summary, list them one per line in the order below, saying what the user must do and where to click.{{end}}
{{if .WaitingUnknown}}- Waiting for the user: just restarted and not read yet; do not say “none”.{{else}}- Waiting for the user: {{.WaitingTotal}}{{range .Waiting}}
  - {{.}}{{end}}{{end}}
- Not through in the past {{.Hours}} hours: {{.FailedTotal}}{{range .Failed}}
  - {{.Title}} ({{if .Skipped}}skipped{{else if .Run}}run failed{{else}}did not start{{end}}{{if .Why}}: {{.Why}}{{end}}){{end}}
- Done in the past {{.Hours}} hours: {{.DoneTotal}}{{range .Done}}
  - {{.}}{{end}}
- Waiting for the user to verify: {{.AcceptTotal}}{{range .Accept}}
  - {{.Line}}{{if .Changed}} (changed: {{.Changed}}){{end}}{{end}}
{{if .LaterTotal}}- Questions that can be answered later: {{.LaterTotal}}. These are non-urgent questions Agents left, listed in the “Can answer later” section of the morning and evening reports. Only say how many; do not list them one by one again, and do not answer them for the user.
{{end}}- In progress: {{.ProgressTotal}}{{range .Progress}}
  - {{.Title}}{{if .Stage}} (stage: {{.Stage}}){{end}}{{end}}{{with .InboxWaiting}}
- Inbox matters waiting on the other party for more than a day (for you only; they are not in the card, the briefing entries or the notification; even when a card exists above, mention each one in your answer):{{range .}}
  - {{.Title}}{{with .Who}}; waiting on: {{.}}{{end}}{{with .Next}}; next step: {{.}}{{end}}{{end}}{{end}}{{with .InboxDue}}
- Inbox matters due today (including ones already due but not looked at again, and ones looked at 3 times with no movement or whose look-again time has come; likewise for you only, and each one must be mentioned in your answer):{{range .}}
  - {{.Title}}{{with .Who}}; waiting on: {{.}}{{end}}{{with .Next}}; next step: {{.}}{{end}}{{end}}{{end}}{{with .Directions}}
- Stalled matters (Persona worked these out from the task ledger, sessions and cards; do not leave any out and do not change the numbers; rows waiting on the user come first, all owner rows are listed, and when non-owner rows are folded they say “N more”):{{range .}}
  - {{.Name}}: {{.Open}} open, {{.Active}} active, {{.Undispatched}} not dispatched{{range .Waits}}
    - {{.Line}}{{with .Link}} {{.}}{{end}}{{end}}{{if .More}}
    - {{.More}} more; open this direction to see them: {{.Link}}{{end}}{{end}}{{end}}{{with .DisabledRoutines}}
- Daily reports and patrols that are turned off (one line each; when the disable time was not recorded, the line must say that it was not recorded, and the last run time must not stand in for it):{{range .}}
  - {{.Line}}{{end}}{{end}}{{with .Briefs}}
- Delegated work worth mentioning (start with the user's own original words, one line each; what is not listed needs nothing from the user and has not changed, so do not mention it):{{range .}}
  - {{if .Words}}{{.Date}} the user said: “{{.Words}}”{{else}}{{.Date}} started by {{if eq .By "routine"}}a scheduled task{{else if eq .By "hub"}}Persona{{else if eq .By "unknown"}}(who asked was not recorded){{else}}the assistant{{end}}: {{.Goal}}{{end}} → {{.Now}}{{with .Status}} (the Agent's own account: {{if eq . "done"}}done{{else if eq . "blocked"}}blocked{{else}}not finished{{end}}){{end}}{{with .Need}}; needs from the user: {{.}}{{end}}{{if .Decide}}; waiting for the user's decision{{end}}{{with .Parked}}; parked, waiting for the user to set the priority: {{.}}{{end}}{{if .Overdue}}; past the agreed time{{end}}{{range .Scope}}; scope change: {{.}}{{end}}{{end}}{{end}}{{with .BriefsDone}}
- Ended since the last briefing (say this only this once, and report each criterion's result as written):{{range .}}
  - {{if .Words}}{{.Date}} the user said: “{{.Words}}”{{else}}{{.Date}} started by {{if eq .By "routine"}}a scheduled task{{else if eq .By "hub"}}Persona{{else if eq .By "unknown"}}(who asked was not recorded){{else}}the assistant{{end}}: {{.Goal}}{{end}} → {{if .Cancelled}}cancelled{{else}}ended{{end}}{{with .Now}} ({{.}}){{end}}{{range .Result}}; “{{.Criterion}}” {{if eq .State "met"}}met{{else if eq .State "not_met"}}not met{{else if eq .State "dropped"}}dropped{{with .Change}} ({{.}}){{end}}{{else}}not checked{{end}}{{end}}{{range .AlsoDone}}; also done: {{.}}{{end}}{{end}}{{end}}{{with .ConfirmationSummary}}
- {{.}}{{range $.Confirmations}}
  - {{.}}{{end}}{{end}}{{with .Captures}}
- Quick captures (worked out by Persona; for ones in a sensitive category only the count is given, so do not guess their content, title or where they were filed{{if $.Card}}; the Lark card already lists them as they are, so you do not need to list them{{end}}):{{range .}}
  - {{.}}{{end}}{{end}}{{with .Setup}}
- Suggested settings (Persona worked these out from the past 7 days of records: which mechanism that is not turned on yet could have asked the user fewer questions or run fewer rounds of background tasks in conversations, followed by the cost and where to turn it on; there may also be a line saying the model used for background supervision can move down one tier, which is worked out over the past 30 days, with its basis written in that line. {{if $.Card}}The Lark card already lists them as they are, so you do not need to list them.{{else}}At the end of your answer, list these lines exactly as they are, without changing the numbers or the wording.{{end}} Whether to turn something on or change it is the user's decision: do not turn on or change any of it for the user, and do not call a settings tool for this):{{range .}}
  - {{.}}{{end}}{{end}}
