package assistant

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// sessionLimitLine is Claude Code's line for a session limit that resets in about in (to the minute, in UTC), and that reset.
func sessionLimitLine(in time.Duration) (string, time.Time) {
	reset := time.Now().UTC().Add(in).Truncate(time.Minute)
	return fmt.Sprintf("You've hit your session limit · resets %s (UTC)", reset.Format("3:04pm")), reset
}

// editHealth changes launcher's record on machine in place, as time passing would.
func editHealth(s *Service, machine, launcher string, edit func(*LauncherHealth)) {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()
	edit(s.health.recs[healthKey(machine, launcher)])
}

func checkedCount(f *fakeHub, key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, k := range f.checked {
		if k == key {
			n++
		}
	}
	return n
}

// A subscription's usage limit is its own class whatever comes before its words; a gateway's 429, rate limit or quota
// keeps the class it had.
func TestLauncherHealthClassifiesUsageLimits(t *testing.T) {
	for text, want := range map[string]string{
		"You've hit your session limit · resets 4:20pm (Asia/Shanghai)":                              errKindUsageLimit,
		"Error: You've hit your weekly limit · resets Sep 28, 4pm (America/New_York)":                errKindUsageLimit,
		"You've hit your limit · resets 9am (UTC)":                                                   errKindUsageLimit,
		"Claude AI usage limit reached|1790000000":                                                   errKindUsageLimit,
		"ERROR: You've hit your usage limit. Upgrade to Pro (<url>) or try again at 3:05 PM.":        errKindUsageLimit,
		`stream error: {"codex_error_info":"usage_limit_exceeded"}`:                                  errKindUsageLimit,
		`turn/completed: {"codexErrorInfo":"usageLimitExceeded"}`:                                    errKindUsageLimit,
		"API Error: Request rejected (429) · quota reached":                                          errKindOther,
		`API Error: 429 {"type":"error","error":{"type":"rate_limit_error","message":"rate limit"}}`: errKindOther,
		`API Error: 403 {"error":"monthly quota exceeded"}`:                                          errKindAuth,
		"You've hit your stride, nice work":                                                          errKindOther,
	} {
		if got := classifyLauncherError(text); got != want {
			t.Errorf("%q: %s, want %s", text, got, want)
		}
	}
}

// Unusable until a known reset, however long ago it was recorded, and no longer after it; one too far off is misread
// and counts as no reset, like a limit whose words name no time zone (Codex's): the usual healthStale.
func TestLauncherHealthUsageLimitUnusableUntilTheReset(t *testing.T) {
	now := time.Now()
	ahead, past, absurd := now.Add(time.Hour), now.Add(-time.Minute), now.Add(9*24*time.Hour)
	limit := func(checked time.Duration, reset *time.Time) LauncherHealth {
		return LauncherHealth{State: healthFailing, Kind: errKindUsageLimit, CheckedAt: now.Add(-checked), ResetsAt: reset}
	}
	for _, c := range []struct {
		name string
		r    LauncherHealth
		want bool
	}{
		{"reset ahead, recorded longer ago than healthStale", limit(7*time.Hour, &ahead), true},
		{"reset past", limit(time.Hour, &past), false},
		{"no reset, recent", limit(time.Hour, nil), true},
		{"no reset, stale", limit(7*time.Hour, nil), false},
		{"absurd reset, recent", limit(time.Hour, &absurd), true},
		{"absurd reset, stale", limit(7*time.Hour, &absurd), false},
	} {
		if got := c.r.unusable(); got != c.want {
			t.Errorf("%s: unusable = %v, want %v", c.name, got, c.want)
		}
	}
	if _, ok := limit(time.Hour, &absurd).limitReset(); ok {
		t.Error("an absurd reset was read as one")
	}

	// Recorded: the reset off the words when they name a time zone, none when they do not; one handed in too far
	// off is dropped. A limit after a block clears the block: the request got through.
	s, f := launcherHealthHub(t)
	ctx := context.Background()
	s.healthRound(ctx, true)
	if r := healthState(s, "l4", "work"); r.State != healthBlocked {
		t.Fatalf("l4 not blocked first: %+v", r)
	}
	line, reset := sessionLimitLine(2 * time.Hour)
	r := s.recordHealth(ctx, "l4", "work", healthResult{err: "Error: " + line, source: healthSourceCheck})
	if r.State != healthFailing || r.Kind != errKindUsageLimit || r.ResetsAt == nil || !r.ResetsAt.Equal(reset) || r.BlockedReason != "" || !r.unusable() {
		t.Fatalf("with a reset: %+v", r)
	}
	r = s.recordHealth(ctx, "box", "work", healthResult{err: "You've hit your usage limit. Try again at 3:05 PM.", source: healthSourceCheck})
	if r.Kind != errKindUsageLimit || r.ResetsAt != nil || !r.unusable() {
		t.Fatalf("without a reset: %+v", r)
	}
	r = s.recordHealth(ctx, "box", "work", healthResult{err: line, source: healthSourceCheck, kind: errKindUsageLimit, resetsAt: &absurd})
	if r.ResetsAt != nil {
		t.Fatalf("an absurd reset kept: %+v", r)
	}
	if r = s.recordHealth(ctx, "l4", "work", healthResult{ok: true, source: healthSourceCheck}); r.State != healthOK || r.Kind != "" || r.ResetsAt != nil {
		t.Fatalf("after a pass: %+v", r)
	}
	if len(f.pushes) != 0 {
		t.Fatalf("pushed: %+v", f.pushes)
	}
}

