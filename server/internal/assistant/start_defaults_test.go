package assistant

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// stubStartDefaults makes s ask fn for every task start's defaults; seen are the facts each start gave, in order.
func stubStartDefaults(s *Service, fn func(StartFacts) (StartDefaults, bool)) (seen func() []StartFacts) {
	var mu sync.Mutex
	var facts []StartFacts
	s.startDefaultsFor = func(_ context.Context, f StartFacts) (StartDefaults, bool, error) {
		mu.Lock()
		facts = append(facts, f)
		mu.Unlock()
		d, ok := fn(f)
		return d, ok, nil
	}
	return func() []StartFacts {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(facts)
	}
}

// saveTiers makes a model profile with these tiers in use.
func saveTiers(t *testing.T, s *Service, tiers map[string]ModelTier) {
	t.Helper()
	ctx := context.Background()
	ps := modelProfiles{Revision: 1, Profiles: []ModelProfile{{ID: "p", Name: "P", Tiers: tiers}}}
	if err := s.store().PutSetting(ctx, modelProfilesKey, ps); err != nil {
		t.Fatal(err)
	}
	if err := s.store().PutSetting(ctx, modelProfileStateKey, modelProfileState{Active: "p"}); err != nil {
		t.Fatal(err)
	}
}

// fullDefaults asks for ultracode, effort max, and these presets in order.
func fullDefaults(presets ...string) StartDefaults {
	on := true
	d := StartDefaults{Ultracode: &on, Effort: "max", By: "Example"}
	for _, p := range presets {
		d.Presets = append(d.Presets, PresetChoice{Name: p})
	}
	return d
}

// defaultsTask is a writable task on box that names nothing it runs on, with two criteria.
func defaultsTask() PlanTask {
	return PlanTask{Title: "Fix the build", Prompt: "fix the build", Machine: "box", Cwd: "/Users/me/code/app",
		Permission: "bypassPermissions", DoneWhen: []string{"CI is green", "the fix is merged"}}
}

// defaultsHub is launcherFitHub (box has the launcher gateway, l4 has other; Model Settings › Agents runs gateway's
// gw-model at high) with three presets in use.
func defaultsHub(t *testing.T) (*Service, *fakeHub) {
	t.Helper()
	s, f := launcherFitHub(t)
	saveTiers(t, s, map[string]ModelTier{
		"deep":  {Launcher: "gateway", Model: "gw-deep", Effort: "high"},
		"quick": {Launcher: "gateway", Model: "gw-quick"},
		"plain": {Launcher: "claude", Model: "claude-plain"},
	})
	return s, f
}

// The defaults fill in what the task says nothing about: the first usable preset (its effort overridden), ultracode,
// and effort max. The card's task records what ran and why.
func TestStartDefaultsFillWhatTheTaskLeavesOpen(t *testing.T) {
	s, f := defaultsHub(t)
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return fullDefaults("deep", "quick"), true })
	task := defaultsTask()
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	st := startsOf(f)[0]
	if st["launcher"] != "gateway" || st["model"] != "gw-deep" || st["effort"] != "max" || st["ultracode"] != true {
		t.Fatalf("start = %v", st)
	}
	if st["goal"] != nil {
		t.Fatalf("unexpected automatic goal: %v", st["goal"])
	}
	if !strings.Contains(st["prompt"].(string), "fix the build") {
		t.Fatalf("the prompt went somewhere else: %v", st["prompt"])
	}
	if task.Ultracode == nil || !*task.Ultracode || task.Effort != "max" || task.Model != "gw-deep" || !strings.Contains(task.Why, "Example") {
		t.Fatalf("task = %+v", task)
	}
}

