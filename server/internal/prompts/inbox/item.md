{{if .Described}}Type: {{.Described}}
{{else if eq .Source "github"}}{{template "inbox/kinds/github" .}}
{{else if eq .Source "imessage"}}{{template "inbox/kinds/imessage" .}}
{{else if eq .Source "larkcli"}}{{template "inbox/kinds/larkcli" .}}
{{else if eq .Kind "dm"}}Type: Lark direct message
{{else if eq .Kind "mention"}}Type: Lark group message mentioning the user
{{else if eq .Kind "group"}}Type: new Lark group message (not mentioning the user)
{{else if eq .Kind "bot"}}Type: message from a Lark bot / app
{{else if eq .Kind "doc"}}Type: a Lark document mentions the user or has a comment for them (a spreadsheet or multidimensional table may need a row followed up or its status changed)
{{else if eq .Kind "moment"}}Type: someone commented on the user's post, replied to their comment or mentioned them in Lark Moments (company-wide public posts, like a social feed); most are chatter or appreciation, needing the user only for a question or a requested response
{{else if eq .Kind "mail"}}{{if .MailDirect}}Type: email sent directly to the user
{{else}}Type: email copied to the user, bulk mail or a mailing list
{{end}}{{end}}{{if .Latest}}
Newest message: {{ctx .Latest}}

Conversation context (including earlier messages):
{{end}}{{ctx .Context}}
