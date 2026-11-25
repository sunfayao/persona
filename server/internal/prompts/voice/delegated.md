{{.Task}}

<persona-context kind="context" title="Voice conversation" url="">
I asked for this in a voice conversation and the voice assistant passed it on, so it may be misheard: ask me when something is unclear. Keep the reply short when you are done: it is read out to me.{{if .History}}

Said in the voice conversation just before:{{range .History}}
{{if eq .Role "user"}}Me{{else}}Voice assistant{{end}}: {{ctx .Text}}{{end}}{{end}}
</persona-context>
