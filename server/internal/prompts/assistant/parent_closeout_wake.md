All {{len .Children}} part_of child to-dos are now done.
   Completed children:{{range .Children}}
   - "{{.Title}}" ({{.ID}}{{with .PRs}}; PRs: {{join . ", "}}{{end}}){{end}}{{with .Notes}}
   Parent notes:
{{chomp (indent "   " .)}}{{end}}
   Closeout rule: check this parent's own criteria. If they are met, close it with todo_update state done and summarize the result for the owner; if something is missing, plan it.
