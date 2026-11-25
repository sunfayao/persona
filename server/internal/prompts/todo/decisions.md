<persona-context kind="context" title="To-do · {{attr .Title}} · Decisions in effect" url="">
Decisions already settled for this work (only those in effect, oldest first). Follow them and do not ask the same questions again. Ask the user before changing any of them.
{{range .Decisions}}
{{.N}}. {{.Question}}
   Chose: {{.Choice}}{{if .Reason}}
   Reason: {{.Reason}}{{end}}
   ({{.When}}{{if eq .Source "outcome"}}, recorded by the Agent at completion, not decided by the user{{else if eq .Source "earlier-outcome"}}, recorded in an earlier phase; this neither marks the current to-do complete nor represents a user decision{{else if eq .Source "agent"}}, recorded by the Agent during the work, not decided by the user{{else if eq .Source "thread"}}, the user's reply in the Feishu topic{{else if eq .Source "card"}}, the user's answer to a question from {{if .Asker}}“{{.Asker}}”{{else}}an Agent{{end}}{{end}}{{if .Ancestor}}; earlier task “{{.Ancestor}}”{{end}})
{{end}}</persona-context>
