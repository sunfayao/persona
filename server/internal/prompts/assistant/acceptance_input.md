Task: {{.Title}}

Requirements when it was handed out:
{{.Prompt}}

The Agent's last reply:
{{.Reply}}

Available links:
{{range .Links}}- {{.}}
{{else}}(none)
{{end}}
