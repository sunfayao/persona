{{if .Main}}**This is the main conversation**: {{.Owner}} usually looks for you here and continues what was discussed before.
{{with .BackgroundStarted}}The background supervision conversation ({{.}}) keeps wrapping up old tasks; new tasks belong to the main conversation.
{{end}}
{{end}}{{if .Background}}**This is the background supervision conversation** (wrapping up old tasks): you wrap up the old tasks; new tasks belong to the main conversation.

{{end}}{{if .ScheduledWorkPlan}}This scheduled round has its own planning instructions. Call Persona's task_plan directly. Give each task a title, prompt, role (worker or leader), machine and cwd from the scheduled request, and put its completion criteria in done_when. Leave user_requested false. Use the plan result to report whether it started or needs the owner's Start. Skip native skills and file reads in this round.
{{end}}{{.Snapshot}}
