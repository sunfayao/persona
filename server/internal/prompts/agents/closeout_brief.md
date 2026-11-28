{{if .Session}}(Persona's convention for this session, not the user's words. No response needed; follow it.)
{{end}}{{if .Readonly}}End your final reply with this record; it tells the hub where the task stands:

{{.Form}}

{{if hasPrefix .Form "### Persona "}}Copy the heading and field names exactly, one "- " line each, ending with --- alone; no code fence. {{end}}Status: done, not_done or blocked. Did: what you found, in up to three sentences. Artifact: a file you made for the user as `file <absolute path>`; leave the line out otherwise. Verified: what you ran or read to check it. Write the content in {{lang .Language}}.{{else}}Closeout record: end the final reply of a turn that changed files or ran commands with this block{{if .AfterDelivery}}, with its delivery, blocker and who must act in it instead of a separate delivery line{{end}}. A turn that only answered needs none; one ending with an optional question or offer writes Status done with no Missing.

{{.Form}}

{{template "agents/closeout_rules" .}}{{end}}
