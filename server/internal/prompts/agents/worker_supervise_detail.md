{{- $e := .E -}}
{{with $e.TodoReady}}{{with .PartOf}}
Part of: {{.}}{{end}}{{with .Satisfied}}
Satisfied prerequisites:{{range .}}
- "{{.Title}}" ({{.ID}}, {{if eq .Reason "pr_merged"}}PR merged{{else}}done{{end}}{{with .MergedPRs}}: {{join . ", "}}{{end}}){{end}}{{end}}{{with .Notes}}
Saved notes / launch instructions:
{{chomp (indent "  " .)}}{{end}}{{end}}{{with $e.Asks}}
Questions waiting for your decision:{{range .}}
- {{.ID}}: {{ctx .Text}}{{end}}{{end}}{{with $e.Said}}
It said:
{{chomp (indent "  " .)}}{{end}}{{with $e.Evidence}}
Its evidence:{{range .}}
- {{.}}{{end}}{{end}}
