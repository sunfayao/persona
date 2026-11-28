package agy

import (
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/transcript"
	"reflect"
	"testing"
	"time"
)

func TestAgyTranscriptTokenExtractionAndUsage(t *testing.T) {
	t.Parallel()
	// Realistic agy JSONL: exclusive input_tokens + cache_read_tokens, optional cache_write_tokens,
	// in-place step completion (step 1 RUNNING -> DONE), nested usage fallback, checkpoint compaction,
	// and a subsequent reply after compaction.
	const fixture = "" +
		`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-21T10:00:00Z","content":"Inspect files"}` + "\n" +
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"RUNNING","created_at":"2026-11-21T10:00:01Z","model":"gemini-4-neon-high","thinking":"Drafting","input_tokens":100,"output_tokens":50}` + "\n" +
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T10:00:01Z","model":"gemini-4-neon-high","thinking":"Reading file first","content":"I will read it.","input_tokens":32396,"cache_read_tokens":0,"output_tokens":862,"tool_calls":[{"name":"read_file","args":{"Path":"/tmp/a.go"}}]}` + "\n" +
		`{"step_index":2,"source":"MODEL","type":"GENERIC","status":"DONE","created_at":"2026-11-21T10:00:02Z","content":"package main"}` + "\n" +
		`{"step_index":3,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T10:00:03Z","model":"claude-opus-5-5-high","content":"All checked.","input_tokens":5727,"cache_read_tokens":32397,"cache_write_tokens":400,"cache_write_1h_tokens":100,"output_tokens":226}` + "\n" +
		`{"step_index":4,"source":"SYSTEM","type":"CHECKPOINT","status":"DONE","created_at":"2026-11-21T10:00:04Z","input_tokens":3201,"cache_read_tokens":509879,"output_tokens":4554}` + "\n" +
		`{"step_index":5,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-21T10:00:05Z","content":"Next question"}` + "\n" +
		`{"step_index":6,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T10:00:06Z","model":"gemini-4-neon-high","content":"Post-checkpoint answer.","usage":{"input_tokens":12000,"cache_read_input_tokens":3000,"cache_creation_input_tokens":500,"output_tokens":310,"model_context_window":1048576}}` + "\n"

	steps, _ := parseAgySteps([]byte(fixture), agyTestID)
	history := mapAgySteps(steps, time.Time{}, nil)

	// Step 1's RUNNING draft was replaced by DONE (output 862, not 50+862).
	// The checkpoint counts the summarizer request once.
	wantOutput := 862 + 226 + 4554 + 310
	wantContext := 12000 + 3000
	if u := history.usage; u.outputTokens != wantOutput || u.contextTokens != wantContext || u.compactions != 1 || u.turns != 2 {
		t.Fatalf("usage = %+v, want output %d and context %d", u, wantOutput, wantContext)
	}

	// Step 1 produced 3 items (thinking, text, tool_use): Usage must appear ONLY on the first item, Model on all 3.
	var step1Items []proto.Item
	var step3Item, step6Item proto.Item
	for _, it := range history.items {
		switch it.Seq {
		case 2:
			step1Items = append(step1Items, it)
		case 4:
			step3Item = it
		case 7:
			step6Item = it
		}
	}
	if len(step1Items) != 3 {
		t.Fatalf("step 1 items = %+v", step1Items)
	}
	wantStep1Usage := []proto.TokenUsage{{Model: "gemini-4-neon-high", Input: 32396, Output: 862}}
	if !reflect.DeepEqual(step1Items[0].Usage, wantStep1Usage) {
		t.Fatalf("step 1 first item usage = %+v, want %+v", step1Items[0].Usage, wantStep1Usage)
	}
	if step1Items[1].Usage != nil || step1Items[2].Usage != nil {
		t.Fatalf("step 1 secondary items must not duplicate usage: %+v, %+v", step1Items[1].Usage, step1Items[2].Usage)
	}
	for i, it := range step1Items {
		if it.Model != "gemini-4-neon-high" {
			t.Errorf("step 1 item %d model = %q, want gemini-4-neon-high", i, it.Model)
		}
	}

	wantStep3Usage := []proto.TokenUsage{{
		Model:        "claude-opus-5-5-high",
		Input:        5727,
		Output:       226,
		CacheRead:    32397,
		CacheWrite:   400,
		CacheWrite1h: 100,
	}}
	if !reflect.DeepEqual(step3Item.Usage, wantStep3Usage) || step3Item.Model != "claude-opus-5-5-high" {
		t.Fatalf("step 3 item = %+v, want usage %+v", step3Item, wantStep3Usage)
	}

	wantStep6Usage := []proto.TokenUsage{{
		Model:      "gemini-4-neon-high",
		Input:      12000,
		Output:     310,
		CacheRead:  3000,
		CacheWrite: 500,
	}}
	if !reflect.DeepEqual(step6Item.Usage, wantStep6Usage) {
		t.Fatalf("step 6 item usage = %+v, want %+v", step6Item.Usage, wantStep6Usage)
	}

	// FoldWork must aggregate a folded work run's usage into Fold.Usage without losing or duplicating tokens.
	const workOnly = "" +
		`{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-11-21T10:00:00Z","content":"Inspect"}` + "\n" +
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T10:00:01Z","model":"gemini-4-neon-high","thinking":"Reading","input_tokens":32396,"output_tokens":862,"tool_calls":[{"name":"read_file","args":{"Path":"/tmp/a.go"}}]}` + "\n" +
		`{"step_index":2,"source":"MODEL","type":"GENERIC","status":"DONE","created_at":"2026-11-21T10:00:02Z","content":"package main"}` + "\n" +
		`{"step_index":3,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-21T10:00:03Z","model":"gemini-4-neon-high","content":"Done."}` + "\n"
	workSteps, _ := parseAgySteps([]byte(workOnly), agyTestID)
	folded := transcript.FoldWork(mapAgySteps(workSteps, time.Time{}, nil).items)
	var foldUsage []proto.TokenUsage
	for _, it := range folded {
		if it.Kind == proto.KindFold && it.Fold != nil {
			foldUsage = append(foldUsage, it.Fold.Usage...)
		}
	}
	if !reflect.DeepEqual(foldUsage, wantStep1Usage) {
		t.Fatalf("folded work usage = %+v, want %+v", foldUsage, wantStep1Usage)
	}
}