// Whatever the caller named itself wins: an effort, ultracode off, a tier, a model or a launcher.
func TestStartDefaultsYieldToTheCaller(t *testing.T) {
	off := false
	for _, c := range []struct {
		name string
		edit func(*PlanTask)
		want map[string]any // values the start must carry; nil: the key must be unset
	}{
		{"effort", func(p *PlanTask) { p.Effort = "low" },
			map[string]any{"effort": "low", "model": "gw-deep", "ultracode": true}},
		{"tier without one keeps the source's effort", func(p *PlanTask) { p.Tier = "quick" },
			map[string]any{"effort": "max", "model": "gw-quick", "ultracode": true}},
		{"ultracode off", func(p *PlanTask) { p.Ultracode = &off },
			map[string]any{"effort": "max", "model": "gw-deep", "ultracode": nil}},
		{"tier with an effort", func(p *PlanTask) { p.Tier = "deep" },
			map[string]any{"effort": "high", "model": "gw-deep", "ultracode": true}},

		{"model", func(p *PlanTask) { p.Model = "gw-own" },
			map[string]any{"launcher": "gateway", "effort": "max", "model": "gw-own", "ultracode": true}},
		{"launcher", func(p *PlanTask) { p.Launcher = "claude" },
			map[string]any{"launcher": "claude", "effort": "max", "model": nil, "ultracode": true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f := defaultsHub(t)
			stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return fullDefaults("deep"), true })
			task := defaultsTask()
			c.edit(&task)
			if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
				t.Fatal(err)
			}
			st := startsOf(f)[0]
			for k, v := range c.want {
				if v == nil && !unset(st[k]) && st[k] != false || v != nil && st[k] != v {
					t.Fatalf("%s = %v, want %v (start %v)", k, st[k], v, st)
				}
			}
		})
	}
}

// No defaults, the source saying none or none at all: the start is exactly what it was before defaults existed.
func TestStartDefaultsNoneLeaveTheStartAsItWas(t *testing.T) {
	run := func(stub bool) map[string]any {
		s, f := defaultsHub(t)
		if stub {
			stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return StartDefaults{}, false })
		}
		task := defaultsTask()
		if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
			t.Fatal(err)
		}
		st := maps.Clone(startsOf(f)[0])
		delete(st, "session_id") // a new id each start
		return st
	}
	before, after := run(false), run(true)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("without defaults:\n%v\nwith none asked:\n%v", before, after)
	}
	for _, k := range []string{"goal", "ultracode"} {
		if _, ok := after[k]; ok {
			t.Fatalf("%s sent without defaults: %v", k, after)
		}
	}
}

// A preset that cannot run on the task's computer now is passed over for the next: not in the profile, a launcher the
// computer lacks, a launcher at its usage limit there.
func TestStartDefaultsPassOverPresetsThatCannotRun(t *testing.T) {
	s, f := defaultsHub(t)
	ctx := context.Background()
	saveTiers(t, s, map[string]ModelTier{
		"elsewhere": {Launcher: "other", Model: "o-1"}, // l4's, not box's
		"limited":   {Launcher: "gateway", Model: "gw-deep"},
		"plain":     {Launcher: "claude", Model: "claude-plain"},
	})
	resets := time.Now().Add(2 * time.Hour)
	s.recordHealth(ctx, "box", "gateway", healthResult{err: "You've hit your usage limit", source: healthSourceSession,
		kind: errKindUsageLimit, resetsAt: &resets})
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		return fullDefaults("gone", "elsewhere", "limited", "plain"), true
	})
	task := defaultsTask()
	task.MachinePolicy = machineFlexible // a fixed task would fail on the limited launcher instead of passing over
	if _, _, err := s.startTaskAgent(ctx, &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["launcher"] != "claude" || st["model"] != "claude-plain" {
		t.Fatalf("start = %v", st)
	}
	for _, name := range []string{"gone", "elsewhere", "limited"} {
		if !strings.Contains(task.Why, name) {
			t.Fatalf("why does not say %s was passed over: %q", name, task.Why)
		}
	}
	// None usable: the task starts as it would have with no preset, and says why.
	s, f = defaultsHub(t)
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return fullDefaults("gone"), true })
	task = defaultsTask()
	if _, _, err := s.startTaskAgent(ctx, &task, false); err != nil {
		t.Fatal(err)
	}
	st := startsOf(f)[0]
	if st["launcher"] != "gateway" || st["model"] != "gw-model" {
		t.Fatalf("no usable preset: %v", st)
	}
	if st["effort"] != "max" { // no caller effort: the source's effort still wins over Model Settings
		t.Fatalf("effort = %v, want the source's max", st["effort"])
	}
}

