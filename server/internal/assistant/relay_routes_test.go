package assistant

import (
	"strings"
	"testing"
	"time"
)

// #10099: the relay limits keep loops and spread in check without cutting off the main conversation's dispatching.

// A follow-up that wakes a conversation starts a new chain, but only once per conversation in the relay window: a
// conversation cut off by the hops can't schedule itself a minute ahead again and again to keep a loop going.
func TestScheduledWakeStartsANewChain(t *testing.T) {
	s, send := relayChain(t)
	for i := range 2 * relayMaxHops {
		from, to := "a", "b"
		if i%2 == 1 {
			from, to = to, from
		}
		forgetRate(s)
		if _, err := send(from, to); err != nil {
			t.Fatalf("message %d: %v", i+1, err)
		}
	}
	forgetRate(s)
	if _, err := send("a", "b"); err == nil || !strings.Contains(err.Error(), "hop 4") {
		t.Fatalf("a's fourth request went: %v", err)
	}
	now := time.Now()
	s.scheduledWake("a", now)
	if hop, err := send("a", "b"); err != nil || hop != 1 {
		t.Fatalf("after a's follow-up woke it: hop %d, %v (want a new chain at hop 1)", hop, err)
	}
	// A second wake in the same window does not start another chain.
	for i := range 2 {
		forgetRate(s)
		if _, err := send("b", "a"); err != nil && i == 0 {
			t.Fatal(err)
		}
		forgetRate(s)
		_, _ = send("a", "b")
	}
	s.relays.mu.Lock()
	before := s.relays.st.Hops["a"]
	s.relays.mu.Unlock()
	s.scheduledWake("a", now.Add(time.Minute))
	s.relays.mu.Lock()
	after, kept := s.relays.st.Hops["a"]
	s.relays.mu.Unlock()
	if !kept || after.Hop != before.Hop {
		t.Fatalf("a second wake within the window reset the chain: %+v → %+v", before, after)
	}
	s.scheduledWake("a", now.Add(relayWindow+time.Second))
	s.relays.mu.Lock()
	_, kept = s.relays.st.Hops["a"]
	s.relays.mu.Unlock()
	if kept {
		t.Fatal("a wake after the window did not start a new chain")
	}
}
