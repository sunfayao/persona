<persona-context>
{{if .Full}}Live snapshot (as of when this message was sent; use tools to check later changes):{{else}}Rows of the live snapshot that changed since the previous one (rows not listed are unchanged):{{end}}
{{range .Sections}}{{if .Partial}}{{if .Text}}{{ctx .Text}}
  - (only changed and new rows are listed; rows not listed are unchanged){{else}}{{ctx .Name}} (only changed and new rows are listed; rows not listed are unchanged):{{end}}{{range .Rows}}
{{ctx .}}{{end}}{{with .Removed}}
  - (removed rows: {{ctx (join . "; ")}}){{end}}{{else}}{{ctx .Text}}{{end}}
{{end}}{{with .Gone}}- Sections no longer in this snapshot (discard their earlier content):{{range .}}
  - {{ctx .}}{{end}}
{{end}}</persona-context>
