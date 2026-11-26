package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The converged wire names (#11719): applies_when and candidates, with priority. The old names still decode, reads
// answer both, and a body whose two names disagree is refused — including on the nested start_defaults of PUT
// /api/v1/assistant, which has no DisallowUnknownFields of its own.
func TestStartDefaultsConvergedNamesAndAliases(t *testing.T) {
	s, f := defaultsHub(t)
	ctx := context.Background()
	// New names, with priority: the write works and reaches a real start.
	body := `{"start_defaults":{"rules":[{"applies_when":{"role":"worker"},"then":{
		"candidates":[{"name":"quick","priority":1},{"name":"deep","priority":5}],"effort":"max"}}]}}`
	if code, got := putStartDefaults(t, s, roleFull, "", body); code != http.StatusOK {
		t.Fatalf("converged write: %d %s", code, got)
	}
	// Reads carry both names with the same content, so either generation of client round-trips it.
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/start-defaults", nil))
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	rule := got["start_defaults"].(map[string]any)["rules"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(rule["when"], rule["applies_when"]) {
		t.Fatalf("when and applies_when differ: %v", rule)
	}
	then := rule["then"].(map[string]any)
	if !reflect.DeepEqual(then["presets"], then["candidates"]) {
		t.Fatalf("presets and candidates differ: %v", then)
	}
	if c := then["candidates"].([]any)[1].(map[string]any); c["priority"] != float64(5) {
		t.Fatalf("priority not carried: %v", c)
	}
	task := defaultsTask()
	if _, _, err := s.startTaskAgent(ctx, &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["model"] != "gw-deep" || st["effort"] != "max" {
		t.Fatalf("the converged policy did not apply: %v", st)
	}
	// The stored policy (Postgres JSON) reads back after carrying both names.
	if p := s.activeStartDefaults(ctx); p == nil || p.Rules[0].When.Role != "worker" || p.Rules[0].Then.Presets[1].Priority != 5 {
		t.Fatalf("the stored policy did not round-trip: %+v", p)
	}
	// Old names still write, on this route and nested in PUT /api/v1/assistant.
	if code, got := putStartDefaults(t, s, roleFull, "", `{"start_defaults":{"rules":[{"when":{},"then":{"presets":[{"name":"quick"}]}}]}}`); code != http.StatusOK {
		t.Fatalf("legacy write: %d %s", code, got)
	}
	nested := httptest.NewRequest(http.MethodPut, "/api/v1/assistant", strings.NewReader(
		`{"start_defaults":{"rules":[{"applies_when":{},"then":{"candidates":[{"name":"deep"}]}}]}}`))
	nested.Header.Set(RoleHeader, roleFull)
	nrec := httptest.NewRecorder()
	s.ServeHTTP(nrec, nested)
	if nrec.Code != http.StatusOK {
		t.Fatalf("nested converged write: %d %s", nrec.Code, nrec.Body.String())
	}
	if p := s.activeStartDefaults(ctx); p == nil || p.Rules[0].Then.Presets[0].Name != "deep" {
		t.Fatalf("the nested write did not store the candidates: %+v", p)
	}
}

// A body that names both generations of a field is accepted when they agree (that is what a read answers) and
// refused when they disagree; a misspelt field is still a 400 everywhere, aliases included.
func TestStartDefaultsAliasConflictsAndUnknownFields(t *testing.T) {
	s, _ := defaultsHub(t)
	for _, c := range []struct {
		name, body string
		code       int
	}{
		{"both matches agree", `{"start_defaults":{"rules":[{"when":{"role":"worker"},"applies_when":{"role":"worker"},"then":{"effort":"max"}}]}}`, http.StatusOK},
		{"both matches disagree", `{"start_defaults":{"rules":[{"when":{"role":"worker"},"applies_when":{"role":"leader"},"then":{"effort":"max"}}]}}`, http.StatusBadRequest},
		{"both lists agree", `{"start_defaults":{"rules":[{"when":{},"then":{"presets":[{"name":"deep"}],"candidates":[{"name":"deep"}]}}]}}`, http.StatusOK},
		{"both lists disagree", `{"start_defaults":{"rules":[{"when":{},"then":{"presets":[{"name":"deep"}],"candidates":[{"name":"quick"}]}}]}}`, http.StatusBadRequest},
		{"misspelt applies_when", `{"start_defaults":{"rules":[{"applies_whenx":{},"then":{"effort":"max"}}]}}`, http.StatusBadRequest},
		{"misspelt candidates", `{"start_defaults":{"rules":[{"when":{},"then":{"candidatez":[{"name":"deep"}]}}]}}`, http.StatusBadRequest},
		{"unknown match field", `{"start_defaults":{"rules":[{"applies_when":{"rol":"worker"},"then":{"effort":"max"}}]}}`, http.StatusBadRequest},
		// An explicit null is present on the wire and must not slip past the conflict check the way a decoded nil
		// pointer would (#11719 review): a null beside a real value is a disagreement.
		{"null match beside a real match", `{"start_defaults":{"rules":[{"when":null,"applies_when":{"role":"leader"},"then":{"effort":"max"}}]}}`, http.StatusBadRequest},
		{"real match beside a null match", `{"start_defaults":{"rules":[{"when":{"role":"worker"},"applies_when":null,"then":{"effort":"max"}}]}}`, http.StatusBadRequest},
		{"null match beside an empty match", `{"start_defaults":{"rules":[{"when":null,"applies_when":{},"then":{"effort":"max"}}]}}`, http.StatusBadRequest},
		{"empty match beside a null match", `{"start_defaults":{"rules":[{"when":{},"applies_when":null,"then":{"effort":"max"}}]}}`, http.StatusBadRequest},
		{"null match beside null fields", `{"start_defaults":{"rules":[{"when":null,"applies_when":{"role":null},"then":{"effort":"max"}}]}}`, http.StatusBadRequest},
		{"null list beside a real list", `{"start_defaults":{"rules":[{"when":{},"then":{"presets":null,"candidates":[{"name":"deep"}]}}]}}`, http.StatusBadRequest},
		{"real list beside a null list", `{"start_defaults":{"rules":[{"when":{},"then":{"presets":[{"name":"deep"}],"candidates":null}}]}}`, http.StatusBadRequest},
		{"null list beside an empty list", `{"start_defaults":{"rules":[{"when":{},"then":{"presets":null,"candidates":[],"effort":"max"}}]}}`, http.StatusBadRequest},
		{"empty list beside a null list", `{"start_defaults":{"rules":[{"when":{},"then":{"presets":[],"candidates":null,"effort":"max"}}]}}`, http.StatusBadRequest},
		{"padded aliases still disagree", `{"start_defaults":{"rules":[{"when":{},"then":{"presets":[{"name":" deep "}],"candidates":[{"name":"deep"}]}}]}}`, http.StatusBadRequest},
		{"both matches null", `{"start_defaults":{"rules":[{"when":null,"applies_when":null,"then":{"effort":"max"}}]}}`, http.StatusOK},
		{"both lists null", `{"start_defaults":{"rules":[{"when":{},"then":{"presets":null,"candidates":null,"effort":"max"}}]}}`, http.StatusOK},
	} {
		t.Run(c.name, func(t *testing.T) {
			if code, got := putStartDefaults(t, s, roleFull, "", c.body); code != c.code {
				t.Fatalf("dedicated %s: %d %s", c.name, code, got)
			}
			req := httptest.NewRequest(http.MethodPut, "/api/v1/assistant", strings.NewReader(c.body))
			req.Header.Set(RoleHeader, roleFull)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != c.code {
				t.Fatalf("nested %s: %d %s", c.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// Priority orders the usable candidates: the highest priority with anything usable wins, however the list is ordered,
// and a group whose presets cannot run falls through to the next one. All-zero priorities keep the old order/weights.
func TestStartDefaultsPriorityOrdersAndFallsThrough(t *testing.T) {
	ctx := context.Background()
	// Higher priority wins even when it is listed last.
	s, f := defaultsHub(t)
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		return StartDefaults{Presets: []PresetChoice{{Name: "quick", Priority: 1}, {Name: "deep", Priority: 9}}}, true
	})
	task := defaultsTask()
	if _, _, err := s.startTaskAgent(ctx, &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["model"] != "gw-deep" {
		t.Fatalf("the higher priority did not win: %v", st)
	}
	// The top group's presets cannot run here: the next group is used, and the skip is named.
	s, f = defaultsHub(t)
	saveTiers(t, s, map[string]ModelTier{
		"elsewhere": {Launcher: "other", Model: "o-1"},
		"quick":     {Launcher: "gateway", Model: "gw-quick"},
		"plain":     {Launcher: "claude", Model: "claude-plain"},
	})
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		return StartDefaults{By: "Example", Presets: []PresetChoice{
			{Name: "elsewhere", Priority: 100}, {Name: "gone", Priority: 100},
			{Name: "plain", Priority: 2}, {Name: "quick", Priority: 1},
		}}, true
	})
	task = defaultsTask()
	task.MachinePolicy = machineFlexible
	if _, _, err := s.startTaskAgent(ctx, &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["model"] != "claude-plain" {
		t.Fatalf("the next priority did not take over: %v", st)
	}
	for _, name := range []string{"elsewhere", "gone"} {
		if !strings.Contains(task.Why, name) {
			t.Fatalf("the why does not name the passed-over %s: %q", name, task.Why)
		}
	}
	// Priority bounds are validated like weight's.
	for _, body := range []string{
		`{"start_defaults":{"rules":[{"when":{},"then":{"candidates":[{"name":"deep","priority":-1}]}}]}}`,
		`{"start_defaults":{"rules":[{"when":{},"then":{"candidates":[{"name":"deep","priority":1001}]}}]}}`,
	} {
		if code, got := putStartDefaults(t, s, roleFull, "", body); code != http.StatusBadRequest {
			t.Fatalf("out-of-range priority accepted: %d %s", code, got)
		}
	}
}

// Weights share starts only within the winning priority group: the roll's bound is that group's weight sum, never
// the whole list's, and a lower-priority candidate cannot be rolled into the top group's start.
func TestStartDefaultsWeightsWithinTheTopPriority(t *testing.T) {
	ctx := context.Background()
	old := presetIntN
	t.Cleanup(func() { presetIntN = old })
	var bounds []int
	presetIntN = func(n int) int { bounds = append(bounds, n); return n - 1 } // always the last of the rolled group
	s, f := defaultsHub(t)
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		return StartDefaults{Presets: []PresetChoice{
			{Name: "deep", Priority: 5, Weight: 1}, {Name: "quick", Priority: 5, Weight: 3},
			{Name: "plain", Priority: 1, Weight: 40}, // lower priority: never in the roll, whatever its weight
		}}, true
	})
	task := defaultsTask()
	if _, _, err := s.startTaskAgent(ctx, &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["model"] != "gw-quick" {
		t.Fatalf("the roll did not stay in the top group: %v", st)
	}
	if !slices.Equal(bounds, []int{4}) {
		t.Fatalf("roll bounds = %v, want the top group's weight sum 4", bounds)
	}
	// The production roll is the crypto-grade source and stays in range; the var stays injectable.
	presetIntN = old
	for range 64 {
		if got := presetIntN(7); got < 0 || got >= 7 {
			t.Fatalf("presetIntN out of range: %d", got)
		}
	}
}

// Availability lookups may deduplicate a repeated name, but the rule's choices themselves keep their original
// multiplicity and weights: quick(2), quick(1), deep(1) still rolls over four, not three.
func TestStartDefaultsDuplicateChoicesKeepTheirWeights(t *testing.T) {
	old := presetIntN
	t.Cleanup(func() { presetIntN = old })
	bound := 0
	presetIntN = func(n int) int { bound = n; return 0 }
	s, f := defaultsHub(t)
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		return StartDefaults{Presets: []PresetChoice{{Name: "quick", Weight: 2}, {Name: "quick", Weight: 1}, {Name: "deep", Weight: 1}}}, true
	})
	task := defaultsTask()
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["model"] != "gw-quick" || bound != 4 {
		t.Fatalf("duplicate choices changed the weighted roll: start=%v bound=%d", st, bound)
	}
}

// A retry re-evaluates the rule and re-rolls, keeping nothing of the earlier try's priority choice: the caller's own
// values are restored first, and a rule that no longer fits leaves no preset, effort or ultracode behind.
func TestStartDefaultsRetryRerollsAndKeepsNothingOfTheOldRule(t *testing.T) {
	ctx := context.Background()
	old := presetIntN
	t.Cleanup(func() { presetIntN = old })
	rolls := 0
	presetIntN = func(n int) int { rolls++; return n - 1 } // deep(1), quick(3): the last roll picks quick
	s, f := defaultsHub(t)
	first := true
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		if first {
			return StartDefaults{Ultracode: boolp(true), Effort: "max", Presets: []PresetChoice{
				{Name: "deep", Priority: 1}, {Name: "quick", Priority: 5, Weight: 2}, {Name: "plain", Priority: 5, Weight: 1},
			}}, true
		}
		return StartDefaults{}, false // the policy no longer applies at all
	})
	task := defaultsTask()
	if _, _, err := s.startTaskAgent(ctx, &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["model"] != "claude-plain" || st["ultracode"] != true || st["effort"] != "max" {
		t.Fatalf("the first rule did not apply: %v", st)
	}
	if rolls != 1 {
		t.Fatalf("the first try did not roll once: %d", rolls)
	}
	first = false
	if _, _, err := s.startTaskAgent(ctx, &task, true); err != nil {
		t.Fatal(err)
	}
	st := startsOf(f)[1]
	if st["model"] != "gw-model" || st["launcher"] != "gateway" || st["ultracode"] != nil || st["effort"] == "max" {
		t.Fatalf("the retry kept the old rule's values: %v", st)
	}
	if rolls != 1 {
		t.Fatalf("the retry rolled although no rule fits: %d", rolls)
	}
	if task.Ultracode != nil || task.Effort == "max" || task.Tier != "" {
		t.Fatalf("the task kept policy values: %+v", task)
	}
}

// Tier-resolution errors used by both resolveStart and preview keep their legacy English Error() text and expose the
// same typed message in every client language.
func TestTierModelResolutionErrorsLocalized(t *testing.T) {
	for _, tc := range []struct {
		name, tier, key, wantEN string
		args                    i18n.Args
		setup                   func(*testing.T, *Service)
	}{
		{name: "no active profile", tier: "quick", key: "assistant-model_profiles.err_no_active_profile", wantEN: "no model profile is in use: switch to one with model_profile_switch, or leave tier out", setup: func(t *testing.T, s *Service) {
			if err := s.putSetting(context.Background(), modelProfileStateKey, modelProfileState{}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "deleted active profile", tier: "quick", key: "assistant-model_profiles.err_active_profile_missing", wantEN: "the model profile in use (gone) no longer exists: switch to another with model_profile_switch, or leave tier out", args: i18n.Args{"profile": i18n.Verbatim("gone")}, setup: func(t *testing.T, s *Service) {
			putTierProfiles(t, s, "gone", map[string]ModelTier{"quick": {Launcher: "claude"}}, nil)
		}},
		{name: "unknown tier", tier: "typo", key: "assistant-model_profiles.err_tier_not_found", wantEN: "the model profile in use (P) has no tier typo; its tiers: deep, plain, quick", args: i18n.Args{"profile": i18n.Verbatim("P"), "tier": i18n.Verbatim("typo"), "tiers": i18n.Verbatim("deep, plain, quick")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := defaultsHub(t)
			if tc.setup != nil {
				tc.setup(t, s)
			}
			_, err := s.tierModel(context.Background(), tc.tier)
			if err == nil || err.Error() != tc.wantEN {
				t.Fatalf("legacy error = %v, want %q", err, tc.wantEN)
			}
			for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
				if got, want := i18n.Localize(err, lang), i18n.T(lang, tc.key, tc.args); got != want {
					t.Fatalf("%s error = %q, want %q", lang, got, want)
				}
			}
		})
	}
}

// NUL is never accepted into a policy that must be stored in PostgreSQL jsonb.
func TestStartDefaultsCandidateNameRejectsNUL(t *testing.T) {
	s, _ := defaultsHub(t)
	body, err := json.Marshal(map[string]any{"start_defaults": map[string]any{"rules": []any{map[string]any{
		"applies_when": map[string]any{}, "then": map[string]any{"candidates": []any{map[string]any{"name": "\x00"}}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	if code, out := putStartDefaults(t, s, roleFull, "", string(body)); code != http.StatusBadRequest {
		t.Fatalf("NUL candidate name: %d %s", code, out)
	}
}

// Alias conflicts keep their legacy English text without a language carrier and localize through the real relay.
func TestStartDefaultsAliasConflictsLocalized(t *testing.T) {
	for _, tc := range []struct {
		name, body, key string
	}{
		{name: "match", body: `{"start_defaults":{"rules":[{"when":null,"applies_when":{},"then":{"effort":"max"}}]}}`, key: "assistant-start_defaults.alias_match_conflict"},
		{name: "candidates", body: `{"start_defaults":{"rules":[{"when":{},"then":{"presets":null,"candidates":[],"effort":"max"}}]}}`, key: "assistant-start_defaults.alias_candidates_conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := defaultsHub(t)
			res, err := serveHTTP(context.Background(), s, assistantwire.HTTPRequest{
				Method: http.MethodPut, Path: "/api/v1/assistant/start-defaults", Body: []byte(tc.body),
				Header: map[string][]string{RoleHeader: {roleFull}, "Accept-Language": {string(i18n.Ja)}},
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Code  string `json:"code"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(res.Body, &got); err != nil {
				t.Fatal(err)
			}
			want := i18n.T(i18n.Ja, tc.key)
			if res.Status != http.StatusBadRequest || got.Code != "bad_request" || got.Error != want {
				t.Fatalf("status=%d body=%s; want error %q", res.Status, res.Body, want)
			}
		})
	}
}

// Speed validation is shared with real starts, so its legacy English text remains byte-identical while the same typed
// errors localize for preview callers. This also prevents a new English-only error from slipping in with tier fitting.
func TestStartSpeedErrorsLocalized(t *testing.T) {
	for _, tc := range []struct {
		name, key, wantEN string
		args              i18n.Args
		call              func() error
	}{
		{name: "type", key: "assistant-tasks.speed_type", wantEN: "speed must be a string: standard, fast or ultrafast", call: func() error {
			_, err := validateStartSpeed(map[string]any{"speed": true})
			return err
		}},
		{name: "invalid", key: "assistant-tasks.speed_invalid", wantEN: `invalid Agent speed "warp": use standard, fast or ultrafast`, args: i18n.Args{"speed": i18n.Verbatim(`"warp"`)}, call: func() error {
			_, err := validateStartSpeed(map[string]any{"speed": "warp"})
			return err
		}},
		{name: "resume", key: "assistant-tasks.speed_resume", wantEN: `speed "fast" applies only to a new Agent start, not a resume; leave speed out to use the launcher's own setting`, args: i18n.Args{"speed": i18n.Verbatim(`"fast"`)}, call: func() error {
			return checkStartSpeed(map[string]any{"speed": "fast"}, startAgent{agent: "claude"}, "session-1")
		}},
		{name: "unsupported", key: "assistant-tasks.speed_unsupported", wantEN: `launcher "gateway" (claude) cannot apply speed "ultrafast" to a new Agent start; leave speed out or choose standard`, args: i18n.Args{"launcher": i18n.Verbatim(`"gateway"`), "kind": i18n.Verbatim("claude"), "speed": i18n.Verbatim(`"ultrafast"`)}, call: func() error {
			return checkStartSpeed(map[string]any{"speed": "ultrafast", "launcher": "gateway"}, startAgent{agent: "claude"}, "")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil || err.Error() != tc.wantEN {
				t.Fatalf("legacy error = %v, want %q", err, tc.wantEN)
			}
			for _, lang := range []i18n.Lang{i18n.En, i18n.ZhHans, i18n.Ja} {
				if got, want := i18n.Localize(err, lang), i18n.T(lang, tc.key, tc.args); got != want {
					t.Fatalf("%s error = %q, want %q", lang, got, want)
				}
			}
		})
	}
}

// boolp is a pointer to b, for the retry test's ultracode.
func boolp(b bool) *bool { return &b }

// The candidate source seam: an injected source replaces the profile's data without touching matching or retry, and a
// preview and a real start read the same rows. A source that cannot be read leaves no preset, as the profile's error
// always has.
func TestStartDefaultsCandidateSourceSeam(t *testing.T) {
	ctx := context.Background()
	s, f := defaultsHub(t)
	var requested [][]string
	s.startCandidates = stubCandidateSource{requested: &requested, rows: map[string]startCandidate{
		"deep":  {Name: "deep", Launcher: "gateway", Available: false, Reason: "gateway is not available yet"},
		"quick": {Name: "quick", Launcher: "gateway", Available: true},
	}}
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		return StartDefaults{Presets: []PresetChoice{{Name: "deep", Priority: 5}, {Name: "quick", Priority: 1}}}, true
	})
	task := defaultsTask()
	if _, _, err := s.startTaskAgent(ctx, &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["model"] != "gw-quick" {
		t.Fatalf("the injected source was not used: %v", st)
	}
	if want := [][]string{{"deep", "quick"}}; !reflect.DeepEqual(requested, want) {
		t.Fatalf("real start requested candidates %v, want %v", requested, want)
	}
	// An unreadable source keeps the old fail-closed behavior: no preset, the ordinary start, no error.
	s2, f2 := defaultsHub(t)
	s2.startCandidates = stubCandidateSource{err: context.DeadlineExceeded}
	stubStartDefaults(s2, func(StartFacts) (StartDefaults, bool) {
		return StartDefaults{Presets: []PresetChoice{{Name: "deep"}}}, true
	})
	task = defaultsTask()
	if _, _, err := s2.startTaskAgent(ctx, &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f2)[0]; st["model"] != "gw-model" {
		t.Fatalf("an unreadable source did not fall back to the ordinary start: %v", st)
	}
}

// stubCandidateSource is a fixed candidate source for the seam test; err makes it unreadable.
type stubCandidateSource struct {
	rows      map[string]startCandidate
	err       error
	requested *[][]string
}

func (s stubCandidateSource) candidates(_ context.Context, _ string, names []string) (map[string]startCandidate, error) {
	if s.requested != nil {
		*s.requested = append(*s.requested, slices.Clone(names))
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}

// Candidate evaluation is bounded by the rule, not by unrelated profile tiers. One ordinary inventory read covers
// visible choices; explicitly requested hidden launchers get at most one unhidden read per distinct launcher.
func TestStartDefaultsCandidateSourceScopesAndCachesHidden(t *testing.T) {
	for _, tc := range []struct {
		name          string
		secretPresent bool
		choices       []PresetChoice
		wantTier      string
		wantRPCs      int
	}{
		{name: "visible choice ignores unrelated hidden tiers", secretPresent: true, choices: []PresetChoice{{Name: "quick"}}, wantTier: "quick", wantRPCs: 1},
		{name: "requested tiers share a present hidden probe", secretPresent: true, choices: []PresetChoice{{Name: "hidden-a"}, {Name: "hidden-b"}}, wantTier: "hidden-a", wantRPCs: 2},
		{name: "requested tiers share an absent hidden probe", secretPresent: false, choices: []PresetChoice{{Name: "hidden-a"}, {Name: "hidden-b"}}, wantRPCs: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := defaultsHub(t)
			ctx := context.Background()
			if tc.secretPresent {
				f.zshrc["box"] = append(f.zshrc["box"], proto.Launcher{Name: "secret", Source: "zshrc"})
			}
			if err := s.putSetting(ctx, launchersHiddenKey, []string{"secret"}); err != nil {
				t.Fatal(err)
			}
			tiers := map[string]ModelTier{
				"quick":    {Launcher: "gateway", Model: "gw-quick"},
				"hidden-a": {Launcher: "secret", Model: "secret-a"},
				"hidden-b": {Launcher: "secret", Model: "secret-b"},
			}
			for i := 0; i < 11; i++ {
				tiers[fmt.Sprintf("unrelated-%d", i)] = ModelTier{Launcher: "secret", Model: "secret-other"}
			}
			saveTiers(t, s, tiers)
			f.mu.Lock()
			before := len(f.calls)
			f.mu.Unlock()
			task := defaultsTask()
			params := map[string]any{}
			s.applyStartPreset(ctx, &task, startPick{}, StartDefaults{Presets: tc.choices}, params)
			f.mu.Lock()
			calls := 0
			for _, call := range f.calls[before:] {
				if call.Method == DeviceLaunchersList {
					calls++
				}
			}
			f.mu.Unlock()
			if got, _ := params["tier"].(string); got != tc.wantTier {
				t.Fatalf("selected tier %q, want %q", got, tc.wantTier)
			}
			if calls != tc.wantRPCs {
				t.Fatalf("launcher-list RPCs = %d, want %d", calls, tc.wantRPCs)
			}
		})
	}
}

// Hidden usability is cached per launcher, only for one evaluation. Opposite outcomes stay independent, and a later
// evaluation sees a launcher added after an earlier miss instead of inheriting a stale Service-wide false.
func TestStartDefaultsHiddenCandidateCacheIsPerLauncherAndAttempt(t *testing.T) {
	s, f := defaultsHub(t)
	ctx := context.Background()
	f.zshrc["box"] = append(f.zshrc["box"], proto.Launcher{Name: "present", Source: "zshrc"})
	if err := s.putSetting(ctx, launchersHiddenKey, []string{"present", "absent"}); err != nil {
		t.Fatal(err)
	}
	saveTiers(t, s, map[string]ModelTier{
		"present": {Launcher: "present", Model: "present-model"},
		"absent":  {Launcher: "absent", Model: "absent-model"},
	})
	f.mu.Lock()
	before := len(f.calls)
	f.mu.Unlock()
	rows, err := s.candidateSource().candidates(ctx, "box", []string{"present", "absent"})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	calls := 0
	for _, call := range f.calls[before:] {
		if call.Method == DeviceLaunchersList {
			calls++
		}
	}
	f.mu.Unlock()
	if calls != 3 {
		t.Fatalf("two distinct hidden launchers caused %d RPCs, want 3", calls)
	}
	if !rows["present"].Available || rows["absent"].Available || !strings.Contains(rows["absent"].Reason, "absent") {
		t.Fatalf("hidden availability leaked between launchers: %+v", rows)
	}

	rows, err = s.candidateSource().candidates(ctx, "box", []string{"absent"})
	if err != nil {
		t.Fatal(err)
	}
	if rows["absent"].Available {
		t.Fatalf("absent launcher unexpectedly became usable: %+v", rows["absent"])
	}
	f.mu.Lock()
	f.zshrc["box"] = append(f.zshrc["box"], proto.Launcher{Name: "absent", Source: "zshrc"})
	f.mu.Unlock()
	rows, err = s.candidateSource().candidates(ctx, "box", []string{"absent"})
	if err != nil {
		t.Fatal(err)
	}
	if !rows["absent"].Available {
		t.Fatalf("a later attempt reused the earlier attempt's false: %+v", rows["absent"])
	}
}

// The owner's own write still may omit the expiry, and an extension's still may not: the converged names did not
// loosen that guard, and a policy past its expiry matches nothing.
func TestStartDefaultsConvergedExpiryUnchanged(t *testing.T) {
	s, _ := defaultsHub(t)
	rule := `{"applies_when":{},"then":{"candidates":[{"name":"deep"}]}}`
	if code, got := putStartDefaults(t, s, roleExtension, "kit", `{"start_defaults":{"rules":[`+rule+`]}}`); code != http.StatusBadRequest {
		t.Fatalf("an extension's policy without expiry was accepted: %d %s", code, got)
	}
	soon := time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339)
	if code, got := putStartDefaults(t, s, roleExtension, "kit", `{"start_defaults":{"expires_at":"`+soon+`","rules":[`+rule+`]}}`); code != http.StatusOK {
		t.Fatalf("an expiring converged policy was refused: %d %s", code, got)
	}
	past := time.Now().Add(-time.Minute)
	setSettings(t, s, func(v *Settings) { v.StartDefaults.ExpiresAt = &past })
	if p := s.activeStartDefaults(context.Background()); p != nil {
		t.Fatalf("an expired policy is still in force: %+v", p)
	}
}

// A policy at the very top of every bound must still round-trip through the dual-name GET within the write body
// limit: 16 rules of 16 candidates each, every name 64 runes, every priority and weight at its maximum, and every
// supervisor class. The write is single-name (applies_when / candidates only), the read answers both names, and that
// read is itself a valid write body — the single-name-write → dual-name-read → write-back path the unbounded
// supervisor array used to break.
func TestStartDefaultsRoundTripStaysWithinBodyLimit(t *testing.T) {
	s, _ := defaultsHub(t)
	rules := make([]map[string]any, 0, startDefaultsMaxRules)
	for i := 0; i < startDefaultsMaxRules; i++ {
		cands := make([]map[string]any, 0, startDefaultsMaxPresets)
		for j := 0; j < startDefaultsMaxPresets; j++ {
			// '<' takes six JSON bytes per rune and, unlike NUL, is valid in PostgreSQL jsonb storage.
			cands = append(cands, map[string]any{"name": strings.Repeat("<", 64), "priority": startDefaultsMaxPriority, "weight": startDefaultsMaxWeight})
		}
		rules = append(rules, map[string]any{
			"applies_when": map[string]any{"role": "worker", "supervisor": []string{"main", "background_supervisor", "scheduled", "other"}},
			"then":         map[string]any{"candidates": cands, "effort": "max"},
		})
	}
	// Built as plain maps, so the body really is single-name (the custom marshaler would have written both names).
	body, err := json.Marshal(map[string]any{"start_defaults": map[string]any{"rules": rules}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"when"`) || strings.Contains(string(body), `"presets"`) {
		t.Fatal("the initial write is not single-name")
	}
	if code, got := putStartDefaults(t, s, roleFull, "", string(body)); code != http.StatusOK {
		t.Fatalf("the maximum single-name policy was refused: %d %s", code, got[:200])
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/start-defaults", nil))
	got := rec.Body.Bytes()
	if len(got) >= startDefaultsBodyLimit {
		t.Fatalf("the dual-name read is %d bytes, at or over the %d-byte write limit; a GET-to-PUT round-trip would break",
			len(got), startDefaultsBodyLimit)
	}
	// The read carries both names with equal values, so it is itself accepted as a write (this is what an older or
	// newer client does when it saves the policy back untouched).
	if code, out := putStartDefaults(t, s, roleFull, "", string(got)); code != http.StatusOK {
		t.Fatalf("the dual-name read could not be written back: %d %s", code, out[:200])
	}
}

// Candidate names are canonical on every decode path. In particular, unbounded outer whitespace cannot be accepted
// once and then duplicated under presets and candidates until the unchanged GET no longer fits the PUT body limit.
func TestStartDefaultsCandidateNameCanonicalizationRoundTrips(t *testing.T) {
	padded := strings.Repeat(" ", 140*1024) + "deep \n\t"
	write := func(t *testing.T, s *Service, route, body string) {
		t.Helper()
		if route == "dedicated" {
			if code, got := putStartDefaults(t, s, roleFull, "", body); code != http.StatusOK {
				t.Fatalf("dedicated write: %d %.200s", code, got)
			}
			return
		}
		req := httptest.NewRequest(http.MethodPut, "/api/v1/assistant", strings.NewReader(body))
		req.Header.Set(RoleHeader, roleFull)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("nested write: %d %.200s", rec.Code, rec.Body.String())
		}
	}
	check := func(t *testing.T, s *Service, route string) {
		t.Helper()
		if p := s.activeStartDefaults(context.Background()); p == nil || p.Rules[0].Then.Presets[0].Name != "deep" {
			t.Fatalf("candidate name was not canonicalized: %+v", p)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/start-defaults", nil))
		if rec.Code != http.StatusOK || rec.Body.Len() >= startDefaultsBodyLimit {
			t.Fatalf("canonical GET: status=%d bytes=%d limit=%d", rec.Code, rec.Body.Len(), startDefaultsBodyLimit)
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		then := got["start_defaults"].(map[string]any)["rules"].([]any)[0].(map[string]any)["then"].(map[string]any)
		for _, alias := range []string{"presets", "candidates"} {
			rows := then[alias].([]any)
			if len(rows) != 1 || rows[0].(map[string]any)["name"] != "deep" {
				t.Fatalf("%s retained a padded name: %v", alias, rows)
			}
		}
		t.Logf("route=%s dual-name GET bytes=%d limit=%d", route, rec.Body.Len(), startDefaultsBodyLimit)
		write(t, s, route, rec.Body.String())
	}
	body, err := json.Marshal(map[string]any{"start_defaults": map[string]any{"rules": []any{map[string]any{
		"applies_when": map[string]any{},
		"then":         map[string]any{"candidates": []any{map[string]any{"name": padded}}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"dedicated", "nested"} {
		t.Run(route, func(t *testing.T) {
			s, _ := defaultsHub(t)
			write(t, s, route, string(body))
			check(t, s, route)
		})
	}
	t.Run("legacy stored policy", func(t *testing.T) {
		s, _ := defaultsHub(t)
		ctx := context.Background()
		raw, err := json.Marshal(map[string]any{
			"notes": "preserve me",
			"start_defaults": map[string]any{"by": "owner", "rules": []any{map[string]any{
				"when": map[string]any{"role": "worker", "readonly": false},
				"then": map[string]any{"presets": []any{map[string]any{"name": padded, "weight": 2}}, "ultracode": false},
			}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.store().PutSetting(ctx, settingsKey, json.RawMessage(raw)); err != nil {
			t.Fatal(err)
		}
		s.prefs.mu.Lock()
		s.prefs.at = time.Time{}
		s.prefs.mu.Unlock()
		v, ok := s.savedSettings(ctx)
		if !ok || v.Notes == nil || *v.Notes != "preserve me" || v.StartDefaults == nil {
			t.Fatalf("legacy settings did not survive: %+v", v)
		}
		rule := v.StartDefaults.Rules[0]
		if rule.When.Role != "worker" || rule.When.Readonly == nil || *rule.When.Readonly || rule.Then.Ultracode == nil || *rule.Then.Ultracode || rule.Then.Presets[0].Weight != 2 {
			t.Fatalf("legacy fields changed: %+v", rule)
		}
		check(t, s, "dedicated")
	})
}

// Accepting both aliases with equal values keeps their content: the stored rule still scopes to the role it named,
// so a regression that dropped the match on a both-names write would be caught.
func TestStartDefaultsAliasAgreementKeepsTheMatch(t *testing.T) {
	s, _ := defaultsHub(t)
	if code, got := putStartDefaults(t, s, roleFull, "",
		`{"start_defaults":{"rules":[{"when":{"role":"worker"},"applies_when":{"role":"worker"},"then":{"effort":"max"}}]}}`); code != http.StatusOK {
		t.Fatalf("both-names write: %d %s", code, got)
	}
	p := s.activeStartDefaults(context.Background())
	if p == nil || len(p.Rules) != 1 || p.Rules[0].When.Role != "worker" {
		t.Fatalf("the both-names write lost the match: %+v", p)
	}
	if startDefaultsMatch(p.Rules[0].When, StartFacts{Role: "leader"}) {
		t.Fatal("the stored rule now matches leaders too")
	}
	if !startDefaultsMatch(p.Rules[0].When, StartFacts{Role: "worker"}) {
		t.Fatal("the stored rule no longer matches workers")
	}
}

// The production roll really draws from the trusted entropy source: point it at a failing reader and the roll must
// surface that failure (the package's panic precedent), never fall back to a predictable value.
func TestPresetIntNReadsTheTrustedEntropySource(t *testing.T) {
	oldReader, oldRoll := presetEntropy, presetIntN
	t.Cleanup(func() { presetEntropy, presetIntN = oldReader, oldRoll })
	presetIntN = oldRoll // the production implementation, not another test's stub
	presetEntropy = failingEntropy{}
	defer func() {
		if recover() == nil {
			t.Fatal("a failing entropy source did not fail the roll: the value came from somewhere else")
		}
	}()
	presetIntN(4)
}

// failingEntropy always fails, to prove the roll reads the entropy source.
type failingEntropy struct{}

func (failingEntropy) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

// Candidate agent metadata follows real configured, last-seen and managed-launcher sources. A wrapper absent from all
// readable metadata stays unknown rather than being guessed as Claude.
func TestStartDefaultsCandidateAgentMetadataDoesNotGuessUnknownWrappers(t *testing.T) {
	s, _ := defaultsHub(t)
	ctx := context.Background()
	if err := s.putSetting(ctx, launchersZshrcKey, map[string][]proto.Launcher{
		"box": {{Name: "seen", Source: "zshrc", Agent: "codex"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.putSetting(ctx, launchersManagedKey, []storedLauncher{{
		ManagedLauncher: proto.ManagedLauncher{Name: "direct", Kind: "api", Agent: "codex", BaseURL: "http://example.test"},
	}}); err != nil {
		t.Fatal(err)
	}
	lastSeen := s.candidateAgentLookup(ctx, "box", nil, false)
	for name, want := range map[string]string{"claude": "claude", "seen": "codex", "direct": "codex", "absent": ""} {
		if got := lastSeen(name); got != want {
			t.Fatalf("last-seen metadata for %q = %q, want %q", name, got, want)
		}
	}
	known := s.candidateAgentLookup(ctx, "box", []proto.Launcher{{Name: "gateway", Source: "zshrc"}}, true)
	if got := known("gateway"); got != "claude" {
		t.Fatalf("ordinary gateway agent = %q, want claude", got)
	}
	if got := known("absent"); got != "" {
		t.Fatalf("a known inventory's absent wrapper was guessed as %q", got)
	}
}

// applies_when.supervisor is normalized to its distinct classes, so an accepted policy's dual-name read always fits
// the write limit: a repeated class is stored once (this is what made an unbounded array break GET→PUT), an unknown
// class is still refused, and the policy round-trips.
func TestStartDefaultsSupervisorBounds(t *testing.T) {
	s, _ := defaultsHub(t)
	rule := func(sup string) string {
		return `{"start_defaults":{"rules":[{"applies_when":{"supervisor":` + sup + `},"then":{"effort":"max"}}]}}`
	}
	// A repeated class is accepted but stored once, not rejected.
	if code, got := putStartDefaults(t, s, roleFull, "", rule(`["main","main"]`)); code != http.StatusOK {
		t.Fatalf("a repeated supervisor class was not accepted and deduped: %d %s", code, got)
	}
	p := s.activeStartDefaults(context.Background())
	if p == nil || !slices.Equal(p.Rules[0].When.Supervisor, []string{"main"}) {
		t.Fatalf("the repeated class was not stored once: %+v", p.Rules[0].When.Supervisor)
	}
	// The unbounded reproduction (thousands of one class) is accepted, deduped to one entry, and round-trips.
	big := `["main"` + strings.Repeat(`,"main"`, 100) + `]`
	if code, got := putStartDefaults(t, s, roleFull, "", rule(big)); code != http.StatusOK {
		t.Fatalf("an unbounded repeated supervisor array was not accepted and deduped: %d %s", code, got[:120])
	}
	// Every distinct class once (with a duplicate thrown in) is stored deduped, and its dual-name read writes back.
	if code, got := putStartDefaults(t, s, roleFull, "", rule(`["main","main","background_supervisor","scheduled","other"]`)); code != http.StatusOK {
		t.Fatalf("distinct classes with a duplicate were refused: %d %s", code, got)
	}
	p = s.activeStartDefaults(context.Background())
	if p == nil || len(p.Rules[0].When.Supervisor) != len(dispatchClasses) {
		t.Fatalf("the classes were not deduped to the distinct set: %+v", p.Rules[0].When.Supervisor)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assistant/start-defaults", nil))
	if rec.Body.Len() >= startDefaultsBodyLimit {
		t.Fatalf("the deduped supervisor policy still exceeds the write limit: %d bytes", rec.Body.Len())
	}
	if code, got := putStartDefaults(t, s, roleFull, "", rec.Body.String()); code != http.StatusOK {
		t.Fatalf("the deduped supervisor policy did not round-trip: %d %s", code, got[:120])
	}
	// An unknown class is still refused.
	if code, got := putStartDefaults(t, s, roleFull, "", rule(`["owner"]`)); code != http.StatusBadRequest {
		t.Fatalf("an unknown supervisor class was accepted: %d %s", code, got)
	}
}
