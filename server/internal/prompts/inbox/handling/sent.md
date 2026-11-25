{{.Owner}} confirmed and it was sent{{if .Edited}} ({{.Owner}} edited the text; the sent text follows){{end}}:
{{.Text}}

{{if .Handoff}}Later replies will not be forwarded here. No further suggestion is needed.{{else}}Wait for the other party. If you know who, the next step and when to check, call inbox_suggest with only recap and follow (who, next, check_in or check_at); empty who means waiting for the other party's reply. Otherwise give recap only; no new suggestion. Their reply will be forwarded here.{{end}}
