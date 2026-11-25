{{- $to := "the Persona main conversation"}}{{if .Agent}}{{$to = "the agent"}}{{end}}{{with .To}}{{$to = .}}{{end -}}
{{- if eq .Code "unprompted" -}}
"{{.Task}}" was not sent to {{$to}}: Persona did not hear a new request from the user since the last thing you were told about an agent's work (or they had not spoken in the last half minute), so it held it back to be safe. Tell the user in one sentence that you didn't hear a fresh request from them since the last result (or lately), so you held it back to be safe; if they want it done, they can say it again{{if .Resend}} or tap Send Anyway on the screen{{end}}. Do not make it sound as if someone else said it.
{{- else if eq .Code "empty" -}}
It was not clear what to hand to {{$to}}, so nothing was sent. Ask the user in one sentence to say it again.
{{- else if eq .Code "unknown" -}}
Not sure whether "{{.Task}}" reached {{$to}}: the connection broke off while sending. Do not hand it over again; ask the user in one sentence to look at it on the phone.
{{- else if eq .Code "unreachable" -}}
"{{.Task}}" was not sent to {{$to}}: Persona could not be reached. Tell the user in one sentence that it was not sent.
{{- else if eq .Code "on_screen" -}}
"{{.Task}}" was not sent to {{$to}}: it only says yes or no, and {{$to}} is waiting on a card (a plan, a permission). Cards are answered by tapping them on the screen, not by voice. Tell the user in one sentence to tap it on the screen.
{{- else if eq .Code "dialog_open" -}}
"{{.Task}}" was not sent to {{$to}}: {{if .Plan}}its plan is waiting on the screen. The user can tap there to send back what they just said as feedback on the plan{{else}}a dialog in its terminal is waiting to be dealt with first, on the screen{{end}}. Nothing was typed. Tell the user that in one sentence.
{{- else if eq .Code "input_draft" -}}
"{{.Task}}" was not sent to {{$to}}: its input box on the computer holds words typed there and not sent. Nothing was typed. Tell the user in one sentence; the screen offers to clear them and send.
{{- else if .Reason -}}
"{{.Task}}" was not sent to {{$to}}: {{.Reason}}
Tell the user in one sentence that it was not sent, and why.
{{- else -}}
"{{.Task}}" was not sent to {{$to}}. Tell the user in one sentence that it was not sent.
{{- end -}}
