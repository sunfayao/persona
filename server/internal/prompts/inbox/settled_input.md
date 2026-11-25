Event: {{.Title}}{{with .Summary}}
Summary: {{.}}{{end}}

Its messages:
{{.Context}}

Later messages (oldest first):
{{range .Later}}{{with .At}}[{{.}}] {{end}}{{if .Mine}}Me{{else}}{{.Sender}}{{end}}: {{.Text}}{{if .ReplyTo}} (reply {{.ReplyTo}}){{end}}{{if .Thread}} (thread {{.Thread}}){{end}}
{{end}}
