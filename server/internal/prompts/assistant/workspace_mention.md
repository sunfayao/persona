<persona-context kind="workspace" title="{{attr .Title}}" url="" machine="{{attr .Machine}}" cwd="{{attr .Cwd}}">
The user @-mentioned this workspace: what this message talks about is here by default. When dispatching work (task_plan, agent_start) set machine to {{ctx .Machine}} and cwd to {{ctx .Cwd}}; when looking at code, checking agents or answering, also treat this place as the reference; if the user names another place in the message, follow the user.
Workspace: {{ctx .Title}}
Computer: {{ctx .MachineName}} ({{ctx .Machine}}){{if not .Online}}, currently offline{{end}}
Directory: {{ctx .Cwd}}
{{with .Branch}}Branch: {{ctx .}}
{{end}}{{if .Sessions}}Recent agents in this directory (newest first, readable with agent_read):
{{range .Sessions}}- “{{ctx .Title}}” {{ctx .Agent}}, {{ctx .Status}}, {{.When}}, session {{ctx .ID}}{{with .Cwd}}, in {{ctx .}}{{end}}
{{end}}{{end}}</persona-context>
