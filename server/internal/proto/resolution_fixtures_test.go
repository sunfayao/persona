package proto

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/nwverse/persona/server/internal/screen"
)

type answerFixture struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Source       []string `json:"source"`
	FromTest     string   `json:"from_test"`
	Derived      bool     `json:"derived"`
	DesignChange bool     `json:"design_change"`
	Input        struct {
		Profile     string          `json:"profile"`
		Interaction json.RawMessage `json:"interaction"`
		Entry       string          `json:"entry"`
		Kind        string          `json:"kind"`
		Locked      bool            `json:"locked"`
	} `json:"input"`
	Expected struct {
		Allowed    bool            `json:"allowed"`
		Resolution json.RawMessage `json:"resolution"`
		Actions    []struct {
			ID             string `json:"id"`
			Mode           string `json:"mode"`
			Authentication string `json:"authentication"`
			RequiresUnlock *bool  `json:"requires_unlock"`
			AvailableNow   *bool  `json:"available_now"`
		} `json:"actions"`
	} `json:"expected"`
}

func answerFixtureRoot(t *testing.T) string {
	t.Helper()
	// go test runs in the package directory; runtime.Caller is not usable under -trimpath.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "../../.."))
}

func answerFixtures(t *testing.T, domain string) []answerFixture {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(answerFixtureRoot(t), "spec/fixtures", domain, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("fixture files: %v", err)
	}
	var all []answerFixture
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			SchemaVersion int             `json:"schema_version"`
			Cases         []answerFixture `json:"cases"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if doc.SchemaVersion != 1 || len(doc.Cases) == 0 {
			t.Fatalf("invalid fixture document: %s", file)
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, bytes.TrimSpace(data), "", "  "); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(append(pretty.Bytes(), '\n'), data) {
			t.Fatalf("expected two-space JSON: %s", file)
		}
		all = append(all, doc.Cases...)
	}
	return all
}

func TestResolutionFixtures(t *testing.T) {
	names := map[string]bool{}
	policies := map[string]int{}
	for _, domain := range []string{"answer", "question-review", "approval"} {
		for _, c := range answerFixtures(t, domain) {
			t.Run(c.Name, func(t *testing.T) {
				if !regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`).MatchString(c.Name) {
					t.Fatal("name must be snake_case")
				}
				if names[c.Name] {
					t.Fatal("duplicate fixture name")
				}
				names[c.Name] = true
				if c.Description == "" || len(c.Source) == 0 {
					t.Fatal("missing provenance")
				}
				flags := 0
				if c.Derived {
					flags++
				}
				if c.DesignChange {
					flags++
				}
				if c.FromTest != "" {
					flags++
				}
				if flags != 1 {
					t.Fatal("exactly one provenance marker is required")
				}
				if (c.Input.Profile == "unified") != c.DesignChange {
					t.Fatal("design changes must be explicit")
				}
				if domain == "approval" {
					key := c.Input.Profile + "/" + c.Input.Entry + "/" + c.Input.Kind
					if c.Input.Locked {
						key += "/locked"
					} else {
						key += "/unlocked"
					}
					policies[key]++
					if len(c.Expected.Actions) == 0 {
						t.Fatal("missing policy actions")
					}
					ids := map[string]bool{}
					for _, a := range c.Expected.Actions {
						if ids[a.ID] {
							t.Fatal("duplicate action")
						}
						ids[a.ID] = true
						if !c.Input.Locked && (a.RequiresUnlock == nil || *a.RequiresUnlock) {
							t.Fatal("unlocked entry must not request unlock")
						}
						if a.RequiresUnlock != nil && (a.AvailableNow == nil || *a.RequiresUnlock == *a.AvailableNow) {
							t.Fatal("unlock and immediate availability disagree")
						}
						if a.Authentication == "required" && c.Input.Locked && (a.RequiresUnlock == nil || !*a.RequiresUnlock) {
							t.Fatal("locked authenticated action must require unlock")
						}
					}
					return
				}
				var ix Interaction
				if err := json.Unmarshal(c.Input.Interaction, &ix); err != nil {
					t.Fatal(err)
				}
				if ix.Dialog != nil {
					parsed := screen.ParseDialog(ix.Screen)
					if parsed != nil {
						parsed.Cursor = ""    // where the ❯ is: the hub's own, never on the wire
						parsed.LegacyRev = "" // the rev older hubs and nodes compute (#5961): never on the wire either (#5970)
					}
					if !reflect.DeepEqual(parsed, ix.Dialog) {
						t.Fatalf("screen parser differs from fixture dialog: %+v / %+v", parsed, ix.Dialog)
					}
				}
				if ix.ID == "" || ix.Kind == "" {
					t.Fatal("missing interaction identity")
				}
				raw := c.Expected.Resolution
				if len(raw) == 0 {
					t.Fatal("missing resolution (use null for no request)")
				}
				if bytes.Equal(raw, []byte("null")) {
					return
				}
				if !c.Expected.Allowed {
					t.Fatal("disabled operation must not emit a resolution")
				}
				var r Resolution
				decoder := json.NewDecoder(bytes.NewReader(raw))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&r); err != nil {
					t.Fatalf("hub Resolution decoder: %v", err)
				}
				encoded, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				var before, after map[string]any
				if err := json.Unmarshal(raw, &before); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(encoded, &after); err != nil {
					t.Fatal(err)
				}
				// Swift encodes non-nil empty strings; Go omits their zero values.
				for k, v := range before {
					if v == "" && (k == "feedback" || k == "message" || k == "text") {
						delete(before, k)
					}
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("wire fields lost: %s -> %s", raw, encoded)
				}
				if r.Dismiss {
					return
				}
				switch ix.Kind {
				case IxQuestion:
					if len(r.Answers) != len(ix.Questions) {
						t.Fatal("every question needs an answer")
					}
					for _, q := range ix.Questions {
						if r.Answers[q.Question] == "" {
							t.Fatal("missing original question key")
						}
					}
				case IxPlan:
					if r.Approve == nil {
						t.Fatal("approve must remain a boolean pointer, including false")
					}
					if *r.Approve && r.Mode != "acceptEdits" && r.Mode != "bypassPermissions" && r.Mode != "default" {
						t.Fatal("explicit supported mode required")
					}
				case IxPermission:
					if r.Allow == nil {
						t.Fatal("allow must remain a boolean pointer, including false")
					}
				case IxScreen:
					if r.Option != "" && (ix.Dialog == nil || ix.Dialog.Option(r.Option) == nil) {
						t.Fatal("option must exist in hub dialog")
					}
					if r.Option != "" && (len(r.Keys) != 0 || len(r.PreKeys) != 0) {
						t.Fatal("hub options must not include legacy keys")
					}
				default:
					t.Fatalf("unknown interaction kind: %s", ix.Kind)
				}
			})
		}
	}
	for _, name := range reviewConfirmedCases {
		if !names[name] {
			t.Errorf("review-confirmed case missing: %s", name)
		}
	}
	for _, kind := range []string{IxPlan, IxPermission, IxQuestion, IxScreen} {
		if policies["unified/conversation/"+kind+"/unlocked"] != 1 {
			t.Errorf("missing or duplicate conversation policy for %s", kind)
		}
	}
}

// reviewConfirmedCases are the questions.json cases whose expected Resolution is what the review's Submit sends: on a
// card of several questions their click opens the review (#2970), and the replay then confirms it (#8483).
// spec/fixtures/question-review/review.json asserts the click itself. Every other question case must leave the review
// closed.
var reviewConfirmedCases = []string{"multiple_questions_send_original_keys", "all_answered_can_submit_from_earlier_page"}
