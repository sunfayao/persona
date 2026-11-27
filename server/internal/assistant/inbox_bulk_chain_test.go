package assistant

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestInboxBulkFilterKeepsOlderPendingAfterNewerFailure(t *testing.T) {
	for _, depth := range []int{3, 5, 9} {
		t.Run(fmt.Sprint("depth=", depth), func(t *testing.T) {
			h, st := newBulkFailHub(t)
			it := filingItem(h, "item")
			h.inbox.file(it, time.Now().Add(-time.Minute), "user")
			h.inbox.closeHandling(it, "user", time.Now().Add(-time.Minute))
			if failed := h.saveInbox(context.Background(), []*InboxItem{it}); len(failed) != 0 {
				t.Fatal("seeding the closed row failed")
			}
			// Construct each request's real mutation phase, then choose a save order independently of mutex waiters.
			var writes []*inboxFiling
			for i := 0; i < depth; i++ {
				h.inbox.mu.Lock()
				w := h.inbox.beginFiling(it, time.Now(), "user")
				if i%2 == 0 {
					it.DoneAt, it.DoneBy = nil, ""
				} else {
					h.inbox.file(it, w.now, w.why)
					w.close = true
				}
				h.inbox.settleFiling(w)
				writes = append(writes, w)
				h.inbox.mu.Unlock()
			}
			mux := h.routes()
			_, list := inboxCall(t, mux, "GET", "/api/v1/inbox", "")
			body := fmt.Sprintf(`{"action":"done","filter":{"source":"mail"},"as_of":%q}`, list["as_of"])
			st.failAll = true
			if failed := h.saveInboxFilings(context.Background(), writes[depth-1:]); !failed[it.ID] {
				t.Fatal("the newest pending reopen did not fail")
			}
			if it.open() || st.doneAt(it.ID) == nil {
				t.Fatal("the newer failure should leave both endpoints closed, with an older reopen still pending")
			}
			st.failAll = false
			code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", body)
			if code != 200 || !slices.Equal(jsonIDs(out["failed"]), []string{it.ID}) || len(out["changed"].([]any)) != 0 {
				t.Fatalf("a newer failure hid the older pending open state: HTTP %d %v", code, out)
			}
			for i, w := range writes[:depth-1] {
				st.failAll = i%2 == 1 // older reopens commit, filings fail
				if failed := h.saveInboxFilings(context.Background(), []*inboxFiling{w}); failed[it.ID] != st.failAll {
					t.Fatalf("older stage %d outcome = %v", i, failed)
				}
			}
			if !it.open() || st.doneAt(it.ID) != nil {
				t.Fatal("the surviving older reopen was lost")
			}
			st.failAll = false
			code, out = inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", body)
			if code != 200 || !slices.Equal(jsonIDs(out["ids"]), []string{it.ID}) || len(jsonIDs(out["failed"])) != 0 || it.open() {
				t.Fatalf("same-cutoff retry failed after the chain resolved: HTTP %d %v", code, out)
			}
		})
	}
}
