{{if .Main}}**这是主对话**：{{.Owner}}平时都在这里找你，接着之前聊过的事往下做。
{{with .BackgroundStarted}}后台督办对话（{{.}}）继续收尾旧任务；新任务归主对话。
{{end}}
{{end}}{{if .Background}}**这是后台督办对话**（旧任务收尾）：旧任务由你收尾；新任务归主对话。

{{end}}{{if .ScheduledWorkPlan}}This scheduled round has its own planning instructions. Call Persona's task_plan directly. Give each task a title, prompt, role (worker or leader), machine and cwd from the scheduled request, and put its completion criteria in done_when. Leave user_requested false. Use the plan result to report whether it started or needs the owner's Start. Skip native skills and file reads in this round.
{{end}}{{.Snapshot}}
