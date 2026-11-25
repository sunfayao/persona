{{- /* The voice model owes the user an answer and has said nothing (docs/assistant/voice.md "Context for the fast
layer" rule 9, voice/nudge.go): the user finished a line or typed one (user), or something was handed to it (result: a
tool's or a turn's result, a refusal, a status), and no caption, audio or function call followed for a while. Sent once,
as a say, which the measured model always speaks after. Nothing of what was said or handed over: it has that already. */ -}}{{- if eq .Cause "result" -}}
You have not told the user what was just handed to you yet, and they are waiting for it. Tell them now in a sentence or two; do not call any tools, and do not mention this reminder.
{{- else -}}
The user is waiting for your answer to what they just said, and you have not said anything. Answer now in a sentence or two; if you did not catch it, ask them to say it again. Do not mention this reminder.
{{- end -}}
