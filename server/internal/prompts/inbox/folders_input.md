{{with .Folders}}Existing folders (item counts):
{{range .}}- {{.Path}} ({{.Count}})
{{end}}{{else}}No folders yet.

{{end}}{{with .Examples}}Items the user filed by hand (follow their habits):
{{range .}}- {{.Title}}{{with .Sender}} ({{.}}){{end}} → {{.Folder}}
{{end}}
{{end}}Items to file:
{{range .Items}}
id: {{.ID}}
Type: {{.Kind}}{{with .Source}} ({{.}}){{end}}
Title: {{.Title}}{{with .Sender}}
From: {{.}}{{end}}{{with .Summary}}
Summary: {{.}}{{end}}{{with .Preview}}
Content: {{.}}{{end}}
{{end}}
