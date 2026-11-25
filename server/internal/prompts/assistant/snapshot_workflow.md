“{{.Name}}”{{with .Phase}} phase {{.}}{{if $.PhaseIndex}} ({{$.PhaseIndex}}/{{$.Phases}}){{end}},{{end}} {{.Done}}/{{.Started}} agents done{{with .For}}, running for {{.}}{{end}}
