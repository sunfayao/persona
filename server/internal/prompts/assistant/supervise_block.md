{{.N}}. {{if .Own}}Your own to-do{{else if eq .Role "leader"}}Worker{{else}}Task{{end}} “{{.Title}}” ({{.ID}}, {{if .Multi}}signals={{join .Kinds ", "}}{{else}}signal={{index .Kinds 0}}{{end}}{{with .Link}}, link {{.}}{{end}}){{if .Gone}}: it was {{.Gone}} since this signal was raised; nothing is left to do for it{{else}}{{if .Multi}}{{range .Signals}}{{if .Show}}
   - {{.Kind}}: {{.What}}{{if .Detail}}
{{chomp (indent "     " .Detail)}}{{end}}{{end}}{{end}}{{else}}{{range .Signals}}{{if .Show}}: {{.What}}{{if .Detail}}
{{chomp (indent "   " .Detail)}}{{end}}{{end}}{{end}}{{end}}{{if .ShowCriteria}}
   {{if eq .Role "leader"}}Done when{{else}}Acceptance criteria{{end}}{{if .Partial}} (only the changed ones){{end}}:{{range .Criteria}}{{if .Show}}
   {{.N}}. [{{.State}}] {{.Text}}{{with .Evidence}} — {{.}}{{end}}{{with .By}} (by {{.}}){{end}}{{end}}{{end}}{{if .NoCriteria}} {{if eq .Role "leader"}}not written down: judge by what the task asked for{{else}}not recorded{{end}}{{end}}{{end}}{{with .Last}}
   {{if eq $.Role "leader"}}Its last reply{{else}}What the Agent said last{{end}}:
{{chomp (indent "   " .)}}{{end}}{{if .Cut}}
   (cut to keep this message short; the rest: agent_read{{with .Session}} with session_id={{.}}{{end}}){{end}}{{if .Unchanged}}
   Everything else about this task is unchanged since the previous message about it.{{end}}{{end}}
