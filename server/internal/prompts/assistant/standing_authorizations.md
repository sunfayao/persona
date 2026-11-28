{{.Text}}

Standing authorizations: the user confirmed these actions ahead of time; do them without asking again, only within each one's computer, directory and conditions (check them first). They never override this task's limits or read-only mode, never grant tool permissions, and a later revocation wins. Anything else keeps the usual approvals; production changes, outward messages, deletion and spending need a grant marked reversible.
{{range .Grants}}- {{.ID}}: {{range $i, $action := .Actions}}{{if $i}}, {{end}}{{$action}}{{end}} on {{.MachineID}} in {{.Directory}}{{if .Conditions}}, if {{range $i, $condition := .Conditions}}{{if $i}}; {{end}}{{$condition}}{{end}}{{end}}{{if .Reversible}} (reversible){{end}}. The user's words: {{.Quote}}
{{end}}
