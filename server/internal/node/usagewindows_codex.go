package node

import (
	"context"
	"encoding/json"
	"github.com/sunfayao/persona/server/internal/node/harness/codex/appserver"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/sunfayao/persona/server/internal/proto"
)

const (
	codexUsagePollParallel = 4
	codexUsagePollTimeout  = 5 * time.Second // each home, including app-server startup
	codexUsageMaxBackoff   = 30 * time.Minute
)

type codexUsagePollState struct {
	delay       time.Duration
	retryAt     time.Time
	unavailable bool
}

type codexUsageProbe struct {
	account     proto.UsageNodeAccount
	unavailable bool // the CLI confirmed there is no subscription on this home
}

// Usage covers standalone Codex homes as well as Desktop homes. Prefer the machine's CLI, falling back to the
// Desktop bundle when it is the only install. Desktop's own feature calls keep using its bundled CLI first.
var codexUsageCLI = func() (string, error) {
	if cli, err := agentExecutable("codex"); err == nil {
		return cli, nil
	}
	return codexAppCLI()
}

// Each home has its own deadline and backoff. A failure in one installation never delays a later poll of healthy
// installations, and a signed-out home cannot revive an old rollout during a later failed read.
func collectCodexAppUsage(ctx context.Context, homes []string, found map[string]proto.UsageNodeAccount,
	previous map[string]codexUsagePollState, now time.Time,
) map[string]codexUsagePollState {
	type poll struct {
		key   string
		state codexUsagePollState
		probe codexUsageProbe
	}
	cli, cliErr := codexUsageCLI()
	var reads errgroup.Group
	reads.SetLimit(codexUsagePollParallel)
	polls := make([]poll, 0, len(homes))
	for i, home := range homes {
		if ctx.Err() != nil {
			break
		}
		key := "codex:" + codexUsageKey(realPath(home))
		state := previous[key]
		polls = append(polls, poll{key: key, state: state})
		entry := &polls[i]
		if info, err := os.Stat(home); err != nil || !info.IsDir() {
			// Starting here would create an unused home. An absent directory is conclusive; a temporary
			// filesystem error leaves the previous reading alone.
			entry.probe.unavailable = os.IsNotExist(err) || err == nil
			continue
		}
		if cliErr != nil || now.Before(state.retryAt) {
			continue
		}
		reads.Go(func() error {
			readCtx, cancel := context.WithTimeout(ctx, codexUsagePollTimeout)
			defer cancel()
			entry.probe = readCodexAppUsage(readCtx, cli, home)
			if len(entry.probe.account.Windows) == 0 && !entry.probe.unavailable {
				entry.state.delay = min(max(2*entry.state.delay, 2*usageWindowsEvery), codexUsageMaxBackoff)
				entry.state.retryAt = now.Add(entry.state.delay)
			}
			return nil
		})
	}
	_ = reads.Wait()
	states := make(map[string]codexUsagePollState, len(polls))
	for _, entry := range polls {
		switch {
		case entry.probe.unavailable:
			entry.state = codexUsagePollState{unavailable: true}
		case len(entry.probe.account.Windows) > 0:
			entry.state = codexUsagePollState{}
			found[entry.key] = entry.probe.account
		}
		states[entry.key] = entry.state
	}
	return states
}

