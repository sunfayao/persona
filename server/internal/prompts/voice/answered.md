{{- /* A question asked by voice is over (docs/assistant/voice.md「Questions and cards」): its answer went to the conversation (voice: on
the delegation that answered it; .Unfollowed: no run follows that conversation, so its result is not told by itself),
it was answered on the screen or another device while it was asked (elsewhere: said by itself, only for the one being
asked) or before the user's answer got there (late), the answer did not get there (failed: it is asked again when the
user says it again; .Code input_draft / dialog_open, a node's refusal, in words of its own, never the hub's, which
hold the box's draft or a dialog's text; else .Reason, the hub's own words), the connection broke off and the question
no longer waits (unknown), or it was not heard twice and is left to the screen (screen). No agent's words: nothing here
needs the <agent_output> framing. Data: VoiceAnswered. */ -}}{{- $to := or .To "the Persona main conversation" -}}
{{- $To := or .To "The Persona main conversation" -}}
{{- $where := print "on the phone, in " $to -}}{{if .Here}}{{$where = "on the screen"}}{{end -}}
{{- if eq .By "voice" -}}
The user's answer went to {{$to}}: it goes on with its work, it is not finished yet.{{if .Unfollowed}} What it does next is not followed in this call: you will not be told when it is done, so do not offer to tell the user; they can ask how far it is.{{end}} Tell the user briefly, in one sentence, that you passed it on; do not ask the question again.
{{- else if eq .By "elsewhere" -}}
The question {{$to}} asked was answered on the screen or elsewhere: it goes on with its work, it is not finished yet. Do not ask the question again; just confirm it briefly in one sentence.
{{- else if eq .By "late" -}}
What the user just said was not passed to {{$to}}: its question had already been answered on the screen or elsewhere. It goes on with its work, it is not finished yet. Tell the user in one sentence that the question was already answered; do not ask it again.
{{- else if eq .By "unknown" -}}
Not sure whether the user's answer got to {{$to}}: the connection broke off while it was passed on, and the question no longer waits; it is not finished yet. Do not ask it again; tell the user in one sentence to look {{$where}} whether it took their answer.
{{- else if eq .By "failed" -}}
The user's answer did not get to {{$to}}{{if eq .Code "input_draft"}}: its input box on the computer holds words typed there and not sent, so nothing was typed.{{else if eq .Code "dialog_open"}}: another dialog is open on the computer first, so nothing was typed.{{else if .Reason}}: {{.Reason}}{{else}}.{{end}}
It still waits for the answer, it is not finished. {{if eq .Code "input_draft"}}Ask the user in one sentence to send or clear those words on the computer, then say the answer again.{{else if eq .Code "dialog_open"}}Ask the user in one sentence to deal with that dialog on the computer, then say the answer again.{{else}}Ask the user in one sentence to say it again, or to answer it {{$where}}.{{end}}
{{- else -}}
The user's answer was not heard again, so the question is no longer asked by voice: {{$to}} still waits for it, it is not finished. Ask the user in one sentence to answer the card {{$where}}.
{{- end -}}
