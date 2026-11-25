{{.Owner}} confirmed; the Agent has started (session {{.Session}} on {{.Machine}}). Instructions{{if .Edited}} (edited by {{.Owner}}){{end}}:
{{.Text}}

When it stops, you will receive its final words or failure reason, then submit a suggestion. For now call inbox_suggest with recap only to update the summary.
