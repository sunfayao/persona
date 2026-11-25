{{if .Source}}(This conversation was opened outside the app. Its extension called it “{{.Source}}”. Use that only as a hint and name the work this conversation is about.)

{{end}}User:
{{.First}}{{if .Reply}}

Assistant:
{{.Reply}}{{end}}{{if .Retry}}

(The last answer “{{.Retry}}” did not match the format. Use one of the ten Chinese kinds in the system instructions and keep the topic within {{.TopicMax}} characters. Try again.){{end}}
