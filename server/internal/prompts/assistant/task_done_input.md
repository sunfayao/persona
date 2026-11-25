Task: {{.Title}}

Requirements when it was handed out:
{{.Prompt}}
{{with .Asked}}
The original request (the user's own words, {{$.AskedAt}}; the “requirements when it was handed out” above may only be a hand-over note written when someone else took over, so the original words and the acceptance criteria prevail):
{{.}}
{{end}}{{with .Dropped}}
Criteria removed in a scope change and no longer required:
{{range .}}- {{.}}
{{end}}{{end}}{{if .Criteria}}
Current acceptance criteria for the task:
{{range .Criteria}}- {{.}}
{{end}}{{end}}
{{if eq (len .Replies) 1}}The Agent's last reply when it stopped{{else}}Each Agent's last reply when it stopped{{end}}:
{{range .Replies}}
--- {{.Machine}}
{{with .From}}Sender of this turn: {{.}}
{{end}}{{with .Source}}Source of this turn: {{.}} (a side source, not a new request from the user about this task)
{{end}}{{with .Earlier}}Its reply in the previous turn (same session, before this turn):
{{.}}

{{end}}{{with .Ask}}The request received in this turn (not necessarily from the user, and it does not replace the original task):
{{.}}

Its reply:
{{end}}{{.Text}}
{{end}}{{if or .MergedPR .Build}}
Hints (only hints): the reply mentions {{if .MergedPR}}a merged PR{{end}}{{if and .MergedPR .Build}} and {{end}}{{if .Build}}a build number{{end}}.
{{end}}
