package assistant

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sunfayao/persona/server/internal/apierr"
	"github.com/sunfayao/persona/server/internal/i18n"
)

// The Agents routes include display options and the triage switches.

func (s *Service) agentRoutes() {
	s.agentsDisplayRoutes()
	s.handle("GET /api/v1/agents/triage", func(w http.ResponseWriter, r *http.Request) {
		sw, err := s.readTriageSwitches(r.Context())
		if err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, i18n.Err("assistant-agents_work.read_switches_error", i18n.Args{"error": err.Error()}))
			return
		}
		apierr.WriteJSON(w, sw)
	})
	s.handle("PUT /api/v1/agents/triage", func(w http.ResponseWriter, r *http.Request) {
		if !fullRole(r) {
			apierr.Write(w, http.StatusForbidden, errors.New("read-only role cannot change the triage"))
			return
		}
		var request struct {
			TriageSwitches
			ToolLoop *bool `json:"tool_loop"`
			Closeout *bool `json:"closeout"`
			// CloseoutAsk shadows the embedded switch, as the two above do: left out is not "off".
			CloseoutAsk *bool `json:"closeout_ask"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&request); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		// An app that does not know the reminder's switch (#5096) leaves it as it was. While the saved switches cannot be
		// read, a PUT still goes through (it saves the whole set): then the reminder's switch it leaves out is the default.
		prev, _ := s.readTriageSwitches(r.Context())
		in := request.TriageSwitches
		in.ToolLoop = prev.ToolLoop
		if request.ToolLoop != nil {
			in.ToolLoop = *request.ToolLoop
		}
		in.Closeout = prev.Closeout // an app that does not know it leaves it as it was
		if request.Closeout != nil {
			in.Closeout = *request.Closeout
		}
		in.CloseoutAsk = prev.CloseoutAsk
		if request.CloseoutAsk != nil {
			in.CloseoutAsk = *request.CloseoutAsk
		}
		if in.ReplyReminder == nil {
			in.ReplyReminder = prev.ReplyReminder
		}
		if in.TransientRetry == nil {
			in.TransientRetry = prev.TransientRetry // an app that does not know it (#3208) leaves it as it was
		}
		switch {
		case in.ReplyReminderMinutes == 0:
			in.ReplyReminderMinutes = prev.ReplyReminderMinutes
		case in.ReplyReminderMinutes < 0:
			in.ReplyReminderMinutes = 0 // back to the default
		}
		if err := s.putTriageSwitches(r.Context(), in); err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
		apierr.WriteJSON(w, in)
	})
}
