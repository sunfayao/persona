package extmgr

import (
	"context"
	"testing"
	"time"
)

func TestPageNativeFiniteLoadStillStartsWithLoading(t *testing.T) {
	h, process, output := pageProcessManager(t)
	pageFixture(t, h, "loading", map[string]any{"enabled": true, "delay": 200})
	tree := pageOpenTest(t, h, pageWho)
	if tree.Snapshot().View.Sections[0].Items[0].Text != "loading" || pageStats(t, process)["snapshotTransport"] != 0 {
		t.Fatal("native mount waited for snapshot readiness")
	}
	pageAwait(t, func() bool { return pageTree(h, tree.Session()).Snapshot().View.Sections[0].Items[0].Text == "loaded" })
	// The tree changes before the delivery goroutine sends the delta (ui_page_delivery.go): wait for the frame too,
	// or a busy machine reads the frames in between (#2152). A delta never delivered still fails, by the wait's timeout.
	pageAwait(t, func() bool { return len(output.all()) >= 2 })
	if pageStats(t, process)["loadCanceled"] != 0 {
		t.Fatal("native finite load did not publish its existing delta path")
	}
	t.Log("PASS native initial loading and loaded delta")
}

func TestPageHTTPCancelPendingFiniteMount(t *testing.T) {
	h, process, _ := pageProcessManager(t)
	pageFixture(t, h, "loading", map[string]any{"enabled": true, "delay": 200})
	ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	defer cancel()
	var in uiRequest
	in.Context.UIVersion = 6
	if _, err := h.uiCall(ctx, "example-page", "home", in); err == nil {
		t.Fatal("canceled HTTP mount returned a View")
	}
	pageAwait(t, func() bool { return pageStats(t, process)["loadActive"] == 0 })
	stats := pageStats(t, process)
	if stats["disposes"] != 1 || stats["loadCanceled"] != 1 {
		t.Fatalf("canceled finite factory retained: %v", stats)
	}
	h.uiPages.mu.Lock()
	left := len(h.uiPages.sessions)
	h.uiPages.mu.Unlock()
	if left != 0 {
		t.Fatal("retired finite-load reservation leaked")
	}
	t.Log("PASS canceled HTTP mount disposed and released")
}