// Weights share starts among the usable presets in proportion; without weights the first usable one always wins.
func TestStartDefaultsWeights(t *testing.T) {
	ctx := context.Background()
	pick := func(roll int, choices ...PresetChoice) any {
		s, f := defaultsHub(t)
		old := presetIntN
		presetIntN = func(n int) int {
			if n != 4 {
				t.Fatalf("rolled over %d, want the weights' sum 4", n)
			}
			return roll
		}
		t.Cleanup(func() { presetIntN = old })
		d := fullDefaults()
		d.Presets = choices
		stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return d, true })
		task := defaultsTask()
		if _, _, err := s.startTaskAgent(ctx, &task, false); err != nil {
			t.Fatal(err)
		}
		return startsOf(f)[0]["model"]
	}
	weighted := []PresetChoice{{Name: "deep", Weight: 1}, {Name: "gone", Weight: 5}, {Name: "quick", Weight: 3}}
	if got := pick(0, weighted...); got != "gw-deep" {
		t.Fatalf("roll 0: %v", got)
	}
	if got := pick(1, weighted...); got != "gw-quick" {
		t.Fatalf("roll 1: %v", got)
	}
	if got := pick(3, weighted...); got != "gw-quick" {
		t.Fatalf("roll 3: %v", got)
	}
}

// Ultracode and effort max are defaults for Claude Code.
func TestStartDefaultsOnlyWhatTheAgentTakes(t *testing.T) {
	s, f := defaultsHub(t)
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return fullDefaults(), true })
	task := defaultsTask()
	task.DoneWhen = nil
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["goal"] != nil || st["ultracode"] != true {
		t.Fatalf("no criteria: %v", st)
	}

	s, f = defaultsHub(t)
	f.zshrc["box"] = append(f.zshrc["box"], proto.Launcher{Name: "cx", Source: "zshrc", Agent: "codex"})
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return fullDefaults(), true })
	task = defaultsTask()
	task.Launcher = "cx"
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["goal"] != nil || st["ultracode"] != nil || st["effort"] == "max" {
		t.Fatalf("codex: %v", st)
	}
	if st := startsOf(f)[0]; st["launcher"] != "cx" {
		t.Fatalf("the codex launcher was changed: %v", st)
	}

	// Codex without criteria: no goal either.
	s, f = defaultsHub(t)
	f.zshrc["box"] = append(f.zshrc["box"], proto.Launcher{Name: "cx", Source: "zshrc", Agent: "codex"})
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return fullDefaults(), true })
	task = defaultsTask()
	task.Launcher, task.DoneWhen = "cx", nil
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["goal"] != nil {
		t.Fatalf("codex without criteria: %v", st)
	}
}

// A source can ask for the self-planning constraint in the prompt: for launches whose model does not act on effort.
func TestStartDefaultsPlanFirstGoesInThePrompt(t *testing.T) {
	s, f := defaultsHub(t)
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		d := fullDefaults()
		d.PlanFirst = true
		return d, true
	})
	task := defaultsTask()
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	if prompt, _ := startsOf(f)[0]["prompt"].(string); !strings.Contains(prompt, prompts.RenderPlanFirstTaskConstraint()) {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestStartDefaultsUltracodeRefusedStartsWithoutIt(t *testing.T) {
	s, f := defaultsHub(t)
	f.refuseUltra = &rpc.Error{Code: "ultracode_unsupported", Message: "no ultracode here"}
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return fullDefaults(), true })
	task := defaultsTask()
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	starts := startsOf(f)
	if len(starts) != 2 || starts[0]["ultracode"] != true || starts[1]["ultracode"] != nil ||
		starts[0]["goal"] != nil || starts[1]["goal"] != nil {
		t.Fatalf("starts = %v", starts)
	}
	if !strings.Contains(task.Why, "no ultracode here") {
		t.Fatalf("why = %q", task.Why)
	}
}

