{{if .Blocked}}This instruction was refused because this Agent has a newer {{else}}Latest {{end}}{{if .Owner}}owner input{{else}}resolved answer{{end}}.
<persona-context>
{{ctx .Text}}
</persona-context>{{if .Blocked}}
Use the latest decision. Read the Agent again before issuing a new instruction.{{end}}