// A repair agent's recheck that meets the usage limit does not block the launcher: the repair got it as far as the
// subscription.
func TestLauncherHealthRepairRecheckMeetsAUsageLimit(t *testing.T) {
	s, f := launcherHealthHub(t)
	f.hello.Agents.LauncherHealth.Repair.Skill = "agent-cli-setup"
	ctx := context.Background()
	s.healthRound(ctx, true)
	if r := healthState(s, "l4", "work"); r.State != healthRepairing {
		t.Fatalf("repairing: %+v", r)
	}
	line, reset := sessionLimitLine(time.Hour)
	f.mu.Lock()
	f.checks["l4/work"] = proto.LauncherCheck{Agent: "claude", Error: line}
	f.sessions["server"] = []proto.Session{{SessionID: "s1", MachineID: "server", State: proto.StateUnread, Status: proto.StatusIdle, LastText: "reconfigured the gateway"}}
	f.mu.Unlock()
	s.health.mu.Lock()
	s.health.repairs["l4"].StartedAt = time.Now().Add(-3 * time.Minute)
	s.health.mu.Unlock()
	s.followRepairs(ctx, s.healthConfig(ctx))
	r := healthState(s, "l4", "work")
	if r.State != healthFailing || r.Kind != errKindUsageLimit || r.BlockedReason != "" || r.ResetsAt == nil || !r.ResetsAt.Equal(reset) || len(s.health.repairs) != 0 {
		t.Fatalf("after the recheck: %+v, repairs %v", r, s.health.repairs)
	}
	s.setHealthState(ctx, "l4", "work", healthBlocked, "still failing", nil)
	if r := healthState(s, "l4", "work"); r.State != healthFailing {
		t.Fatalf("blocked: %+v", r)
	}
}

// Readiness reads a usage-limited launcher as a rate limit that passes by itself, until its reset.
func TestLauncherHealthUsageLimitInReadiness(t *testing.T) {
	_, s, _ := readyMachine(t, proto.Session{SessionID: "m1", State: proto.StateIdle, LastText: "你好，我是 Persona。", UpdatedAt: time.Now().Add(-time.Hour)})
	ctx := context.Background()
	line, reset := sessionLimitLine(time.Hour)
	s.recordHealth(ctx, "server", "claude", healthResult{err: line, source: healthSourceSession, kind: errKindUsageLimit, resetsAt: &reset})
	if m := readinessStep(t, getReadiness(t, s, "", ""), StepModel); m.State != StepProblem || m.Code != proto.TurnErrorRateLimited {
		t.Fatalf("limited: %+v", m)
	}
	editHealth(s, "server", "claude", func(r *LauncherHealth) { past := time.Now().Add(-time.Minute); r.ResetsAt = &past })
	if m := readinessStep(t, getReadiness(t, s, "", ""), StepModel); m.State != StepOK {
		t.Fatalf("after the reset: %+v", m)
	}
}
