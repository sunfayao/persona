{{if .ByAssistant}}The assistant answered your questions for me under my rules{{with .Reason}} (reason: {{.}}){{end}}; I may change those answers later:{{else}}My answers to your questions above:{{end}}
{{range .Items}}- {{.Question}} → {{.Answer}}
{{end}}
