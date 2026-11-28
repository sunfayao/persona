package node

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"time"

	"github.com/sunfayao/persona/server/internal/node/usagewindows"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
)

// After a model reply, Claude Code's stream-json output carries a rate_limit_event with the windows of the account the
// process runs on (#12855): rate_limit_info.unifiedWindows holds five_hour and seven_day,
// each with utilization (a fraction) and resetsAt (epoch seconds), measured on Claude Code 2.1.330. The node already
// reads that stream for its Claude assistant conversations (readAssistant), so every turn refreshes the windows with no
// process, credential or request of its own. An event without unifiedWindows (an API key or a gateway) adds nothing.

// claudeStreamUsageKey is the usage source of the Claude conversations one launcher runs with one config directory
// choice (modelExecution.ConfigDir): their newer report replaces their own older one and never another pair's, which
// may be signed in to another account.
func claudeStreamUsageKey(launcher, configDir string) string {
	if configDir != "" && configDir != "default" {
		configDir = realPath(configDir)
	}
	if launcher == "" {
		launcher = "claude"
	}
	return "claude-stream:" + codexUsageKey(launcher + "\x00" + configDir)[:16]
}

// parseClaudeRateLimitEvent reads one stream-json line's rate_limit_event as the account's windows sampled at now.
func parseClaudeRateLimitEvent(line []byte, now time.Time) (proto.UsageAccount, bool) {
	var ev struct {
		Type string `json:"type"`
		Info struct {
			Windows map[string]struct {
				Utilization *float64        `json:"utilization"`
				ResetsAt    json.RawMessage `json:"resetsAt"`
			} `json:"unifiedWindows"`
		} `json:"rate_limit_info"`
	}
	if json.Unmarshal(line, &ev) != nil || ev.Type != "rate_limit_event" {
		return proto.UsageAccount{}, false
	}
	var wins []proto.UsageWindow
	for _, def := range claudeStatusWindows {
		w, ok := ev.Info.Windows[def.key]
		if !ok || w.Utilization == nil || math.IsNaN(*w.Utilization) || math.IsInf(*w.Utilization, 0) {
			continue
		}
		// Rounded to 1e-9 so 0.29 reads as 29, as a gateway's used_ratio is (usagesource).
		used := clampPercent(math.Round(*w.Utilization*100*1e9) / 1e9)
		win := proto.UsageWindow{Label: proto.WindowLabel(def.minutes), WindowMinutes: def.minutes, UsedPercent: used}
		if t, ok := transcript.ParseResetsAt(w.ResetsAt); ok {
			win.ResetsAt = &t
		}
		wins = append(wins, win)
	}
	wins = liveWindows(wins, now)
	if len(wins) == 0 {
		return proto.UsageAccount{}, false
	}
	return proto.UsageAccount{Provider: "claude", Name: "Claude", Windows: wins, SampledAt: now.UTC()}, true
}

// claudeStreamUsage keeps a rate_limit_event line as its launcher's usage source, the way a local runtime's
// usage.windows report is kept (runtimeUsageWindows): nothing while collection is off, the newest sample wins, and the
// next polls keep it like any quiet source.
func (n *Node) claudeStreamUsage(launcher, configDir string, line []byte, now time.Time) {
	a, ok := parseClaudeRateLimitEvent(line, now)
	if !ok {
		return
	}
	if configDir == "" || configDir == "default" {
		configDir = n.defaultClaudeDir()
	}
	n.recordUsageSample(claudeStreamUsageKey(launcher, configDir), a, now)
}

const claudeUsagePollTimeout = 45 * time.Second // includes cold launches via the user's shell wrapper

func (n *Node) pollClaudeUsage(ctx context.Context) (string, proto.UsageNodeAccount) {
	dir := n.defaultClaudeDir()
	if start := n.claudeStartDir.Load(); start != nil && *start != "" {
		dir = proto.ExpandHome(*start, n.home)
	}
	key := claudeStreamUsageKey("claude", dir)
	var account proto.UsageAccount
	usagewindows.Probe(ctx, claudeUsagePollTimeout, usageProbeBackoff, func(read context.Context) bool {
		var retry bool
		account, retry = n.readClaudeCLIUsage(read)
		return retry
	})
	return key, proto.UsageNodeAccount{UsageAccount: account}
}

func (n *Node) readClaudeCLIUsage(ctx context.Context) (proto.UsageAccount, bool) {
	// The interactive shell resolves the same Claude wrapper as a terminal Agent.
	// The /usage slash command is local and starts no model turn or session.
	const args = " -p /usage --output-format json --verbose --no-session-persistence --tools '' --strict-mcp-config --setting-sources user"
	// A file on PATH can be the bare CLI even on a machine enforcing PMA. An
	// installed PMA launcher launches the same account as Persona's Agents.
	command := n.launcherForScript("claude") + args
	if pma := agentExecutableOnDisk("proxymyagent"); pma != "" {
		command = "if typeset -f claude >/dev/null; then " + command + "; else " + scriptPath(pma) + " run" + args + "; fi"
	}
	cmd := loginShell(ctx, "cd "+scriptPath(os.TempDir())+" && "+command)
	cmd.Env = append(os.Environ(), "PERSONA_AGENT=1", "PMA_ACCOUNT_NO_PROMPT=1")
	killGroupOnCancel(cmd)
	out, _ := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return proto.UsageAccount{}, true
	}
	return usagewindows.ParseClaudeReport(out, time.Now())
}
