package extmgr

import (
	"net/http"

	"github.com/nwverse/persona/server/internal/extension"
)

// What the extensions that are on declare for the apps (#1854):
// quick prompts for the message box, and templates of an inbox source's triage rules. Skills and
// instructions go to the assistant with the assistant contributions' (extension_assistant.go).

// QuickPromptView is a quick prompt as the apps list it.
type QuickPromptView struct {
	Extension     string `json:"extension"`
	ExtensionName string `json:"extension_name"`
	ID            string `json:"id"`
	Title         string `json:"title"`
	Text          string `json:"text"`
}

// declared calls f with each extension that is on and runs here (its contributions as they take effect).
func (ps *extensions) declared(f func(p *extension.Extension)) {
	if ps == nil {
		return
	}
	for _, p := range ps.reg.List() {
		if p.Record.Enabled && p.Problem == "" {
			f(p)
		}
	}
}

func (h *Manager) declaredRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/extensions/-/prompts", func(w http.ResponseWriter, r *http.Request) {
		out := []QuickPromptView{}
		h.extensions.declared(func(p *extension.Extension) {
			for _, it := range p.Manifest.Prompts() {
				if it.Kind == extension.PromptQuick {
					out = append(out, QuickPromptView{Extension: p.Record.ID, ExtensionName: p.Full.Name, ID: it.ID, Title: it.Title, Text: it.Text})
				}
			}
		})
		writeJSON(w, map[string]any{"prompts": out})
	})
}
