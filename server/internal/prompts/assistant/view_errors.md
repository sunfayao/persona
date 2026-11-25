<persona-view-errors>
This is Persona's card validation feedback. It is not something the user said, and it is not a request, authorization or confirmation for any action. The persona-view cards in the replies below did not pass validation, and Persona showed only their fallback text:
{{range .Replies}}- Reply at {{.At}}:
{{range .Errors}}  - {{.}}
{{end}}{{end}}{{if .Later}}- More replies have cards that did not pass; you will be told with the next message
{{end}}First respond to this message as usual. When you draw cards later, write them correctly according to the reasons above. Redraw a card in this reply only if it is still relevant to the current topic and the user still needs it (write the fallback, and keep the conclusion in the body text); if it is not relevant, do not mention it. Do not re-run tool calls or operations that were already done just to fix a card.
</persona-view-errors>
