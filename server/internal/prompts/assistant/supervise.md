📋 Supervision: {{len .Blocks}} of the tasks you handed out need you
(This is a supervision message from Persona, not a message from the user.)
{{if .Fresh}}This round usually starts from a clean context: earlier rounds of the conversation may not be in front of you. Whatever must carry across rounds is in the task ledger (brief, scope changes, settled decisions, last verdict); read it with todo_get when needed.
{{end}}{{if .Asked}}Question routing: timeout_minutes={{.AskMinutes}}, low_distraction={{.PushGrading}}.
{{end}}<persona-supervision level="{{if .Level}}{{.Level}}{{else}}triage{{end}}" />
{{if .Fresh}}<persona-supervision-round />
{{end}}{{range .Blocks}}
{{template "assistant/supervise_block" .}}
{{end}}{{with .Rules}}
How to handle {{if eq (len .) 1}}this signal{{else}}these signals{{end}}:{{range .}}
{{.}}{{end}}
{{end}}
