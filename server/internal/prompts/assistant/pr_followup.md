{{if eq .Event "merged"}}✅ The delivery PR for the closed task "{{.Title}}" was merged{{else if eq .Event "closed"}}⌧ The delivery PR for the closed task "{{.Title}}" was closed without merging{{else if eq .Event "conflict"}}⚠️ The delivery PR for the closed task "{{.Title}}" has a merge conflict{{else}}🔁 The delivery PR for the closed task "{{.Title}}" received a changes-requested review at its current head{{end}}: {{.PRURL}}
The task stays closed and will not reopen; this is only a post-closure PR follow-up.
(This is a notice from Persona, not a message from the user. {{if .Realtime}}The user may receive one notification; no reply is needed.{{else}}No reply is needed.{{end}} Delegate another task only if this needs action.)

