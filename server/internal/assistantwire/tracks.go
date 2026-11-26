package assistantwire

// TrackKindSchema is the version of TrackKindConfig this assistant knows. A kind whose config says a higher one is
// not enabled: its tracks are left out until the assistant is updated.
const TrackKindSchema = 1

// TrackKindConfig is what a track of a kind does, each field one of the options the assistant defines (a new option is
// a code change; a new combination of options is a new kind, config only). Nothing branches on a kind's name.
type TrackKindConfig struct {
	// Schema is the config's version (TrackKindSchema).
	Schema int `json:"schema"`
	// Template is the instructions the track's conversations get: "assistant" (the main chat's), "space" (a short
	// baseline plus the track's SPACE.md) or "health". A track may set its own.
	Template string `json:"template"`
	// UserInstructions is "space_md" when the track keeps a SPACE.md the user writes, appended to its instructions;
	// "" when it has none.
	UserInstructions string `json:"user_instructions,omitempty"`
	// Context is what each message carries: "engineering" (the main chat's snapshot), "track" (a short one of the
	// track) or "none".
	Context string `json:"context"`
	// DefaultCategories are the information categories everything produced in the track carries at least (a tagging
	// floor; collecting is never limited). A track may set its own.
	DefaultCategories []string `json:"default_categories"`
	// TagFocus are categories the tagging model looks for especially in the track. A track may set its own.
	TagFocus []string `json:"tag_focus"`
	// Rhythms are the scheduled tasks a new track of the kind gets. A track may set its own (only when it is created).
	Rhythms []TrackRhythm `json:"rhythms"`
	// Attention is how loud the track may be. A track may set its own.
	Attention TrackAttention `json:"attention"`
	// Supervision is who reviews the track's work: "leaders" (directions and leaders: the main chat only for now),
	// "routine" (its scheduled tasks) or "none".
	Supervision string `json:"supervision"`
	// Usage tightens how the track's information is used, rule by rule; never looser than the assistant's own rules. A
	// track may tighten more.
	Usage []TrackUsageRule `json:"usage"`
	// Assembly is what the memory assembler gives the track's conversations: "assistant" (the main chat's) or "track"
	// (personal entries and the track's own).
	Assembly string `json:"assembly"`
	// CaptureTarget: what the user captures may be filed into the track. A track may set its own.
	CaptureTarget bool `json:"capture_target"`
	// Channels are the chat bindings the track takes: "topics" and "single".
	Channels []string `json:"channels"`
	// Sidebar is where the apps list the track. A track may set its own icon.
	Sidebar TrackSidebar `json:"sidebar"`
}

// TrackRhythm is one scheduled task a new track gets.
type TrackRhythm struct {
	Title string `json:"title"`
	// Cron is five fields; TZ an IANA zone ("" the user's).
	Cron   string `json:"cron"`
	TZ     string `json:"tz,omitempty"`
	Prompt string `json:"prompt"`
	// TopicPolicy is "new" (a new conversation each run) or "continue" (one conversation, continued).
	TopicPolicy string `json:"topic_policy"`
	Enabled     bool   `json:"enabled"`
}

// TrackAttention is how loud a track may be.
type TrackAttention struct {
	// PushCap is the highest level a push of the track that is not a card may have: "P0", "P1" or "P2".
	PushCap string `json:"push_cap"`
	// DeepDefault is a card's default depth: "quick" or "deep".
	DeepDefault string `json:"deep_default"`
	// EnterMode is the mode the apps suggest when the track is opened: "none" or "immersive". Never switched to on its
	// own.
	EnterMode string `json:"enter_mode"`
}

// TrackUsageRule tightens one scene for the track's information of one category ("*" every one).
type TrackUsageRule struct {
	// Scene is "source_track", "elsewhere", "hand_off", "lock_screen", "others", "extension" or "kb_write".
	Scene    string `json:"scene"`
	Category string `json:"category"`
	// Rule is "allow", "relevant", "minimal", "redact", "confirm" or "deny".
	Rule string `json:"rule"`
}

// TrackSidebar is where the apps list a track: tracks of one group in one section, by order.
type TrackSidebar struct {
	Group string `json:"group,omitempty"`
	Order int    `json:"order,omitempty"`
	Icon  string `json:"icon,omitempty"`
}
