{{- /* What a call bound to an agent sends it (docs/assistant/voice.md「Sending, gates, and 409」): the task on its first line (a run finds
the message by it when the transcript has no client_msg_id), then a context block. Not "keep the reply short": the
agent's work is what it is, the voice says what it needs of it. */ -}}{{.Task}}

<persona-context kind="context" title="Voice conversation" url="">
I said this in a voice conversation and the voice assistant passed it on, so it may be misheard: when something is ambiguous, ask me before acting on it.{{if .History}}

Said in the voice conversation just before:{{range .History}}
{{if eq .Role "user"}}Me{{else}}Voice assistant{{end}}: {{ctx .Text}}{{end}}{{end}}
</persona-context>
