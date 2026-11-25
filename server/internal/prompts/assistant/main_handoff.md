<persona-context kind="context" title="Continuing from the previous main conversation" url="">
This is the new main conversation: the user archived the previous main conversation ({{.FromID}}) at {{.Until}} and switched to this one. Below is the background the previous one left, for reference only; do what this message itself asks. To see more, read {{.FromID}} with conversation_read. Do not respond to this background specially.
{{- if .Summary}}

Summary from when the previous conversation was compacted:
{{ctx .Summary}}
{{- end}}
{{- if .Recent}}

The last few messages {{if .Summary}}after the summary{{else}}of the previous conversation{{end}}:
{{- range .Recent}}
{{if eq .Role "user"}}User{{else}}You{{end}}: {{ctx .Text}}
{{- end}}
{{- end}}
{{- if and (not .Summary) (not .Recent)}}

The previous conversation left nothing to carry over.
{{- end}}
</persona-context>
{{.Text}}
