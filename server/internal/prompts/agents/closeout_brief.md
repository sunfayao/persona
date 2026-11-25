{{if .Session}}(Persona's convention for this session, not the user's words. No response needed; follow it.)
{{end}}Closeout record (the hub uses it to see where this turn stands, so nobody has to ask again): end the final reply of any turn that changed files or ran commands with a block like the one below{{if .AfterDelivery}}, recording the delivery, what blocks it and who must act in its delivery / missing / unblock instead of a separate delivery line{{end}}. A turn that only answered a question without file changes or commands needs no record, except when it ends with an optional conversational question or offer that does not block work if unanswered: include status="done" and no missing so the hub does not mistake it for waiting on the user.

{{.Form}}

{{template "agents/closeout_rules" .}}
