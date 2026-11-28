package slashcommands

import (
	"context"
	"net/http"
	"time"

	"github.com/sunfayao/persona/server/internal/apierr"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

type Read func(context.Context, proto.SlashCommandQuery, *proto.SlashCommandCatalog) error

func Serve(w http.ResponseWriter, r *http.Request, available bool, read Read) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get(assistantwire.RoleHeader) != "full" {
		apierr.WriteCode(w, http.StatusForbidden, "slash_commands_forbidden", i18n.Err("devices-slash_commands.unavailable"))
		return
	}
	if !available {
		apierr.WriteJSON(w, proto.SlashCommandCatalog{Commands: []proto.SlashCommand{}})
		return
	}
	query := proto.SlashCommandQuery{SessionID: r.PathValue("sid")}
	if query.SessionID == "" {
		query.Launcher = r.URL.Query().Get("launcher")
		query.Cwd = r.URL.Query().Get("cwd")
	}
	if len(query.SessionID) > 256 || len(query.Launcher) > 256 || len(query.Cwd) > 4096 ||
		(query.SessionID == "" && (query.Launcher == "" || query.Cwd == "")) {
		apierr.WriteCode(w, http.StatusBadRequest, "slash_commands_invalid_target", i18n.Err("devices-slash_commands.unavailable"))
		return
	}
	var out proto.SlashCommandCatalog
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	if err := read(ctx, query, &out); err != nil {
		apierr.WriteCode(w, http.StatusServiceUnavailable, "slash_commands_unavailable", i18n.Err("devices-slash_commands.unavailable"))
		return
	}
	apierr.WriteJSON(w, out)
}
