{{if .Owner}}This worker's decision is waiting for the owner. Wait for the upper conversation to relay the decision.
{{else}}This worker's supervisor owns its pending question. Let that supervisor handle it, or relay an instruction from the owner.
{{end -}}
