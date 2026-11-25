{{if eq .Kind "dm"}}Type: Lark direct message received through lark-cli; the other party is a person
{{else if eq .Kind "bot"}}Type: Lark direct message received through lark-cli; the other party is a bot or app
{{else if eq .Kind "mention"}}Type: group message received through lark-cli, mentioning you or everyone
{{else if eq .Kind "group"}}Type: ordinary group message received through lark-cli, including replies in topics you joined
{{else if eq .Kind "doc"}}Type: comment on a Lark document watched through lark-cli, mentioning the user or replying to a comment they participated in{{else}}Type: Lark message received through lark-cli{{end}}{{if ne .Kind "doc"}}
If the chat has sections, judge new activity under “Current messages (new activity):” and use “Background history (context only, not new requests):” for context. In the older format, treat only messages after “—— 新消息 ——” as new.
{{end -}}
