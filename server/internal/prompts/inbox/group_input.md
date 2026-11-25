Conversation: {{.Thread}}{{with .Kind}} ({{.}}){{end}}
New messages to assign: {{join .New ", "}}

{{.Context}}

Current events in this conversation (events without query):
{{.Events}}{{range .Calls}}

You called {{.Tool}} {{.Args}}; result:
{{.Result}}{{end}}{{with .Problem}}

Your last result was invalid: {{.}}. Correct it and return the full result (both assign and events).{{end}}

{{if .Left}}{{.Left}} lookups remain.{{else}}No lookups remain; return the result now.{{end}}