// putStartDefaults sends PUT /api/v1/assistant as role (full, extension, read-only) and answers the code
// and body.
func putStartDefaults(t *testing.T, s *Service, role, ext, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/assistant", strings.NewReader(body))
	if role != "" {
		req.Header.Set(RoleHeader, role)
	}
	if ext != "" {
		req.Header.Set(ExtensionHeader, ext)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// A task that names its own model and asks ultracode off keeps both when a later try finds no matching policy: the
// policy's fill-in never replaces what the task named.
func TestStartDefaultsDroppedKeepsTheCallersModelAndUltracode(t *testing.T) {
	s, f := defaultsHub(t)
	off := false
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		return StartDefaults{Effort: "max", Presets: []PresetChoice{{Name: "plain"}}}, true
	})
	task := defaultsTask()
	task.Model = "gw-own"
	task.Ultracode = &off
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	if !task.startFilled.any() {
		t.Fatal("the first try did not take the policy")
	}
	// The policy may not override the caller's model or its explicit ultracode off.
	st := startsOf(f)[0]
	if st["model"] != "gw-own" || st["ultracode"] != nil {
		t.Fatalf("the policy overrode the caller: %v", st)
	}
	// No rule applies on the second try: the model and ultracode off are still the caller's, the preset is gone.
	s.startDefaultsFor = nil
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	st = startsOf(f)[1]
	if st["model"] != "gw-own" {
		t.Fatalf("the caller's model was wiped: %v", st)
	}
	// A task that names a model gets no preset, so its launcher is Model Settings' on both tries; without the policy
	// the effort is Model Settings' again, not the policy's max.
	if st["ultracode"] != nil || st["launcher"] != "gateway" || st["effort"] == "max" {
		t.Fatalf("the retry kept policy values: %v", st)
	}
	// The explicit off is still the task's: a later policy asking ultracode on must not turn it on.
	if task.Ultracode == nil || *task.Ultracode {
		t.Fatalf("the caller's explicit ultracode off was lost: %v", task.Ultracode)
	}
	on := true
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return StartDefaults{Ultracode: &on}, true })
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[2]; st["ultracode"] != nil {
		t.Fatalf("a policy overrode the caller's explicit ultracode off: %v", st)
	}
}

// A preset the policy picks starts exactly as if the task had named that tier: the same launcher, model, launch
// fingerprint and permission, here for a preset whose launcher is not Model Settings' default.
func TestStartDefaultsPresetStartsAsIfTheTaskNamedIt(t *testing.T) {
	start := func(policy bool) map[string]any {
		t.Helper()
		s, f := defaultsHub(t)
		task := defaultsTask()
		if policy {
			stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
				return StartDefaults{Presets: []PresetChoice{{Name: "plain"}}}, true
			})
		} else {
			task.Tier = "plain"
		}
		if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
			t.Fatal(err)
		}
		if task.Tier != "plain" {
			t.Fatalf("the task does not name the preset it runs: %+v", task)
		}
		st := maps.Clone(startsOf(f)[0])
		delete(st, "session_id")
		return st
	}
	picked, named := start(true), start(false)
	if !reflect.DeepEqual(picked, named) {
		t.Fatalf("picked by the policy:\n%v\nnamed by the task:\n%v", picked, named)
	}
	if picked["launcher"] != "claude" || picked["model"] != "claude-plain" {
		t.Fatalf("not the preset's launcher and model: %v", picked)
	}
}

