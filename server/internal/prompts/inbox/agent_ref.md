{{if .Lark}}Lark conversation id: {{.Lark.FeedID}}{{if .Lark.Account}}, in Lark account {{.Lark.Account}}{{end}}
{{else if .Doc}}Document URL: {{.Doc.URL}}
{{if .Doc.Rows}}If a row's status should change (for example finished or now followed up), include [{ref, field, value}] in inbox_suggest (kind reply) changes, using one of the refs, fields and allowed values listed above. It changes when the user sends. Omit if no change is needed.{{end}}The reply will be a document comment.
{{else if .Moment}}Moments post id: {{.Moment.PostID}}. The reply will be a comment under this post (replying to the comment marked as the one to reply to above), visible company-wide: include only what can be public.
{{else if .Connector}}{{with .Connector}}{{if .Via}}This came from subscription “{{.Name}}”, reading connector {{.Via}}{{with .ViaSource}} source “{{.}}”{{end}}. Use that connector's read tools for earlier context.
{{end}}{{if or (eq .Source "github") (eq .Via "github")}}GitHub URL: {{.URL}} (github_read for full text and comments, github_diff for changes, github_checks for CI)
{{else if .URL}}URL: {{.URL}}
{{end}}{{with .ReplyTo}}Reply destination (source description): {{.}}.
{{end}}{{end}}{{else if .Mail}}Email account {{.Mail.Account}}, mailbox {{.Mail.Mailbox}}, newest uid {{.Mail.UID}} (for mail_* tools)
{{end}}
