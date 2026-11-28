package hub

// startsAsync: a start the app asked to have answered at once is, when the node can acknowledge it before the session
// is ready (only Claude can, and not a resume).
func startsAsync(asked bool, resume, launcher, agentType string) bool {
	return asked && resume == "" && (agentType == "claude" || (agentType == "" && launcher != "agy"))
}

func desktopStartLauncher(launcher string) bool {
	return launcher == "claude-desktop" || launcher == "codex-app"
}
