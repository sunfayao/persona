package codex

import (
	"encoding/json"
)

// A missing serviceTier is unknown. Native null means Standard, and older
// Codex versions used "fast" for the tier now advertised as "priority".
func ServiceTier(raw json.RawMessage) string {
	var tier string
	if len(raw) == 0 || json.Unmarshal(raw, &tier) != nil {
		return ""
	}
	switch tier {
	case "":
		return "default"
	case "fast":
		return "priority"
	default:
		return tier
	}
}
