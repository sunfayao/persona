{{- $e := .E -}}
{{with $e.Workers}}
Its workers (the hub's record, not the supervisor's words):{{range .}}
- “{{.Title}}” ({{.ID}}{{with .Machine}}, on {{.}}{{end}}): {{.Verdict}}{{with .PR}}, PR {{.}}{{end}}{{if .Merged}} (merged){{end}}{{end}}{{end}}{{with $e.TodoReady}}{{with .PartOf}}
Parent request: {{.}}{{end}}{{with .Satisfied}}
Satisfied prerequisites:{{range .}}
- {{.Title}} ({{.ID}}, {{if eq .Reason "pr_merged"}}PR merged{{else}}done{{end}}{{with .MergedPRs}}: {{join . ", "}}{{end}}){{end}}{{end}}{{with .Notes}}
To-do notes / launch instructions:
{{chomp (indent "  " .)}}{{end}}{{end}}{{with $e.Asked}}
Original request ({{$e.AskedAt}}, the user's own words): {{.}}{{end}}{{with $e.Started}}
Why this exists (the user did not ask for it directly): started by {{if eq . "routine"}}a routine{{else if eq . "hub"}}Persona itself{{else}}the assistant{{end}}{{with $e.StartedWhy}}; reason: {{.}}{{end}}{{end}}{{with $e.Dropped}}
Criteria removed in a scope change:{{range .}}
- {{.}}{{end}}{{end}}{{if $.Fresh}}{{with $e.LastReview}}
Last verdict ({{.At}}): {{.Verdict}}{{with .Note}}, {{.}}{{end}}{{end}}{{end}}{{with $e.Inbox}}
Inbox event {{.Item}} “{{.Title}}”{{with .Sender}}, newest message sent by {{.}}{{end}}{{with .Preview}}: “{{.}}”{{end}}{{with .URL}} ({{.}}){{end}}{{if .AfterAcceptance}}. The to-do was accepted but the PR is not closed yet. Read this feedback and keep the original acceptance record{{end}}{{if eq .ToAgent "sent"}}; the hub already sent the gist and the link straight to its Agent (it received them); this is a copy{{end}}{{end}}{{with $e.Recommend}}
{{if eq $e.Kind "decide"}}Its supervisor Agent recommends{{else}}Recommended{{end}}: {{.}}{{end}}{{with $e.Decision}}
Decision sent to the user at the time (why_owner={{.WhyOwner}}{{if .NeverDefault}}, never taken by default{{end}}): {{.Question}}{{with .Background}}
- Task background (as the hub recorded it): {{.}}{{end}}{{range .Options}}
- Option “{{.Label}}”: {{.LeadsTo}}{{end}}{{with .Recommend}}
- Recommendation: {{.}}{{end}}{{with .Cost}}
- Cost of waiting: {{.}}{{end}}{{end}}{{range $e.Asks}}
- Card interaction_id={{.ID}} (machine={{.Machine}}, {{if eq .Kind "permission"}}permission{{else if eq .Kind "plan"}}plan{{else if eq .Kind "screen"}}terminal choice, only the user can act; use ask_escalate when the user needs to choose{{else}}question{{end}}, session_id={{.Session}}{{if .Reminded}}, not handled in time; the user turned on push grading, so it was not sent to the user and is handed to you once more{{else if .Raised}}, the next level did not handle it in time and it was raised to you{{end}}):
{{chomp (indent "  " .Text)}}{{end}}{{with $e.Decisions}}
Already settled for this task:
{{chomp (indent "  " .)}}{{end}}{{with $e.Ask}}
It is asking:
{{chomp (indent "  " .)}}{{end}}{{with $e.Said}}
It said:
{{chomp (indent "  " .)}}{{end}}{{with $e.Evidence}}
Evidence it gave:{{range .}}
- {{.}}{{end}}{{end}}{{with $e.Files}}
Files it handed back (already on your computer; read them directly, do not ask it to paste the content):{{range .}}
- {{.Name}}{{if .Error}} (not retrieved: {{.Error}}; on the original computer at {{.Path}}){{else}}: {{.Path}}{{end}}{{end}}{{end}}{{with $e.Closeout}}
Closeout record it filled in itself (its own account; nobody has checked it): {{if eq .Status "done"}}done{{else if eq .Status "blocked"}}blocked{{with .Waiting}}, waiting on {{.}}{{end}}{{else}}not done{{end}}; merged: {{if eq .Merged "yes"}}yes{{else if eq .Merged "no"}}no{{else}}not stated or not applicable{{end}}; deployed: {{if eq .Deployed "yes"}}yes{{else if eq .Deployed "no"}}no{{else}}not stated or not applicable{{end}}{{if or .CheckedMerged .CheckedDeployed}}
- The hub checked its PR itself: {{if eq .CheckedMerged "yes"}}merged{{else if eq .CheckedMerged "no"}}not merged{{else}}cannot tell whether it is merged{{end}}, {{if .LocalDeployment}}the local deployment includes the PR's changes{{else if eq .CheckedDeployed "yes"}}the version running in production includes it{{else if eq .CheckedDeployed "no"}}the version running in production does not include it{{else}}cannot tell whether it is live{{end}}{{end}}{{range .Did}}
- Did: {{.}}{{end}}{{range .Artifacts}}
- Artifact: {{.}}{{end}}{{with .Verified}}
- Verified: {{.}}{{end}}{{with .Missing}}
- Missing: {{.}}{{end}}{{end}}{{if $e.KeptUntil}}{{with $e.Screen}}
Last lines of the launch tab:
{{chomp (indent "  | " .)}}{{end}}
{{$e.RetriesLeft}} retries left.{{end}}{{with $e.Review}}
Independent review (the reviewer saw only the task requirements and the change, not the Agent's reasoning): {{if eq .State "failed"}}the review did not complete ({{.Error}}){{else if eq .State "clean"}}no blocking issues{{with .Count}}, {{.}} non-blocking findings{{end}}{{else}}the rework rounds are used up, with {{.Blocking}} blocking issues left{{end}}{{with .Findings}}
{{chomp (indent "  " .)}}{{end}}{{end}}
