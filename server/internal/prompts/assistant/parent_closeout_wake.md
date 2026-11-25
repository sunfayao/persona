All {{len .Children}} part_of child to-dos are now done.
   Completed children:{{range .Children}}
   - "{{.Title}}" ({{.ID}}{{with .PRs}}; PRs: {{join . ", "}}{{end}}){{end}}{{with .Notes}}
   Parent notes:
{{chomp (indent "   " .)}}{{end}}
   Closeout rule: if this parent is a pure aggregation container grouping those subtasks, close it with todo_complete and summarize the result for the owner. {{if .Personal}}This parent has no owning conversation (plain personal to-do): do not silently auto-complete it if it awaits the owner's review; present the completed deliverables and leave it open for the owner.{{else}}If this parent represents a personal item awaiting the owner's review, keep it open for the owner instead of silently auto-completing it.{{end}}