func TestAgyTranscriptCheckpointResetsContextAndRewindExcludesAbandoned(t *testing.T) {
	t.Parallel()
	// Compaction at the tail leaves ContextTokens == 0 until the next reply.
	const compacted = "" +
		`{"step_index":0,"type":"USER_INPUT","created_at":"2026-11-21T10:00:00Z","content":"hi"}` + "\n" +
		`{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-11-21T10:00:01Z","model":"claude-opus-5-5-high","content":"done","input_tokens":50000,"cache_read_tokens":450000,"output_tokens":300}` + "\n" +
		`{"step_index":2,"type":"CHECKPOINT","created_at":"2026-11-21T10:00:02Z","input_tokens":3000,"cache_read_tokens":500000,"output_tokens":4000}` + "\n"
	steps, _ := parseAgySteps([]byte(compacted), agyTestID)
	u := mapAgySteps(steps, time.Time{}, nil).usage
	if u.contextTokens != 0 || u.outputTokens != 4300 || u.compactions != 1 {
		t.Fatalf("after checkpoint: %+v", u)
	}

	// Rewind drops abandoned step 2 and replaces step 1.
	const rewound = "" +
		`{"step_index":0,"type":"USER_INPUT","created_at":"2026-11-21T10:00:00Z","content":"hi"}` + "\n" +
		`{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-11-21T10:00:01Z","model":"gemini-4-neon-high","content":"old","input_tokens":40000,"output_tokens":900}` + "\n" +
		`{"step_index":2,"type":"PLANNER_RESPONSE","created_at":"2026-11-21T10:00:02Z","model":"gemini-4-neon-high","content":"abandoned","input_tokens":60000,"output_tokens":1200}` + "\n" +
		`{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-11-21T10:01:01Z","model":"gemini-4-neon-high","content":"replacement","input_tokens":15000,"cache_read_tokens":2000,"output_tokens":120}` + "\n"
	steps, _ = parseAgySteps([]byte(rewound), agyTestID)
	u = mapAgySteps(steps, time.Time{}, nil).usage
	if u.contextTokens != 17000 || u.outputTokens != 120 {
		t.Fatalf("after rewind: context=%d output=%d, want 17000 / 120", u.contextTokens, u.outputTokens)
	}

	// Missing, zero, negative, and synthetic-model fields degrade honestly.
	const missing = "" +
		`{"step_index":0,"type":"USER_INPUT","created_at":"2026-11-21T10:00:00Z","content":"hi"}` + "\n" +
		`{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-11-21T10:00:01Z","model":"<synthetic>","content":"plain"}` + "\n" +
		`{"step_index":2,"type":"PLANNER_RESPONSE","created_at":"2026-11-21T10:00:02Z","content":"neg","input_tokens":-10,"output_tokens":-5,"cache_read_tokens":-1}` + "\n"
	steps, _ = parseAgySteps([]byte(missing), agyTestID)
	h := mapAgySteps(steps, time.Time{}, nil)
	if h.usage.hasUsage || h.usage.contextTokens != 0 || h.usage.outputTokens != 0 || h.usage.model != "" {
		t.Fatalf("missing/negative usage should stay zero: %+v", h.usage)
	}
	for _, it := range h.items {
		if it.Usage != nil || it.Model != "" {
			t.Fatalf("unexpected usage or model on degraded item: %+v", it)
		}
	}
}

