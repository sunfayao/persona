package devices

import (
	"context"
)

// Live replies (#1309, docs/integrations/claude-integration.md "Live replies"): the Claude sessions Persona starts on a
// machine load the plugin with Claude Code's MessageDisplay hook, so their replies reach the apps as Claude shows them.
// It costs the terminal on that computer something (replies show a batch of lines at a time, each once the hook ran),
// and the previous per-machine choice still applies. On unless the user turned it off; a node of an older hub, which
// never says, keeps it on too.

// liveRepliesKey is where a machine's choice is kept (the hub's settings table).
func liveRepliesKey(machine string) string { return "claude_live_replies/" + machine }

// liveReplies is whether the machine's Claude starts stream their replies: what the user chose, else on.
func (m *Machine) liveReplies(ctx context.Context) bool {
	if m.d.opts.Store != nil {
		var on bool
		if ok, err := m.d.opts.Store.Setting(ctx, liveRepliesKey(m.cfg.ID), &on); ok && err == nil {
			return on
		}
	}
	return true
}