// A rule whose then does nothing is a mistake: it would shadow every later rule, so the write is refused.
func TestStartDefaultsRouteRejectsEmptyThen(t *testing.T) {
	s, _ := defaultsHub(t)
	if code, body := putStartDefaults(t, s, roleFull, "",
		`{"start_defaults":{"rules":[{"when":{},"then":{}}]}}`); code != http.StatusBadRequest {
		t.Fatalf("empty then: %d %s", code, body)
	}
	// The settings route accepts a valid policy and clears it when the rules are empty.
	rec := httptest.NewRecorder()
	body := `{"start_defaults":{"rules":[{"when":{},"then":{"effort":"max"}}]}}`
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/assistant", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT settings: %d %s", rec.Code, rec.Body.String())
	}
	if got := s.settings(context.Background()).StartDefaults; got == nil || got.By != "owner" {
		t.Fatalf("the settings write was not applied: %+v", got)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/assistant", strings.NewReader(`{"start_defaults":{"rules":[]}}`)))
	if rec.Code != http.StatusOK || s.settings(context.Background()).StartDefaults != nil {
		t.Fatalf("empty rules did not clear the policy: %d %s", rec.Code, rec.Body.String())
	}
}

// The policy is visible only with its own scope: an assistant.ask extension reads GET /api/v1/assistant without it.
func TestStartDefaultsHiddenFromTheGeneralSettingsRoute(t *testing.T) {
	s, f := defaultsHub(t)
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		return StartDefaults{Effort: "max"}, true
	})
	expires := time.Now().Add(time.Minute)
	setSettings(t, s, func(v *Settings) {
		v.StartDefaults = &StartDefaultsPolicy{ExpiresAt: &expires, Rules: []StartDefaultsRule{{Then: StartDefaultsThen{Effort: "max"}}}}
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/assistant", nil)
	req.Header.Set(RoleHeader, roleExtension)
	req.Header.Set(ExtensionHeader, "kit")
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "start_defaults") {
		t.Fatalf("an extension read the policy through assistant.ask: %d %s", rec.Code, rec.Body.String())
	}
	_ = f
}

// A try without a matching policy must not keep an earlier try's fill-in: the start goes back to what the task named,
// and the plan-first prompt and goal are gone.
func TestStartDefaultsDroppedWhenThePolicyNoLongerApplies(t *testing.T) {
	s, f := defaultsHub(t)
	on := func() bool { return true }
	off := func() bool { return false }
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		if on() {
			return StartDefaults{Effort: "max", Presets: []PresetChoice{{Name: "deep"}}, PlanFirst: true}, true
		}
		return StartDefaults{}, false
	})
	task := defaultsTask()
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	if !task.startFilled.any() || task.planFirst != true || task.Tier != "deep" || task.Effort != "max" {
		t.Fatalf("the first try did not take the defaults: %+v", task)
	}
	on = off
	// A second try of the same task: no policy matches now, so everything it filled in is gone.
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	if task.startFilled.any() && (task.planFirst || task.Tier == "deep" || task.Effort == "max") {
		t.Fatalf("the second try kept the dropped policy: %+v", task)
	}
	st := startsOf(f)[1]
	if st["goal"] != nil || st["ultracode"] != nil || st["effort"] == "max" || st["model"] == "gw-deep" {
		t.Fatalf("the second start kept policy params: %v", st)
	}
	if st["launcher"] != "gateway" || st["model"] != "gw-model" { // Model Settings' default still applies
		t.Fatalf("the ordinary default launch changed: %v", st)
	}
	_ = off
}

// A hidden launcher still starts a preset: availability agrees with GET worker-presets, which offers hidden-but-usable
// launchers.
func TestStartDefaultsPresetOnAHiddenLauncher(t *testing.T) {
	s, f := defaultsHub(t)
	f.zshrc["box"] = append(f.zshrc["box"], proto.Launcher{Name: "secret", Source: "zshrc"})
	if err := s.putSetting(context.Background(), launchersHiddenKey, []string{"secret"}); err != nil {
		t.Fatal(err)
	}
	saveTiers(t, s, map[string]ModelTier{"h": {Launcher: "secret", Model: "m-1"}})
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		return StartDefaults{Presets: []PresetChoice{{Name: "h"}}}, true
	})
	task := defaultsTask()
	if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[0]; st["launcher"] != "secret" {
		t.Fatalf("the hidden preset was passed over: %v", st)
	}
}

