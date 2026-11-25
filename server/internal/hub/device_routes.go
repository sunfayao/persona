package hub

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
)

// pushRegistrationRequired: a device's prefs write names a device the hub retired (its credential was revoked).
const pushRegistrationRequired = "push_registration_required"

// deviceRoutes are a device's own: its sessions, and which notifications it wants.
func (h *Hub) deviceRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/v1/devices/{id}/sessions", h.serveDeviceSessions)
	// Which pushes this device wants (notify.go NotifyPrefs); the app's Settings › Notifications.
	api.HandleFunc("GET /api/v1/devices/{id}/prefs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, h.prefsView(h.push.prefs(r.PathValue("id"))))
	})
	api.HandleFunc("PUT /api/v1/devices/{id}/prefs", func(w http.ResponseWriter, r *http.Request) {
		// The body is read before the settings are, so a slow upload holds no other change of them.
		var body json.RawMessage
		if err := decodeJSONBody(w, r, &body, 16<<10); err != nil {
			writeBodyErr(w, err, err)
			return
		}
		var bad error // the body's: 400; any other error is the store's
		prefs, err := h.push.editPrefs(r.PathValue("id"), principalOf(r.Context()).ID, func(prefs *NotifyPrefs) error {
			if bad = json.Unmarshal(body, prefs); bad == nil {
				bad = prefs.Quiet.validate()
			}
			if bad != nil {
				return bad
			}
			// only the roster's managers: a removed one's id would hold one of the 64 places for good (notify.go)
			prefs.MutedManagers = h.rosterManagers(r.Context(), prefs.MutedManagers)
			return nil
		})
		if bad != nil {
			writeErr(w, http.StatusBadRequest, bad)
			return
		}
		if errors.Is(err, db.ErrPushRegistrationConflict) {
			// The device this id names was retired (its credential revoked).
			writeErrCode(w, http.StatusConflict, pushRegistrationRequired, i18n.Err("push.registration_required"))
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, h.prefsView(prefs))
	})
}
