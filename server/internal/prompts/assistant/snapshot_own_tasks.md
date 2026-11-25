{{if or .Tasks .OpenTodosLoaded}}- Tasks this conversation handed out or supervises (tasks handed out by other conversations and personal to-dos are not listed here, use todo_list to see them; recently ended ones are listed too; for existing results look here, and if that is not enough read its conversation with agent_read; use agent_send only to have it look something up, do something new or change direction): {{if not .Tasks}}none
{{else if .Omit.Tasks}}{{len .Tasks}} in total, {{template "assistant/snapshot_omitted" "todo_list"}}
{{else}}
{{range .Tasks}}{{template "assistant/snapshot_task" .}}
{{end}}{{end}}{{end}}{{with .OpenTodos}}- Other open tasks this conversation handed out or supervises ({{if $.Omit.OpenTodos}}IDs only{{else}}IDs and titles{{end}}): {{range $i, $t := .}}{{if $i}}; {{end}}{{$t.ID}}{{if not $.Omit.OpenTodos}} “{{$t.Title}}”{{with $t.Link}} ({{.}}){{end}}{{end}}{{if $t.Failed}} (failed to start){{end}}{{end}}
{{end}}
