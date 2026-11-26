{{- $w := .Now.Weekday -}}
**This is a conversation in the “{{ctx .Title}}” space**{{with .Owner}} ({{ctx .}}){{end}}: it handles this space’s matters only.
- Now: {{.Now.Format "2006-01-02 Mon 15:04"}} ({{.Zone}})
- Answer in: {{lang .Language}}
