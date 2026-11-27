package link

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestParsePresenceLine(t *testing.T) {
	for line, want := range map[string]Presence{
		`{"presence":{"active":true,"notifies":true}}`:  {Active: true, Notifies: true},
		`{"presence":{"active":false,"notifies":true}}`: {Notifies: true},
		`{"presence":{}}`: {},
	} {
		if got, ok := ParsePresenceLine([]byte(line)); !ok || got != want {
			t.Errorf("%s: %+v %v, want %+v", line, got, ok, want)
		}
	}
	for _, line := range []string{``, `{}`, `{"presence":null}`, `{"other":{"active":true}}`} {
		if p, ok := ParsePresenceLine([]byte(line)); ok {
			t.Errorf("%q read as presence %+v", line, p)
		}
	}
}

// What the manager is told: nothing before the app said anything, what is left of the TTL while an active line is
// fresh, inactive once it is PresenceTTL old (the app stopped saying it).
func TestPresenceBoxLatest(t *testing.T) {
	t0 := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	if _, ok := (*PresenceBox)(nil).Latest(t0); ok {
		t.Fatal("a nil box (no app) has a presence")
	}
	b := NewPresenceBox()
	if _, ok := b.Latest(t0); ok {
		t.Fatal("a presence before the app said one")
	}
	b.Set(Presence{Active: true, Notifies: true}, t0)
	for after, want := range map[time.Duration]PresenceInfo{
		0:                                {Active: true, Notifies: true, TTLMS: 120_000},
		30 * time.Second:                 {Active: true, Notifies: true, TTLMS: 90_000},
		PresenceTTL - time.Millisecond:   {Active: true, Notifies: true, TTLMS: 1},
		PresenceTTL - time.Millisecond/2: {Notifies: true},
		PresenceTTL:                      {Notifies: true},
		PresenceTTL + 10*time.Minute:     {Notifies: true},
	} {
		if got, ok := b.Latest(t0.Add(after)); !ok || got != want {
			t.Errorf("%s after: %+v, want %+v", after, got, want)
		}
	}
	b.Set(Presence{Notifies: true}, t0)
	if got, _ := b.Latest(t0.Add(time.Second)); got != (PresenceInfo{Notifies: true}) {
		t.Errorf("inactive: %+v", got)
	}
}

func TestParseRangLine(t *testing.T) {
	if c, ok := ParseRangLine([]byte(`{"rang":{"interaction_id":"ix-1","conn":3}}`)); !ok || c != (Confirm{"ix-1", 3}) {
		t.Fatalf("%+v %v", c, ok)
	}
	for _, line := range []string{``, `{}`, `{"rang":null}`, `{"rang":{}}`, `{"rang":{"interaction_id":"","conn":1}}`,
		`{"rang":{"interaction_id":"ix-1"}}`, `{"rang":{"interaction_id":"ix-1","conn":0}}`, `{"presence":{"active":true}}`} {
		if c, ok := ParseRangLine([]byte(line)); ok {
			t.Errorf("%q read as a confirmation %+v", line, c)
		}
	}
}

// A restarted persona-link numbers its links above the earlier run's: a confirmation the app wrote for the earlier
// run's link matches none of the new one's.
func TestLinkNumbersOutliveARestart(t *testing.T) {
	srv, _ := fakeHubHello(t, func(c *websocket.Conn, _ Message) {
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	})
	connected := func(s Status) bool { return s.State == "connected" }
	first := waitFor(t, runAgainst(t, srv, "jd_x", Options{}), connected)
	// Each run seeds its counter from Unix milliseconds, then increments it for the first link.
	// Start the next run only once its seed is past the earlier link's number.
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for uint64(time.Now().UnixMilli()) <= first.Conn {
		select {
		case <-tick.C:
		case <-timeout.C:
			t.Fatal("clock did not advance past the earlier link's number")
		}
	}
	second := waitFor(t, runAgainst(t, srv, "jd_x", Options{}), connected)
	if second.Conn <= first.Conn {
		t.Fatalf("a later run's first link is %d, the earlier run's %d", second.Conn, first.Conn)
	}
}
