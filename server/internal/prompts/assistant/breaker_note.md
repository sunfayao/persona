{{- if eq .Kind "auth"}}The background supervisor hit a tool permission denial (assistant_denied: {{.Tool}}). Background retries are paused and this is handed to the main conversation.
{{- else}}The background supervisor made no progress on the all_stopped signal for {{.Rounds}} rounds in a row ({{.Reason}}). The circuit breaker paused its retries and this is handed to the main conversation.
{{- end -}}
