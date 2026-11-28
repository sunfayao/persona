package usagewindows

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

type claudeUsageEvent struct {
	UsageReport *struct {
		RateLimits struct {
			Limits []struct {
				Kind     string   `json:"kind"`
				Percent  *float64 `json:"percent"`
				ResetsAt string   `json:"resets_at"`
				Scope    struct {
					Model struct {
						DisplayName string `json:"display_name"`
					} `json:"model"`
				} `json:"scope"`
			} `json:"limits"`
		} `json:"rate_limits"`
	} `json:"usage_report"`
}

// ParseClaudeReport reads the CLI's local /usage command result and classifies failures.
func ParseClaudeReport(raw []byte, now time.Time) (account proto.UsageAccount, retry bool) {
	var report *claudeUsageEvent
	text := string(raw)
	for rest := text; rest != ""; {
		line, next, _ := strings.Cut(rest, "\n")
		candidate := strings.TrimSpace(line)
		if strings.HasPrefix(candidate, "[") || strings.HasPrefix(candidate, "{") {
			// Verbose JSON can be an array of events after the wrapper's preamble.
			if report = decodeClaudeUsageEvent(rest); report != nil {
				break
			}
		}
		rest = next
	}
	if report == nil {
		message := strings.ToLower(text)
		for _, refusal := range []string{"not logged in", "run /login", "command not found", "unknown option", "unknown slash command", "managed-cli-setting-refused"} {
			if strings.Contains(message, refusal) {
				return account, false
			}
		}
		return account, true
	}
	var windows []proto.UsageWindow
	for _, limit := range report.UsageReport.RateLimits.Limits {
		if limit.Percent == nil || math.IsInf(*limit.Percent, 0) || math.IsNaN(*limit.Percent) {
			continue
		}
		window := proto.UsageWindow{UsedPercent: min(100, max(0, *limit.Percent))}
		switch limit.Kind {
		case "session":
			window.WindowMinutes = 300
		case "weekly_all":
			window.WindowMinutes = 10080
		case "weekly_scoped":
			window.WindowMinutes = 10080
			window.Model = strings.TrimSpace(limit.Scope.Model.DisplayName)
			if window.Model == "" {
				continue
			}
		default:
			continue
		}
		window.Label = proto.WindowLabel(window.WindowMinutes)
		if limit.ResetsAt != "" {
			reset, err := time.Parse(time.RFC3339Nano, limit.ResetsAt)
			if err != nil || !reset.After(now) {
				continue
			}
			reset = reset.UTC().Round(time.Second)
			window.ResetsAt = &reset
		}
		windows = append(windows, window)
	}
	slices.SortStableFunc(windows, func(a, b proto.UsageWindow) int { return a.WindowMinutes - b.WindowMinutes })
	if len(windows) > 0 {
		account = proto.UsageAccount{Provider: "claude", Name: "Claude", SampledAt: now.UTC(), Windows: windows}
	}
	return account, false
}

func decodeClaudeUsageEvent(text string) *claudeUsageEvent {
	var value json.RawMessage
	if json.NewDecoder(strings.NewReader(text)).Decode(&value) != nil {
		return nil
	}
	var events []claudeUsageEvent
	if value[0] == '[' {
		if json.Unmarshal(value, &events) != nil {
			return nil
		}
	} else {
		var event claudeUsageEvent
		if json.Unmarshal(value, &event) != nil {
			return nil
		}
		events = append(events, event)
	}
	for _, event := range events {
		if event.UsageReport != nil {
			return &event
		}
	}
	return nil
}
