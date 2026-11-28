package usagesource

import (
	"cmp"
	"net"
	"net/url"
	"slices"
	"strings"

	"github.com/sunfayao/persona/server/internal/proto"
)

// The local agy /usage exporter used agy-<hostname>-<group> as an account ID.
// Its group quotas are windows of one account. External generic gateways keep their own accounts.
func (res *Result) foldLegacyAgy(endpoint string) {
	u, err := url.Parse(endpoint)
	if err != nil || !strings.EqualFold(u.Hostname(), "localhost") && !net.ParseIP(u.Hostname()).IsLoopback() {
		return
	}
	byHost := map[string]int{}
	out := make([]proto.UsageAccount, 0, len(res.Accounts))
	for _, a := range res.Accounts {
		host, model := legacyAgyAccount(a)
		if host == "" {
			out = append(out, a)
			continue
		}
		a.Provider, a.Name, a.Ref, a.LegacyAgyHost = "agy", "Antigravity", "agy-"+host, host
		if model != "" {
			for i := range a.Windows {
				a.Windows[i].Model = model
			}
		}
		if i, exists := byHost[host]; exists {
			combined := &out[i]
			combined.Windows = append(combined.Windows, a.Windows...)
			if a.SampledAt.After(combined.SampledAt) {
				combined.SampledAt = a.SampledAt
			}
			combined.Stale = combined.Stale && a.Stale
		} else {
			byHost[host] = len(out)
			out = append(out, a)
		}
	}
	for _, i := range byHost {
		windows := out[i].Windows
		slices.SortStableFunc(windows, func(a, b proto.UsageWindow) int {
			return cmp.Or(cmp.Compare(a.WindowMinutes, b.WindowMinutes), cmp.Compare(a.Model, b.Model))
		})
	}
	res.Accounts = out
}

func legacyAgyAccount(a proto.UsageAccount) (host, model string) {
	if a.Provider != "antigravity" || a.Plan != "" {
		return "", ""
	}
	host, ok := strings.CutPrefix(a.Ref, "agy-")
	if !ok {
		return "", ""
	}
	switch a.Name {
	case "Gemini":
		host, ok = strings.CutSuffix(host, "-gemini")
		model = "Gemini Models"
	case "Claude and GPT":
		host, ok = strings.CutSuffix(host, "-claude-and-gpt")
		model = "Claude and GPT models"
	default:
		return "", ""
	}
	if !ok || host == "" {
		return "", ""
	}
	for _, w := range a.Windows {
		if w.Model != "" || w.WindowMinutes != 300 && w.WindowMinutes != 10080 {
			return "", ""
		}
	}
	return strings.ToLower(host), model
}
