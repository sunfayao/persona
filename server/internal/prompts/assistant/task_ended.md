🔔 The task “{{.Title}}” that this conversation handed out {{if eq .Verdict "accepted"}}was accepted and is finished{{else}}was cancelled{{end}}{{if .Link}}: {{.Link}}{{else}} ({{.ID}}){{end}}{{if .Note}}
Acceptance note: {{.Note}}{{end}}
(This is a Persona notification, not a message from the user. This task is supervised by the main conversation; only a record is kept here, and no separate notification is sent for it.)
No need to respond, and do not supervise it any more; tell the user the result only if they ask about it here.