// readCodexAppUsage asks only for usage metadata. The CLI manages its own login. In particular, the account ID comes
// from the very same response as the windows, so changing the signed-in account never relabels a historical rollout.
func readCodexAppUsage(ctx context.Context, cli, home string) codexUsageProbe {
	// This short-lived probe has no need to start plugin setup in the background.
	app, err := startCodexStdio(ctx, cli, home, os.TempDir(), "-c", "features.plugins=false")
	if err != nil {
		return codexUsageProbe{}
	}
	defer app.Close()
	type window struct {
		UsedPercent        *float64 `json:"usedPercent"`
		WindowDurationMins *int     `json:"windowDurationMins"`
		ResetsAt           *int64   `json:"resetsAt"`
	}
	type limits struct {
		LimitID   string  `json:"limitId"`
		PlanType  string  `json:"planType"`
		Primary   *window `json:"primary"`
		Secondary *window `json:"secondary"`
	}
	var result struct {
		AccountID           string             `json:"accountId"`
		RateLimits          *limits            `json:"rateLimits"`
		RateLimitsByLimitID map[string]*limits `json:"rateLimitsByLimitId"`
	}
	params := map[string]bool{"excludeResetCreditDetails": true}
	if err := app.Call(ctx, "account/rateLimits/read", params, &result); err != nil {
		return codexUsageProbe{unavailable: codexUsageUnavailable(ctx, app)}
	}
	bucket := result.RateLimitsByLimitID["codex"]
	if bucket == nil && result.RateLimits != nil && (result.RateLimits.LimitID == "codex" || result.RateLimits.LimitID == "") {
		bucket = result.RateLimits
	}
	if bucket == nil {
		return codexUsageProbe{unavailable: codexUsageUnavailable(ctx, app)}
	}
	reported := [2]*window{bucket.Primary, bucket.Secondary}
	var windows []proto.UsageWindow
	for _, w := range reported {
		if w == nil || w.WindowDurationMins == nil {
			continue
		}
		minutes := canonicalCodexUsageDuration(*w.WindowDurationMins)
		windows = appendCodexWindow(windows, w.UsedPercent, &minutes, w.ResetsAt)
	}
	// Old CLI responses omitted the duration, with the session limit first and weekly second.
	// An explicitly reported length always wins over that positional fallback.
	for i, w := range reported {
		minutes := [2]int{300, 10080}[i]
		if w == nil || w.WindowDurationMins != nil || slices.ContainsFunc(windows, func(win proto.UsageWindow) bool {
			return win.WindowMinutes == minutes
		}) {
			continue
		}
		windows = appendCodexWindow(windows, w.UsedPercent, &minutes, w.ResetsAt)
	}
	if len(windows) == 0 {
		return codexUsageProbe{unavailable: codexUsageUnavailable(ctx, app)}
	}
	// Keep recognized windows even when they have expired. The manager must see the newest snapshot for this
	// account before dropping expired windows, or a stale observation from another machine could resurrect it.
	sort.SliceStable(windows, func(i, j int) bool { return windows[i].WindowMinutes < windows[j].WindowMinutes })
	plan := strings.ToLower(strings.TrimSpace(bucket.PlanType))
	account := proto.UsageNodeAccount{
		UsageAccount: proto.UsageAccount{
			Provider: "codex", Name: codexAccountName(plan), Plan: plan,
			Windows: windows, SampledAt: time.Now().UTC(),
		},
		SourceKey: codexUsageKey(realPath(home)),
	}
	switch plan {
	case "free", "go", "plus", "pro", "prolite", "promax":
		// Workspace members can share an account ID without sharing their individual limits.
		if id := strings.TrimSpace(result.AccountID); id != "" {
			account.AccountKey = codexUsageKey(id)
		}
	}
	return codexUsageProbe{account: account}
}

func canonicalCodexUsageDuration(minutes int) int {
	for _, standard := range []int{300, 10080} {
		if minutes >= standard-1 && minutes <= standard+1 {
			return standard
		}
	}
	return minutes
}

// Use an explicit account response to distinguish an account with no applicable quota from a transient read failure.
// Do not refresh the login, consume reset credits, or read the email or credentials.
func codexUsageUnavailable(ctx context.Context, app *appserver.Stdio) bool {
	var state struct {
		Account            json.RawMessage `json:"account"`
		RequiresOpenAIAuth *bool           `json:"requiresOpenaiAuth"`
	}
	params := map[string]bool{"refreshToken": false}
	if app.Call(ctx, "account/read", params, &state) != nil || state.RequiresOpenAIAuth == nil {
		return false
	}
	var account *struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(state.Account, &account) != nil {
		return false
	}
	return account == nil || account.Type == "apiKey" || account.Type == "amazonBedrock"
}
