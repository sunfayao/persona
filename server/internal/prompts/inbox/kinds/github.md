{{if eq .Kind "review_requested"}}Type: GitHub PR review requested from the user (listed as a reviewer), usually needs them
{{- else if eq .Kind "mention"}}Type: the user is mentioned in a GitHub PR / issue
{{- else if eq .Kind "team_mention"}}Type: the user's team, not the user personally, is mentioned on GitHub; usually FYI
{{- else if eq .Kind "author"}}Type: a new comment or review on the user's GitHub PR / issue; changes or questions need them, LGTM, approvals and bot messages are FYI
{{- else if eq .Kind "ci_failed"}}Type: GitHub Actions (CI) triggered by the user failed
{{- else if eq .Kind "assign"}}Type: a GitHub issue / PR is assigned to the user
{{- else}}Type: GitHub notification{{end}}
