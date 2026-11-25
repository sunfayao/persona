{{if .Handled}}Owner reply in {{.Machine}}/{{.Session}}: {{printf "%q" .Text}}. {{with .Closeout}}Agent closeout: {{printf "%q" .}}. {{end}}Owner ask withdrawn. Verify the task's result or clarify; record only an explicit choice.
{{else}}Owner wrote in {{.Machine}}/{{.Session}}; default paused. Read with `agent_read`, then confirm via `task_decided` or clarify.
{{end}}
