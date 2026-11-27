package node

import (
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
)

// Only one start holds a kept tab at a time; the others are refused until it is released, and a released tab is
// kept again (startremedy.go).
func TestKeptTabIsClaimedOnce(t *testing.T) {
	n, _, _ := herdrNode(t)
	created := &host.Created{Pane: host.PaneRef{ID: "w1:p2", Kind: host.Herdr}, SlotID: "w1:t2", SpaceID: "w1"}
	n.mu.Lock()
	n.keptStarts = map[string]keptStart{
		"w1:p2": {created: created, until: time.Now().Add(time.Minute)},
	}
	n.mu.Unlock()
	start := make(chan struct{})
	got := make(chan *keptClaim, 2)
	for range 2 {
		go func() {
			<-start
			c, err := n.claimKeptTab("w1:p2")
			if c == nil && rpcCode(err) != "start_tab_taken" {
				t.Errorf("the second claim: %v", err)
			}
			got <- c
		}()
	}
	close(start)
	var held *keptClaim
	for range 2 {
		if c := <-got; c != nil {
			if held != nil {
				t.Fatal("kept tab was claimed twice")
			}
			held = c
		}
	}
	if held == nil {
		t.Fatal("kept tab was not claimed")
	}
	if _, ok := n.keptStartTab("w1:p2"); ok {
		t.Error("start.keys could type into a held tab")
	}
	held.release()
	if c, err := n.claimKeptTab("w1:p2"); c == nil || err != nil {
		t.Errorf("a released tab is not kept again: %v", err)
	}
}
