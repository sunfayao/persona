<persona-context kind="context" title="Messages that did not wake you" url="">
Other conversations sent you these with conversation_send wake: false. They started no turn of their own and need no reply; read them along with the message below.
{{range .Items}}
From the conversation "{{ctx .FromTitle}}" ({{.FromID}}), {{.At}}:
{{ctx .Text}}
{{end}}</persona-context>
