Now: {{.Now}}
Agent: {{.Agent}} ({{.Machine}}{{with .Cwd}}, directory {{.}}{{end}}){{with .Task}}
Its task: {{.}}{{end}}{{with .Decisions}}

The user's decisions on this task:
{{.}}{{end}}{{with .Asks}}

The user's recent requests, oldest first:
{{range .}}- {{.}}
{{end}}{{end}}{{with .Reply}}
Its recent reply:
{{.}}
{{end}}
Its questions:
{{range .Questions}}- {{.Question}}{{if .MultiSelect}} (multiple selections allowed){{end}}
{{range .Options}}  - {{.Label}}{{with .Description}}: {{.}}{{end}}
{{end}}{{end}}
