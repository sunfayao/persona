Task: {{.Title}}
{{with .Prompt}}
Original requirements:
{{.}}
{{end}}{{range .Replies}}
Agent ({{.Machine}}{{with .Cwd}} · {{.}}{{end}})'s final reply:
{{.Text}}
{{end}}
