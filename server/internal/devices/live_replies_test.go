package devices

import (
	"context"
	"testing"
)

// Never set (a hub upgraded without anyone touching the switch), live replies are on.
func TestLiveRepliesDefaultOn(t *testing.T) {
	h := newTestDevices(t)
	m, _ := herdrMac(t, h, "mac", false)
	if !m.liveReplies(context.Background()) {
		t.Fatal("configureNode would send off")
	}
}

// Connecting tells the node whether its Claude starts load the live replies plugin: on unless the user turned it off.
func TestConfigureNodeSaysLiveReplies(t *testing.T) {
	_, _, m, _ := dirsHub(t)
	f := connectDirsNode(t, m)
	ctx := context.Background()
	m.configureNode(ctx, m.peer, nil, true)
	if err := m.d.opts.Store.PutSetting(ctx, liveRepliesKey(m.cfg.ID), false); err != nil {
		t.Fatal(err)
	}
	m.configureNode(ctx, m.peer, nil, true)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.configure) != 2 || string(f.configure[0]["claude_live_replies"]) != "true" || string(f.configure[1]["claude_live_replies"]) != "false" {
		t.Fatalf("claude_live_replies in node.configure: %v", f.configure)
	}
}
