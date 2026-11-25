{{- /* Part of voice/report: the earlier tasks of a folded run (sent while it worked) were handed over on calls of
their own, which get no result of their own: nothing goes on those calls (docs/lessons/voice.md row 4: anything on an
open call has the model speak again), this answer covers them. Data: VoiceReport. */ -}}
{{- $n := len .Tasks -}}This result covers what was handed over before too ({{range $i, $t := .Tasks}}{{if lt (inc $i) $n}}{{if $i}} and {{end}}"{{$t}}"{{end}}{{end}}): it went to the same conversation and is taken together here; that earlier call gets no result of its own, so do not wait for one.