func TestAgyContextWindowAndPolicyParsing(t *testing.T) {
	t.Parallel()
	// Transcript with explicit model_context_window uses it as native window.
	const withWindow = "" +
		`{"step_index":0,"type":"USER_INPUT","created_at":"2026-11-28T10:00:00Z","content":"hi"}` + "\n" +
		`{"step_index":1,"type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-11-28T10:00:01Z","model":"gemini-4-neon-xhigh","content":"ok","usage":{"input_tokens":50000,"cache_read_input_tokens":150000,"output_tokens":200,"model_context_window":1048576}}` + "\n"
	steps, _ := parseAgySteps([]byte(withWindow), agyTestID)
	h := mapAgySteps(steps, time.Time{}, nil)
	if h.ContextWindow() != 1_048_576 {
		t.Fatalf("ContextWindow() = %d, want 1048576", h.ContextWindow())
	}
	var s proto.Session
	h.ApplyWithPolicy(&s, false, ContextPolicy{AutoCompactAt: 1_000_000})
	if s.ContextWindow != 1_048_576 || s.Facets.Context.Window != 1_048_576 || s.Facets.Context.AutoCompactAt != 1_000_000 || s.Facets.Context.Source != proto.ContextSourceReported {
		t.Fatalf("ApplyWithPolicy native = window:%d facets:%+v", s.ContextWindow, s.Facets.Context)
	}
	if bd := s.ContextBreakdown(); bd == nil || bd.AutoCompactThreshold == nil || *bd.AutoCompactThreshold != 1_000_000 {
		t.Fatalf("breakdown = %+v", bd)
	}

	// Without recorded window, Gemini 4 defaults to 1M and GPT-OSS 120B defaults to 131072.
	if got := WindowForModel("gemini-4-carbon-high", 0); got != 1_000_000 {
		t.Fatalf("WindowForModel(gemini-4-carbon-high) = %d, want 1000000", got)
	}
	if got := WindowForModel("gpt-oss-120b-medium", 0); got != 131_072 {
		t.Fatalf("WindowForModel(gpt-oss-120b-medium) = %d, want 131072", got)
	}

	for _, tc := range []struct {
		raw  string
		max  int
		want int
	}{
		{"1000000", 1_048_576, 1_000_000},
		{"1M", 1_048_576, 1_000_000},
		{"900K", 1_048_576, 900_000},
		{"0.5M", 1_048_576, 500_000},
		{"80%", 1_000_000, 800_000},
		{"2M", 1_048_576, 0},
		{"101%", 1_000_000, 0},
		{"bad", 1_000_000, 0},
	} {
		if got := ParseAutoCompactOverride(tc.raw, tc.max); got != tc.want {
			t.Errorf("ParseAutoCompactOverride(%q, %d) = %d, want %d", tc.raw, tc.max, got, tc.want)
		}
	}
	if got := ParseContextWindowPreset("900K"); got != 900_000 {
		t.Errorf("ParseContextWindowPreset(900K) = %d, want 900000", got)
	}
	if got := ParseContextWindowPreset("80%"); got != 0 {
		t.Errorf("ParseContextWindowPreset(80%%) = %d, want 0", got)
	}
}
