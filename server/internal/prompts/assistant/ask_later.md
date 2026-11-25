🔔 At {{.When}} you asked the user with ask_user (wait: false, without waiting for the answer):
{{range .Questions}}- {{.}}
{{end}}{{if .Answers}}
The user answered on the card:
{{range .Answers}}- {{.Question}} → {{.Choice}}
{{end}}
Carry on as the user answered, and tell the user briefly what you did and what is still theirs to do.{{if .Missing}} You asked several questions then; the user did not answer the ones not listed above, so ask again (ask_user) if you need them.{{end}}
{{else if .Expired}}
Nobody answered the card in time: {{.Expired}}
{{else if .Spoke}}
The card ran out with no answer on it. The user wrote in this conversation meanwhile, so it did not become a to-do and the recommended options were not taken by default. Where those messages already answer the question, do as the user said (no need to mention it again if you already did); where they do not, act on none of the options, and ask again (ask_user) if you still need the decision.
{{else}}
This card ended without an answer (the user closed it, or it is no longer valid). Where the user already answered in the conversation, follow that answer; otherwise act on none of the options and do not ask the same question again; if things are still stuck here, tell the user briefly where.
{{end}}
(This is a notice from Persona, not a message the user sent here.)
