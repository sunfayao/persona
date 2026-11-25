(Supervision: acceptance did not pass, rework round {{.Round}}, at most {{.Max}} rounds)
{{with .Checks}}Item-by-item acceptance:
{{range .}}- {{if .Met}}✅{{else}}❌{{end}} {{.Criterion}}{{with .Evidence}}: {{.}}{{end}}
{{end}}{{end}}{{with .Note}}
What you need to do:
{{.}}
{{end}}
Fill in what is missing. When done, give the evidence for each acceptance criterion (PR link, CI result, screenshot, the commands and output of the live check); for anything you cannot do, say why and who has to decide.
