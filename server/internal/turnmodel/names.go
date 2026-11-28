package turnmodel

// NameOf projects previously recorded tool aliases onto canonical names.
func NameOf(name string) string {
	switch name {
	case "exec_command", "shell_command", "local_shell", "shell", "PowerShell":
		return "Bash"
	case "apply_patch":
		return "ApplyPatch"
	case "subagent", "spawn_agent", "collab_agent":
		return "Agent"
	case "wait_agent", "sleep":
		return "Wait"
	case "KillShell", "KillBash":
		return "TaskStop"
	case "BashOutput":
		return "TaskOutput"
	case "request_user_input", "request_user_input_async":
		return "AskUserQuestion"
	case "update_plan":
		return "TodoWrite"
	}
	return name
}
