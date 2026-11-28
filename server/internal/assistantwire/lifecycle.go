package assistantwire

const (
	TodoLifecycleRoute    = "GET /api/v1/todos/{id}/lifecycle"
	SessionLifecycleRoute = "GET /api/v1/sessions/{mid}/{sid}/lifecycle"
)

func init() { Routes = append(Routes, TodoLifecycleRoute, SessionLifecycleRoute) }
