The Agent {{if .Failed}}failed{{else}}stopped{{end}} (session {{.Session}} on {{.Machine}}). {{if .Failed}}Failure reason{{else}}Its final words{{end}}:

<persona-context kind="agent_result" title="{{if .Failed}}Agent failure reason{{else}}Agent's final words{{end}}">
{{if .Text}}{{ctx .Text}}{{else}}(No final words){{end}}
</persona-context>

Read this and continue the matter: call inbox_suggest with the next suggestion and an updated recap.
