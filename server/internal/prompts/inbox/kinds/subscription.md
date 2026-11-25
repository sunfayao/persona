{{if eq .Kind "sub_mail"}}Email fetched periodically by subscription “{{.Label}}”
{{- else if eq .Kind "sub_message"}}Chat messages fetched periodically by subscription “{{.Label}}”
{{- else if eq .Kind "sub_notice"}}Announcements / notices fetched periodically by subscription “{{.Label}}”, usually FYI; needs the user for registration, confirmation, payment, etc.
{{- else if eq .Kind "sub_deadline"}}Deadline items fetched periodically by subscription “{{.Label}}” (assignments, bills, applications, etc.); unfinished items near their deadline need the user
{{- else if eq .Kind "sub_event"}}Calendar / event items fetched periodically by subscription “{{.Label}}”; needs the user to answer an invitation or when about to start
{{- else if eq .Kind "sub_update"}}State changes fetched by subscription “{{.Label}}” (latest balance, holdings, order status, etc.); apply thresholds in user rules if any, otherwise FYI
{{- else if eq .Kind "sub_digest"}}Previous day's items fetched when subscription “{{.Label}}” was enabled, one digest with one line per item; identify which need the user in the summary
{{- else if eq .Kind "sub_failing"}}Persona reminder: subscription “{{.Label}}” has failed to fetch for a full day
{{- else}}Items fetched periodically by subscription “{{.Label}}”{{end}}
