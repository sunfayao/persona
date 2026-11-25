{{- /* Stand-ins for what an answer holds that is not to be read out (voice/spoken.go): the voice model hears that it is
there and that the screen has it. */ -}}{{- if eq .Kind "code"}}(a code block of {{.N}} {{if eq .N 1}}line{{else}}lines{{end}}, on the screen)
{{- else if eq .Kind "table"}}(a table of {{.N}} {{if eq .N 1}}row{{else}}rows{{end}}, on the screen)
{{- else if eq .Kind "link"}}(a link)
{{- else if eq .Kind "image"}}(an image)
{{- else if eq .Kind "path"}}the file {{.Name}}
{{- else if eq .Kind "line"}}line {{.N}} of {{.Name}}
{{- else if eq .Kind "hash"}}(a hash)
{{- else if eq .Kind "snippet"}}(a piece of code)
{{- else}}(… the middle is left out; the whole answer is on the screen …)
{{- end -}}