// A retry that a different rule fits starts from what the task named again: the earlier rule's ultracode and effort are
// gone even when the task named its own launcher, model or tier (no preset is chosen then), and only the rule that
// fits now adds anything.
func TestStartDefaultsRetryUnderAChangedRuleKeepsNothingOfTheOldRule(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(*PlanTask)
	}{
		{"names nothing", func(*PlanTask) {}},
		{"names its model", func(p *PlanTask) { p.Model = "gw-own" }},
		{"names its tier", func(p *PlanTask) { p.Tier = "quick" }},
		{"names its launcher", func(p *PlanTask) { p.Launcher = "gateway" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f := defaultsHub(t)
			first := true
			stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
				if first {
					return fullDefaults("deep"), true // ultracode on, effort max, a goal, preset deep
				}
				return StartDefaults{PlanFirst: true}, true // a different rule: only the self-planning prompt
			})
			task := defaultsTask()
			c.edit(&task)
			if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
				t.Fatal(err)
			}
			if st := startsOf(f)[0]; st["ultracode"] != true || st["effort"] != "max" {
				t.Fatalf("the first rule did not apply: %v", st)
			}
			first = false
			if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
				t.Fatal(err)
			}
			st := startsOf(f)[1]
			if st["ultracode"] != nil || st["effort"] == "max" || st["goal"] != nil || st["model"] == "gw-deep" {
				t.Fatalf("the retry kept the old rule's values: %v", st)
			}
			if !strings.Contains(st["prompt"].(string), prompts.RenderPlanFirstTaskConstraint()) {
				t.Fatalf("the rule that fits now did not apply: %v", st["prompt"])
			}
			if task.Ultracode != nil || task.Effort == "max" {
				t.Fatalf("the task kept the old rule's values: ultracode %v effort %q", task.Ultracode, task.Effort)
			}
		})
	}
}

// A policy's values are for the start they were applied to: the plan keeps only what the task named, so a start that
// reads the task back from the store (a remedy, a queued start, a retry from the card, a manager restart) does not take
// an expired policy's ultracode, effort, model or preset for the task's own, and the card does not show them as asked.
func TestStartDefaultsNotStoredWithTheTask(t *testing.T) {
	s, f := defaultsHub(t)
	ctx := context.Background()
	policy := true
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return fullDefaults("deep"), policy })
	res, err := s.toolTaskPlan(ctx, "conv9", json.RawMessage(`{"tasks":[{"title":"Fix the build","prompt":"fix the build",
		"machine":"box","cwd":"/Users/me/code/app","why":"the repo is on box","permission":"bypassPermissions",
		"done_when":["CI is green","the fix is merged"],"expect_minutes":30}]}`))
	if err != nil {
		t.Fatal(err)
	}
	id := planID(t, res)
	if _, err := s.startPlan(ctx, id, nil, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the start", func() bool { return len(startsOf(f)) == 1 })
	if st := startsOf(f)[0]; st["ultracode"] != true || st["effort"] != "max" || st["model"] != "gw-deep" {
		t.Fatalf("the policy did not apply: %v", st)
	}
	todoID := todoIDOfPlan(t, s, id)
	waitFor(t, "the agent linked", func() bool { return len(todoNow(s, todoID).Agents) == 1 })
	waitFor(t, "the start to finish its bookkeeping", func() bool { return s.taskStarting.Load() == 0 })
	p, err := s.loadPlan(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	stored := p.Tasks[0]
	if stored.Ultracode != nil || stored.Effort == "max" || stored.Model == "gw-deep" || stored.Tier == "deep" {
		t.Fatalf("the policy's values were stored with the task: ultracode %v effort %q model %q tier %q",
			stored.Ultracode, stored.Effort, stored.Model, stored.Tier)
	}
	if !strings.Contains(stored.Why, "Example") {
		t.Fatalf("the task's why lost the policy's note: %q", stored.Why)
	}
	// The policy has expired; the task read back from the store starts as it would without the feature.
	policy = false
	if _, _, err := s.startTaskAgent(ctx, &stored, true); err != nil {
		t.Fatal(err)
	}
	st := startsOf(f)[1]
	if st["ultracode"] != nil || st["effort"] == "max" || st["model"] == "gw-deep" || st["goal"] != nil {
		t.Fatalf("an expired policy still decided the start: %v", st)
	}
	if st["launcher"] != "gateway" || st["model"] != "gw-model" || st["effort"] != "high" {
		t.Fatalf("the ordinary default launch changed: %v", st)
	}
}

// What the task named itself stays stored as it was: a task's own ultracode off and effort survive a policy-filled
// start in the plan, and a task's own ultracode on stays on.
func TestStartDefaultsStoredTaskKeepsItsOwnValues(t *testing.T) {
	for _, c := range []struct {
		name      string
		ultracode *bool
		effort    string
	}{
		{"its own ultracode off and effort", func() *bool { b := false; return &b }(), "low"},
		{"its own ultracode on", func() *bool { b := true; return &b }(), ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, f := defaultsHub(t)
			stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return fullDefaults("deep"), true })
			task := defaultsTask()
			task.Ultracode, task.Effort = c.ultracode, c.effort
			if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
				t.Fatal(err)
			}
			if len(startsOf(f)) != 1 {
				t.Fatalf("starts: %v", startsOf(f))
			}
			kept := task.storedLaunch()
			if (kept.ultracode == nil) != (c.ultracode == nil) || kept.ultracode != nil && *kept.ultracode != *c.ultracode ||
				kept.effort != c.effort || kept.tier != "" || kept.model != "" || kept.launcher != "" {
				t.Fatalf("stored launch = %+v, want the task's own (ultracode %v, effort %q)", kept, c.ultracode, c.effort)
			}
		})
	}
}

