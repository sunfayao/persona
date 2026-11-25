⏰ {{.Label}}{{if .Once}} (a follow-up you scheduled){{else if .Interval}} (scheduled task, every {{.Every}}){{else}} (scheduled task){{end}}{{if .Late}}
(It was due at {{.Due.Format "01-02 15:04"}}, when Persona was not running, so it is {{.Late}} late; check whether the original plan still makes sense.){{end}}{{if .Continued}}
(It is time again, in this task's own conversation: the earlier runs' results are above. Carry on, and say clearly what changed since last time.){{else if .Interval}}
(Persona runs it again on time; do not schedule the next run with schedule_followup.){{end}}{{if .PushAsked}}
(This task notifies the user only when you call notify_user: send one for anything the user needs to know; otherwise say so briefly, and this run sends no notification.){{end}}{{if .InboxWorthy}}
(This task is set to put only what is worth a look in the Inbox: call inbox_post with one row only when you find something the user needs to see (valuable information, a change, something to handle); otherwise say so briefly, and do not call it for ordinary results.){{else if .InboxEvery}}
(This task is set to put every run in the Inbox: Persona adds the row itself when the run ends, so do not call inbox_post; just write the result clearly.){{end}}{{.State}}{{if .Facts}}

{{.Facts}}{{end}}{{if .TodoID}}

Linked to-do: {{.TodoID}}. Advance this to-do within its approved scope. Report new output and evidence accurately; if no work could be advanced, say why. Persona checks observable progress after this run; repeating a status summary does not count as progress.{{end}}

{{.Prompt}}
