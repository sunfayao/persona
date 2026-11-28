package autosupervise

import (
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

func TestOnlyExplicitTurnsTheSettingOff(t *testing.T) {
	t.Parallel()
	for setting, want := range map[string]bool{"": true, Auto: true, Explicit: false, "other": true} {
		if got := On(setting); got != want {
			t.Errorf("On(%q) = %v, want %v", setting, got, want)
		}
	}
}

func TestTwoAskersForOneRootGetOneStart(t *testing.T) {
	t.Parallel()
	g := New(&fakeClock{at: time.Unix(1000, 0)})
	if got := g.Begin("todo_a"); got != Start {
		t.Fatalf("first: %v", got)
	}
	if got := g.Begin("todo_a"); got != Wait {
		t.Fatalf("second while the first starts: %v", got)
	}
	if got := g.Begin("todo_b"); got != Start {
		t.Fatalf("another root: %v", got)
	}
	g.End("todo_a", true)
	if got := g.Begin("todo_a"); got != Start {
		t.Fatalf("after a good end the root may start again (its supervisor row is what stops it): %v", got)
	}
}

func TestAFailedStartCoolsDownForThirtyMinutes(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{at: time.Unix(1000, 0)}
	g := New(clock)
	g.Begin("todo_a")
	g.End("todo_a", false)
	if !g.Cooling("todo_a") || g.Begin("todo_a") != Fallback {
		t.Fatal("a failed start is not tried again at once")
	}
	if g.Begin("todo_b") != Start {
		t.Fatal("the cooldown is the root's own")
	}
	clock.add(Cooldown - time.Second)
	if g.Begin("todo_a") != Fallback {
		t.Fatal("still cooling a second before the end")
	}
	clock.add(time.Second)
	if g.Cooling("todo_a") || g.Begin("todo_a") != Start {
		t.Fatal("tried again once the cooldown is over")
	}
}

func TestAnUnreachableSupervisorCoolsTheRootToo(t *testing.T) {
	t.Parallel()
	g := New(&fakeClock{at: time.Unix(1000, 0)})
	g.Fail("todo_a")
	if g.Begin("todo_a") != Fallback {
		t.Fatal("no new start right after the supervisor was found unreachable")
	}
}

func TestAboveIsTheConversationThatSupervisedBefore(t *testing.T) {
	t.Parallel()
	isAgent := func(s string) bool { return strings.Contains(s, "/") }
	for _, c := range []struct{ above, supervisor, origin, main, want string }{
		{"side", "box/s1", "main", "main", "side"},
		{"", "side", "main", "main", "side"},
		{"", "box/s1", "side", "main", "side"},
		{"", "box/s1", "box/s2", "main", "main"},
		{"", "", "", "main", "main"},
	} {
		if got := Above(c.above, c.supervisor, c.origin, c.main, isAgent); got != c.want {
			t.Errorf("Above(%+v) = %q", c, got)
		}
	}
}

func TestFitAdoptsWhatFitsUnderTheBound(t *testing.T) {
	t.Parallel()
	take, left := Fit(6, []string{"a", "b", "c"}, 8)
	if len(take) != 2 || len(left) != 1 || left[0] != "c" {
		t.Fatalf("take %v left %v", take, left)
	}
	if take, left = Fit(9, []string{"a"}, 8); len(take) != 0 || len(left) != 1 {
		t.Fatalf("over the bound already: take %v left %v", take, left)
	}
	if take, left = Fit(0, []string{"a"}, 8); len(take) != 1 || left != nil {
		t.Fatalf("room: take %v left %v", take, left)
	}
}
