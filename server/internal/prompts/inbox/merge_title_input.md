Merged items (newest first):
{{range .Items}}
Type: {{.Kind}}{{with .Source}} ({{.}}){{end}}
Title: {{.Title}}{{with .Sender}}
From: {{.}}{{end}}{{with .Summary}}
Summary: {{.}}{{end}}{{with .Preview}}
Content: {{.}}{{end}}
{{end}}
