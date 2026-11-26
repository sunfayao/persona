package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
)

// trackKindMemStore is a trackRegistryStore that also keeps the user's kinds.
type trackKindMemStore struct {
	*trackRegistryStore
	kmu   sync.Mutex
	kinds []db.TrackKind
}

func (st *trackKindMemStore) TrackKinds(context.Context) ([]db.TrackKind, error) {
	st.kmu.Lock()
	defer st.kmu.Unlock()
	return slices.Clone(st.kinds), nil
}

// validKind is a valid kind config to change field by field.
func validKind() assistantwire.TrackKindConfig { return builtinKindConfigs()[trackKindSpace] }

// Kind configs (#13472): the built-in ones are valid; every field takes only its options; a newer schema is not
// enabled; a field this manager does not know is refused.
func TestKindConfigValidation(t *testing.T) {
	for slug, c := range builtinKindConfigs() {
		got, err := normalKindConfig(c)
		if err != nil || !reflect.DeepEqual(got, c) {
			t.Errorf("built-in %s: %v (%+v)", slug, err, got)
		}
	}
	bad := map[string]func(c *assistantwire.TrackKindConfig){
		"template":     func(c *assistantwire.TrackKindConfig) { c.Template = "odd" },
		"context":      func(c *assistantwire.TrackKindConfig) { c.Context = "everything" },
		"supervision":  func(c *assistantwire.TrackKindConfig) { c.Supervision = "boss" },
		"assembly":     func(c *assistantwire.TrackKindConfig) { c.Assembly = "" },
		"push_cap":     func(c *assistantwire.TrackKindConfig) { c.Attention.PushCap = "P9" },
		"deep_default": func(c *assistantwire.TrackKindConfig) { c.Attention.DeepDefault = "deeper" },
		"enter_mode":   func(c *assistantwire.TrackKindConfig) { c.Attention.EnterMode = "always" },
		"channel":      func(c *assistantwire.TrackKindConfig) { c.Channels = []string{"broadcast"} },
		"category":     func(c *assistantwire.TrackKindConfig) { c.DefaultCategories = []string{"passwords"} },
		"tag_focus":    func(c *assistantwire.TrackKindConfig) { c.TagFocus = []string{"someday"} },
		"icon":         func(c *assistantwire.TrackKindConfig) { c.Sidebar.Icon = "heart.fill" },
		"schema":       func(c *assistantwire.TrackKindConfig) { c.Schema = assistantwire.TrackKindSchema + 1 },
		"cron": func(c *assistantwire.TrackKindConfig) {
			c.Rhythms = []assistantwire.TrackRhythm{{Title: "Weekly", Cron: "every monday", Prompt: "Review", TopicPolicy: "new"}}
		},
		"tz": func(c *assistantwire.TrackKindConfig) {
			c.Rhythms = []assistantwire.TrackRhythm{{Title: "Weekly", Cron: "0 9 * * 1", TZ: "Mars/Olympus", Prompt: "Review", TopicPolicy: "new"}}
		},
		"topic_policy": func(c *assistantwire.TrackKindConfig) {
			c.Rhythms = []assistantwire.TrackRhythm{{Title: "Weekly", Cron: "0 9 * * 1", Prompt: "Review", TopicPolicy: "sometimes"}}
		},
	}
	for name, change := range bad {
		c := validKind()
		change(&c)
		if _, err := normalKindConfig(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	c := validKind()
	c.Rhythms = []assistantwire.TrackRhythm{{Title: "Weekly", Cron: "0 9 * * 1", TZ: "UTC", Prompt: "Review the week", TopicPolicy: "continue", Enabled: true}}
	c.DefaultCategories = []string{"finance", "finance"}
	got, err := normalKindConfig(c)
	if err != nil || !slices.Equal(got.DefaultCategories, []string{"finance"}) {
		t.Errorf("a valid rhythm and categories: %v %+v", err, got.DefaultCategories)
	}
	if _, err := parseKindConfig([]byte(`{"schema": 2, "whatever": true}`)); !errors.Is(err, errKindSchema) {
		t.Errorf("a newer schema: %v, want errKindSchema", err)
	}
	raw, _ := json.Marshal(validKind())
	withExtra := strings.Replace(string(raw), `{`, `{"later_field": 1, `, 1)
	if _, err := parseKindConfig([]byte(withExtra)); err == nil {
		t.Error("a field this manager does not know: accepted")
	}
	if got, err := parseKindConfig(raw); err != nil || !reflect.DeepEqual(got, validKind()) {
		t.Errorf("a stored config reads back: %v", err)
	}
}

// Usage (acceptance 24): a kind or a track only tightens how information is used, never loosens it; "*" covers the
// strictest category.
func TestKindUsageOnlyTightens(t *testing.T) {
	for _, tc := range []struct {
		scene, category, rule string
		ok                    bool
	}{
		{"elsewhere", "finance", "deny", true},
		{"elsewhere", "finance", "minimal", true},
		{"elsewhere", "finance", "relevant", false},
		{"elsewhere", "*", "relevant", false},
		{"elsewhere", "*", "redact", true},
		{"source_track", "*", "allow", true},
		{"source_track", "health", "confirm", true},
		{"lock_screen", "emotion", "allow", false},
		{"lock_screen", "emotion", "redact", true},
		{"others", "legal_id", "redact", false},
		{"others", "legal_id", "confirm", true},
		{"kb_write", "*", "confirm", false},
		{"kb_write", "*", "deny", true},
		{"extension", "relationships", "deny", true},
		{"nowhere", "finance", "deny", false},
		{"elsewhere", "passwords", "deny", false},
		{"elsewhere", "finance", "sometimes", false},
	} {
		err := tightensUsage(assistantwire.TrackUsageRule{Scene: tc.scene, Category: tc.category, Rule: tc.rule})
		if (err == nil) != tc.ok {
			t.Errorf("%s/%s/%s: %v, want ok=%v", tc.scene, tc.category, tc.rule, err, tc.ok)
		}
	}
}

// A track's override (#13472): only the fields a track may set, the sidebar's icon only; its usage comes on top of
// its kind's; attention changes only the fields given.
func TestTrackOverride(t *testing.T) {
	kind := validKind()
	kind.Usage = []assistantwire.TrackUsageRule{{Scene: "kb_write", Category: "*", Rule: "deny"}}
	got, err := effectiveTrackConfig(kind, trackTemplateSpace, []string{"finance"},
		[]byte(`{"attention": {"push_cap": "P2"}, "usage": [{"scene": "lock_screen", "category": "*", "rule": "redact"}], "sidebar": {"icon": "wallet"}, "capture_target": false}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Attention.PushCap != "P2" || got.Attention.DeepDefault != "quick" || len(got.Usage) != 2 || got.Sidebar.Icon != "wallet" ||
		got.Sidebar.Group != "spaces" || got.CaptureTarget || !slices.Equal(got.DefaultCategories, []string{"finance"}) {
		t.Errorf("effective = %+v", got)
	}
	for _, o := range []string{
		`{"template": "health"}`,
		`{"default_categories": ["health"]}`,
		`{"supervision": "leaders"}`,
		`{"sidebar": {"group": "elsewhere"}}`,
		`{"usage": [{"scene": "elsewhere", "category": "*", "rule": "allow"}]}`,
		`{"attention": {"push_cap": "P7"}}`,
		`[1, 2]`,
	} {
		if _, err := effectiveTrackConfig(kind, trackTemplateSpace, nil, []byte(o)); err == nil {
			t.Errorf("override %s: accepted", o)
		}
	}
	if _, err := effectiveTrackConfig(kind, "odd", nil, nil); err == nil {
		t.Error("an unknown template: accepted")
	}
}
