package codex

import (
	"context"
	"slices"

	"github.com/nwverse/persona/server/internal/proto"
)

// Thinking effort of a Codex thread Persona drives through its app-server: a session listed
// from an app-server (source_kind codex_app_server) or a herdr pane joined on one (codex_daemon.go). The phone's
// messages are turn/start / turn/steer there, so the effort is the thread's own setting, not keys typed into a TUI:
//
//   - Shown: what the app-server says the thread is configured with (thread/read's reasoningEffort, every refresh, and
//     thread/settings/updated), else what its rollout last ran with, then its model's default from model/list; the
//     choices are what the thread's model takes (model/list's supportedReasoningEfforts), Codex's whole list when the
//     model is not listed.
//   - Set: thread/settings/update, which applies from the next turn whoever starts it (the TUI's too). Only an
//     app-server that answers it with -32601 (method not found: it is experimental in 0.163) gets the choice kept for
//     later (codexPending): it goes with the phone's next turn/start (turn/steer takes none) and stays shown as the
//     session's effort until then. Any other failure (the connection gone, no answer in time, Codex refusing it) is
//     the phone's error, and nothing is kept for a later turn to carry.
//
// Permission modes stay Claude's: a Codex thread's approvals and sandbox are not changed from the phone. Panes on the
// terminal route, subagents, the Codex App's own chats and a compatible CLI keep refusing both (modes.go).

// codexModelEfforts is what model/list says about a model's efforts.
type ModelEfforts struct {
	Levels  []string            // weakest first; empty: it takes none
	Default string              // what it runs with when the thread sets none
	Tiers   []proto.ServiceTier // native model/list speed choices
}

// Both external and Persona-owned app-servers advertise the same model metadata.
func ReadModels(ctx context.Context, call func(context.Context, string, any, any) error) (map[string]ModelEfforts, error) {
	models := map[string]ModelEfforts{}
	cursor := ""
	for page := 0; page < 20; page++ {
		params := map[string]any{"includeHidden": true}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var res struct {
			Data []struct {
				Model   string              `json:"model"`
				ID      string              `json:"id"`
				Default string              `json:"defaultReasoningEffort"`
				Tiers   []proto.ServiceTier `json:"serviceTiers"`
				Efforts []struct {
					Effort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := call(ctx, "model/list", params, &res); err != nil {
			return nil, err
		}
		for _, m := range res.Data {
			e := []string{}
			for _, x := range m.Efforts {
				if x.Effort != "" && !slices.Contains(e, x.Effort) {
					e = append(e, x.Effort)
				}
			}
			for _, k := range []string{m.Model, m.ID} {
				if k != "" {
					models[k] = ModelEfforts{Levels: e, Default: m.Default, Tiers: m.Tiers}
				}
			}
		}
		if res.NextCursor == nil || *res.NextCursor == "" || *res.NextCursor == cursor {
			break
		}
		cursor = *res.NextCursor
	}
	return models, nil
}
