package devicewire

const (
	SlashCommandsMethod       = "agent.slash_commands"
	SessionSlashCommandsRoute = "GET /api/v1/sessions/{mid}/{sid}/slash-commands"
	LaunchSlashCommandsRoute  = "GET /api/v1/machines/{mid}/slash-commands"
)

func init() { Routes = append(Routes, SessionSlashCommandsRoute, LaunchSlashCommandsRoute) }