// A policy's preset can change which agent runs, and with it the permission mode its launcher sets (Claude Code runs
// with bypassPermissions): through a real plan, the stored task keeps its own mode, and the task read back from the
// store after the policy expired runs its own agent with it, not with the mode the policy's agent set.
func TestStartDefaultsPermissionOfThePolicysAgentNotStored(t *testing.T) {
	s, f := defaultsHub(t)
	ctx := context.Background()
	f.zshrc["box"] = append(f.zshrc["box"], proto.Launcher{Name: "cx", Source: "zshrc", Agent: "codex"})
	setModel(t, s, "agent.new", ModelBinding{Provider: "launcher.cx"}) // the task's own default runs Codex
	policy := true
	stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) {
		return StartDefaults{Presets: []PresetChoice{{Name: "plain"}}, By: "Example"}, policy // a Claude Code preset
	})
	res, err := s.toolTaskPlan(ctx, "conv9", json.RawMessage(`{"tasks":[{"title":"Fix the build","prompt":"fix the build",
		"machine":"box","cwd":"/Users/me/code/app","why":"the repo is on box","permission":"acceptEdits",
		"done_when":["CI is green"],"expect_minutes":30}]}`))
	if err != nil {
		t.Fatal(err)
	}
	id := planID(t, res)
	if p, err := s.loadPlan(ctx, id); err != nil || p.Tasks[0].Permission != "acceptEdits" {
		t.Fatalf("the plan does not keep the task's mode: %+v %v", p, err)
	}
	if _, err := s.startPlan(ctx, id, nil, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the start", func() bool { return len(startsOf(f)) == 1 })
	if st := startsOf(f)[0]; st["launcher"] != "claude" || st["permission_mode"] != "bypassPermissions" {
		t.Fatalf("the policy's Claude Code preset did not run: %v", st)
	}
	todoID := todoIDOfPlan(t, s, id)
	waitFor(t, "the agent linked", func() bool { return len(todoNow(s, todoID).Agents) == 1 })
	waitFor(t, "the start to finish its bookkeeping", func() bool { return s.taskStarting.Load() == 0 })
	p, err := s.loadPlan(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	stored := p.Tasks[0]
	if !strings.Contains(stored.Why, "Example") {
		t.Fatalf("the start did not write the task back (no policy note in its why): %q", stored.Why)
	}
	if stored.Permission != "acceptEdits" || stored.Launcher == "claude" || stored.Tier == "plain" || stored.Model == "claude-plain" {
		t.Fatalf("the policy's agent was stored with the task: permission %q launcher %q tier %q model %q",
			stored.Permission, stored.Launcher, stored.Tier, stored.Model)
	}
	policy = false
	if _, _, err := s.startTaskAgent(ctx, &stored, true); err != nil {
		t.Fatal(err)
	}
	if st := startsOf(f)[1]; st["launcher"] != "cx" || st["permission_mode"] != "acceptEdits" {
		t.Fatalf("without the policy the task did not start as its own: %v", st)
	}
}

// A rule with no usable settings leaves the start as it is without
// the feature, and the launch the plan keeps is the same as without the feature: the task's own, which names nothing,
// not the launcher and model Model configuration › Agents resolved for this try (#12618).
func TestStartDefaultsARuleThatFillsNothingStoresAsWithout(t *testing.T) {
	stored := func(rule bool) startPick {
		t.Helper()
		s, _ := defaultsHub(t)
		if rule {
			stubStartDefaults(s, func(StartFacts) (StartDefaults, bool) { return StartDefaults{}, true })
		}
		task := defaultsTask()
		task.DoneWhen = nil
		if _, _, err := s.startTaskAgent(context.Background(), &task, false); err != nil {
			t.Fatal(err)
		}
		if task.startFilled.any() {
			t.Fatalf("the rule filled something in: %+v", task.startFilled)
		}
		return task.storedLaunch()
	}
	with, without := stored(true), stored(false)
	if !reflect.DeepEqual(with, without) || with.launcher != "" || with.model != "" || with.effort != "" {
		t.Fatalf("with a rule that fills nothing: %+v\nwithout the feature: %+v", with, without)
	}
}

func TestTaskStartsDoNotEnableGoals(t *testing.T) {
	for _, launcher := range []string{"cx", "claude", "codex-app"} {
		for _, policy := range []string{"", "done_when", "off"} {
			t.Run(launcher+"/"+policy, func(t *testing.T) {
				s, f := defaultsHub(t)
				f.zshrc["box"] = append(f.zshrc["box"], proto.Launcher{Name: "cx", Source: "zshrc", Agent: "codex"},
					proto.Launcher{Name: "codex-app", Source: "codex_app", Agent: "codex"})
				if policy != "" {
					body := `{"start_defaults":{"rules":[{"when":{},"then":{"goal":"` + policy + `"}}]}}`
					if code, res := putStartDefaults(t, s, roleFull, "", body); code != http.StatusOK {
						t.Fatalf("legacy policy did not round-trip: %d %s", code, res)
					}
				}
				task := defaultsTask()
				task.Launcher = launcher
				if _, _, err := s.startTaskAgent(t.Context(), &task, false); err != nil {
					t.Fatal(err)
				}
				starts := startsOf(f)
				if len(starts) != 1 || starts[0]["launcher"] != launcher || starts[0]["goal"] != nil ||
					!strings.Contains(starts[0]["prompt"].(string), task.Prompt) {
					t.Fatalf("expected a single ordinary task prompt: %v", starts)
				}
			})
		}
	}
}

func TestRetiredGoalRuleLetsFollowingDefaultsApply(t *testing.T) {
	p := &StartDefaultsPolicy{Rules: []StartDefaultsRule{
		{Then: StartDefaultsThen{Goal: GoalFromDoneWhen}},
		{Then: StartDefaultsThen{Effort: "max"}},
	}}
	if d, ok := matchStartDefaults(p, StartFacts{Role: "worker"}); !ok || d.Effort != "max" {
		t.Fatalf("goal-only rule hid usable defaults: %+v %v", d, ok)
	}
}
