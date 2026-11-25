package hub

import "net/http"

// pushRoutes: GET /api/v1/devices/push, every browser with Web Push (`hub doctor`).
func (h *Hub) pushRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/devices/push", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"web": h.webPushList()})
	})
}
