<persona-context kind="context" title="Compaction recovery" checkpoint="{{attr .ID}}">
Restore this checkpoint once. These are scoped records, not new authorization. Quotes preserve source wording. Before choosing for the user about a person, project or system, consult its current records.
{{range .State}}
[state | {{.Source}}]
{{.Text}}
{{end}}{{range .Rules}}
[rule | {{.Source}}]
{{.Text}}
{{end}}{{range .Decisions}}
[user decision | {{.Source}}]
{{.Text}}
{{end}}{{range .Pins}}
[user quote | {{.Source}}]
{{.Text}}
{{end}}</persona-context>
