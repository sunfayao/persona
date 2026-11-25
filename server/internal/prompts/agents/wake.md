<persona-wake>{{.Marker}}</persona-wake>
⏰ (Persona is waking you, attempt {{.Attempt}}.) At the end of your last turn, you said you were waiting for “{{.Wait}}”{{with .Then}} and would then “{{.}}”{{end}}.{{with .Agent}}
The Agent you were waiting for, “{{.}}”, has stopped{{with $.AgentLine}}. Its last report: {{.}}{{end}}.{{end}}
Check whether what you were waiting for is ready. If it is, continue the work. Otherwise, briefly explain what remains and roughly how long it will take (Persona will wake you again). Do not keep waiting in a loop.
{{- if .Approved}}
{{if .ApprovedByTask}}Your task already authorized this step, and Persona allowed it under that authorization{{else}}The user has already approved this step{{end}}: when the wait is over, “{{.Approved}}”. Do not ask again for that step. For anything else that pushes code, deploys or publishes, sends messages or comments to others, or merges or changes someone else's MR or PR, stop and ask the user first; do not act on your own.
{{- else}}
If the next step pushes code, deploys or publishes, sends messages or comments to others, or merges or changes someone else's MR or PR, stop and ask the user first; do not act on your own.
{{- end}}

