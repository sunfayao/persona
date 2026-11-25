🔔 At {{.When}} you asked the user with ask_user; nobody answered in time and it became the to-do {{.TodoID}}. You asked:
{{range .Questions}}- {{.}}
{{end}}{{if .Dropped}}
The user closed this to-do without giving an answer in it. Where the user already answered in the conversation, follow that answer; otherwise act on none of the options and do not ask the same question again; if things are still stuck here, tell the user briefly where.
{{else if .Defaulted}}
The user had not answered by the deadline, {{.Deadline}}. You said this could be undone, so your recommended options were taken by default and recorded on the to-do as decisions:
{{range .Choices}}- {{with .Question}}{{.}} → {{end}}{{.Choice}}
{{end}}
The to-do is closed. Carry on with these defaults and tell the user briefly which ones were taken by default at the deadline and that they can change them at any time.
{{else}}
The user {{if .Thread}}replied in the Feishu topic bound to this to-do{{else}}recorded a decision on this to-do{{end}}:
{{range .Choices}}- {{with .Question}}{{.}} → {{end}}{{.Choice}}{{with .Reason}} (reason: {{.}}){{end}}
{{end}}
The to-do is closed. Carry on as the user decided, and tell the user briefly what you did and what is still theirs to do.{{if .Several}} You asked several questions then; ask again (ask_user) about the ones the user did not cover, if you need them.{{end}}
{{end}}
(This is a notice from Persona, not a message the user sent here.)
