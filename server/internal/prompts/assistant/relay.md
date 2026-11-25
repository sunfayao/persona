<persona-context kind="context" title="From the conversation “{{attr .FromTitle}}”" url="">
The user did not send this message: another Persona conversation ({{.FromID}}) sent it with {{.Tool}}, hop {{.Hop}} of at most {{.MaxHops}}.
Do what it says. To tell that conversation the result, send it back to {{.FromID}} with conversation_send; do not reply just to acknowledge, so the two conversations do not message back and forth.
</persona-context>
{{with .Quote}}<persona-context kind="owner-request" message_id="{{attr .ID}}">
{{if .Part}}Below is an excerpt of the user's own words{{else}}Below are the user's own words{{end}}, checked by the hub against the message and unchanged ({{.At}}, said in the conversation {{.Conversation}}{{with .Via}}, relayed through {{join . " → "}}{{end}}).
When you hand out work from this (task_plan, agent_start and so on), the task brief records these words and their source by itself, so do not copy them; to pass them to another conversation, use conversation_send with quote="{{attr .ID}}".
{{ctx .Text}}
</persona-context>
{{end}}{{.Text}}
