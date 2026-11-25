{{if .Files}}Files from the user{{if .Copied}} (copied from the assistant's computer to this one){{end}}:
{{range .Files}}- {{.}}
{{end}}{{end}}{{if .Missing}}These attachments could not be brought over; if you need them, ask the user to send them to you directly:
{{range .Missing}}- {{.}}
{{end}}{{end}}{{if .Pictures}}<attached_images>
{{range .Pictures}}{{.}}
path: {{.}}
{{end}}</attached_images>{{end}}
