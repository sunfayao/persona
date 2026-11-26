package codex

import (
	"regexp"
	"testing"
)

// rolloutID reads the same id as rolloutIDRe, for any name.
func TestRolloutID(t *testing.T) {
	t.Parallel()
	rolloutIDRe := regexp.MustCompile(`([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})(?:_[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})*\.jsonl$`)
	for _, p := range []string{
		"/h/.codex/sessions/2026/01/02/rollout-2026-01-02T03-04-05-0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b.jsonl",
		"/h/.codex/archived_sessions/rollout-2026-01-02T03-04-05-0199A1B2-C3D4-7E5F-8A9B-0C1D2E3F4A5B.jsonl",
		`C:\Users\u\.codex\sessions\2026\01\02\rollout-2026-01-02T03-04-05-0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b.jsonl`,
		"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b.jsonl",
		"x0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b.jsonl",
		"rollout-time-0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b_11111111-2222-3333-4444-555555555555.jsonl",
		"rollout-time-0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b_11111111-2222-3333-4444-555555555555_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl",
		"/h/rollout-2026-01-02T03-04-05-0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b.jsonl.tmp",
		"/h/rollout-2026-01-02T03-04-05-0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5.jsonl",
		"/h/rollout-2026-01-02T03-04-05-0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5g.jsonl",
		"/h/rollout-2026-01-02T03-04-05-0199a1b2_c3d4-7e5f-8a9b-0c1d2e3f4a5b.jsonl",
		"/h/rollout-2026-01-02T03-04-05-0199a1b2-c3d4-7e5f-8a9b0-c1d2e3f4a5b.jsonl",
		"/h/rollout-2026-01-02T03-04-05-plain.jsonl",
		".jsonl",
		"",
	} {
		want := ""
		if m := rolloutIDRe.FindStringSubmatch(p); m != nil {
			want = m[1]
		}
		if got := RolloutID(p); got != want {
			t.Errorf("rolloutID(%q) = %q, want %q", p, got, want)
		}
	}
}
