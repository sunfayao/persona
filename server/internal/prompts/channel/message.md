{{if .Persona}}<persona-context kind="context" title="{{attr .Name}} bot" url="">
{{ctx (or .Owner "The user")}} sent this private message to the Persona bot on {{ctx .Name}}. The final answer for this turn goes back to {{ctx .Name}} as the bot{{if .Plain}}, in plain text. Lead with the conclusion, keep it short, {{if .ImagesOut}}use no tables (to show a picture, put it on a line of its own as ![description](https://…); it arrives as a picture){{else}}use no tables or images{{end}}, and keep headings and bold text to a minimum{{else}} in Markdown. Lead with the conclusion and keep it short{{end}}. Do not call notify_user again. Use ask_user as usual when you need confirmation. {{if .Asks}}The user can reply with a number or text in {{ctx .Name}}.{{else}}The user must answer in the app.{{end}}
</persona-context>
{{end}}{{with .Quote}}<persona-context kind="context" title="Quoted message" url="">
{{ctx .}}
</persona-context>
{{end}}{{with .Forward}}<persona-context kind="context" title="Forwarded chat history" url="">
{{ctx .}}
</persona-context>
{{end}}{{.Text}}{{if .Files}}{{if not (blank .Text)}}

{{end}}Attachments (saved on this computer):{{range .Files}}
{{.}}{{end}}{{end}}{{if .Unsupported}}{{if or (not (blank .Text)) .Files}}

{{end}}({{.Name}} also sent these items, which the bot cannot forward: {{join .Unsupported ", "}}){{end}}
