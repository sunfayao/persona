package feishubot

import (
	"testing"
	"time"
)

// newTestConn returns a connection with a controllable clock and no network.
func newTestConn() (*Conn, *time.Time) {
	c := NewConn(nil, nil, nil)
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	return c, &now
}

func partsLen(c *Conn) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.parts)
}

func TestCombineAssemblesPartsInTime(t *testing.T) {
	c, now := newTestConn()
	if got := c.combine("m", 2, 0, []byte("a")); got != nil {
		t.Fatalf("the first part assembled a message early: %q", got)
	}
	*now = now.Add(time.Second)
	if got := c.combine("m", 2, 1, []byte("b")); string(got) != "ab" {
		t.Fatalf("got %q, want ab", got)
	}
	if partsLen(c) != 0 {
		t.Fatal("a completed message stays in the parts map")
	}
}

// A slow link can deliver the parts more than partsLifetime apart; each arriving part proves the
// sender is still sending this message, so it must still assemble.
func TestCombineAssemblesPartsAfterSlowLinkGap(t *testing.T) {
	c, now := newTestConn()
	if got := c.combine("m", 2, 0, []byte("a")); got != nil {
		t.Fatalf("first part = %q, want nil", got)
	}
	*now = now.Add(partsLifetime + time.Second)
	if got := c.combine("m", 2, 1, []byte("b")); string(got) != "ab" {
		t.Fatalf("parts arriving %s apart assembled to %q, want ab", partsLifetime+time.Second, got)
	}
}

// Parts of one message keep arriving just under the lifetime apart; an unrelated split frame must
// not sweep the message, because a part of it arrived recently.
func TestCombineKeepsPartialAliveWhilePartsKeepArriving(t *testing.T) {
	c, now := newTestConn()
	c.combine("m", 3, 0, []byte("a"))
	*now = now.Add(8 * time.Second)
	c.combine("m", 3, 1, []byte("b"))
	*now = now.Add(8 * time.Second)
	c.combine("n", 2, 0, []byte("x")) // triggers the expiry sweep inside combine
	*now = now.Add(8 * time.Second)
	if got := c.combine("m", 3, 2, []byte("c")); string(got) != "abc" {
		t.Fatalf("got %q, want abc", got)
	}
}

func TestCombineRejectsBadSeq(t *testing.T) {
	c, _ := newTestConn()
	if got := c.combine("m", 2, -1, []byte("a")); got != nil {
		t.Fatalf("seq -1 returned %q", got)
	}
	if got := c.combine("m", 2, 2, []byte("a")); got != nil {
		t.Fatalf("seq 2 returned %q", got)
	}
	if partsLen(c) != 0 {
		t.Fatalf("a bad seq left %d partial(s) behind", partsLen(c))
	}
}

// The rest of a split message never arrives: once it is older than partsLifetime the periodic
// sweep reclaims it (each frame can be up to 16 MiB).
func TestAbandonedPartialIsReapedAfterTheLifetime(t *testing.T) {
	c, now := newTestConn()
	c.combine("m", 2, 0, []byte("first half that never gets a second"))
	*now = now.Add(partsLifetime)
	c.sweepParts()
	if partsLen(c) != 1 {
		t.Fatalf("a partial exactly at the lifetime was swept")
	}
	*now = now.Add(time.Second)
	c.sweepParts()
	if partsLen(c) != 0 {
		t.Fatalf("an expired abandoned partial is still held: %d partial(s)", partsLen(c))
	}
}

func TestSweepPartsKeepsFreshPartials(t *testing.T) {
	c, now := newTestConn()
	c.combine("old", 2, 0, []byte("a"))
	*now = now.Add(partsLifetime / 2)
	c.combine("recent", 2, 0, []byte("x"))
	*now = now.Add(partsLifetime/2 + time.Second) // old is 11s old, recent is 6s old
	c.sweepParts()
	c.mu.Lock()
	_, old := c.parts["old"]
	_, recent := c.parts["recent"]
	c.mu.Unlock()
	if old || !recent {
		t.Fatalf("sweep kept old=%v recent=%v, want old=false recent=true", old, recent)
	}
}
