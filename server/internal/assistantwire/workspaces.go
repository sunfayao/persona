package assistantwire

// workspaceRoutes are the workspace discovery's route: the manager finds the computers' workspaces; the hub relays it
// like Routes.
var workspaceRoutes = []string{
	"GET /api/v1/workspaces",
}

func init() { Routes = append(Routes, workspaceRoutes...) }
