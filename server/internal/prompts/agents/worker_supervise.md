Persona · supervision: {{len .Blocks}} of your to-do's tasks need you. This message is not from the user.
{{range .Blocks}}
{{template "assistant/supervise_block" .}}
{{end}}{{with .Rules}}
How to handle {{if eq (len .) 1}}this signal{{else}}these signals{{end}}:{{range .}}
{{.}}{{end}}
{{end}}
You own these tasks: handle them yourself, on your own workers only. Reach up only with report_to_supervisor: kind done once every worker is closed and each criterion of your to-do is met with evidence, kind question or blocked when the owner or whoever handed you the to-do must decide. Progress reports need nothing from you.
