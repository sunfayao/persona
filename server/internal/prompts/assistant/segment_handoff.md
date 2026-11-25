<persona-context kind="context" title="Conversation continuity" url="">
This is a new execution session of the same conversation. Use the following as background and continue with the current message.
{{if .Partial}}Some history is unavailable.{{end}}
{{if .Summary}}{{ctx .Summary}}{{end}}
{{range .Recent}}{{.Role}}: {{ctx .Text}}
{{end}}</persona-context>
{{.Text}}
