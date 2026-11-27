package assistant

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/prompts"
)

func TestInboxBulkPendingFilingDoesNotBlockOtherSuccesses(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint("firstFail=", fail), func(t *testing.T) {
			h, st := newBulkFailHub(t)
			pending, other := filingItem(h, "item"), filingItem(h, "other")
			if failed := h.saveInbox(context.Background(), []*InboxItem{pending, other}); len(failed) != 0 {
				t.Fatal("seeding stored rows failed")
			}
			if fail {
				st.failOnce = map[string]bool{pending.ID: true}
			}
			mux := h.routes()
			second := make(chan map[string]any, 1)
			var once sync.Once
			probe := &filingProbeStore{inboxPutFailStore: st, before: func(string, []byte) {
				once.Do(func() {
					go func() {
						code, out := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", `{"action":"dismiss","ids":["item","other"]}`)
						if code != 200 {
							t.Errorf("competing bulk status = %d", code)
						}
						second <- out
					}()
					waitForItem(t, h, other.ID, func(it *InboxItem) bool { return it.DoneBy == "dismissed" })
				})
			}}
			h.store = func() Store { return probe }
			inboxCall(t, mux, "POST", "/api/v1/inbox/item/done", `{"done":true}`)
			select {
			case out := <-second:
				if !slices.Equal(jsonIDs(out["failed"]), []string{pending.ID}) || !slices.Equal(jsonIDs(out["ids"]), []string{other.ID}) ||
					len(jsonIDs(out["unchanged"])) != 0 || !slices.Equal(jsonIDs(out["undo"].(map[string]any)["ids"]), []string{other.ID}) {
					t.Fatalf("mixed pending/committed bulk result: %v", out)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("competing bulk did not finish")
			}
			if other.open() || other.DoneBy != "dismissed" || pending.open() != fail {
				t.Fatalf("mixed bulk changed the wrong rows: pending=%s other=%s", stateOf(h, pending), stateOf(h, other))
			}
			if fail {
				for _, n := range pending.Agent.Pending {
					if n.Name == prompts.NoteClosed {
						t.Fatal("a failed pending row queued a stop note")
					}
				}
			}
		})
	}
}

func TestInboxBulkFilterIncludesPendingReopen(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		for _, action := range []string{bulkDone, bulkDismiss} {
			for _, fail := range []bool{false, true} {
				t.Run(fmt.Sprintf("bulk=%v/action=%s/firstFail=%v", bulk, action, fail), func(t *testing.T) {
					h, st := newBulkFailHub(t)
					it := filingItem(h, "item")
					h.inbox.file(it, time.Now().Add(-time.Minute), "user")
					h.inbox.closeHandling(it, "user", time.Now().Add(-time.Minute))
					arrived, source, tier := filingItem(h, "arrived"), filingItem(h, "source"), filingItem(h, "tier")
					for _, item := range []*InboxItem{it, arrived, source, tier} {
						item.Tier, item.Last = TierFYI, "1"
					}
					source.Source, tier.Tier = "example-source", TierAction
					if failed := h.saveInbox(context.Background(), []*InboxItem{it, arrived, source, tier}); len(failed) != 0 {
						t.Fatal("seeding existing stored rows failed")
					}
					if fail {
						st.failOnce = map[string]bool{it.ID: true}
					}
					mux := h.routes()
					var once sync.Once
					var competing map[string]any
					var body string
					var late *InboxItem
					probe := &filingProbeStore{inboxPutFailStore: st, before: func(string, []byte) {
						once.Do(func() {
							code, list := inboxCall(t, mux, "GET", "/api/v1/inbox?sources=all", "")
							if code != 200 {
								t.Fatalf("list status = %d", code)
							}
							visible := false
							for _, v := range list["items"].([]any) {
								item := v.(map[string]any)
								if item["id"] == it.ID && item["done_at"] == nil {
									visible = true
								}
							}
							if !visible {
								t.Fatal("the pending reopen was not listed as open")
							}
							asOf := list["as_of"].(string)
							listedAt, err := time.Parse(time.RFC3339Nano, asOf)
							if err != nil {
								t.Fatal(err)
							}
							h.inbox.mu.Lock()
							arrived.Last = "2" // a listed row receives new content after that list
							late = filingItem(h, "late")
							late.Tier, late.Last, late.ArrivedOf, late.ArrivedAt = TierFYI, "1", "1", listedAt.Add(time.Second)
							h.inbox.mu.Unlock()
							body = fmt.Sprintf(`{"action":%q,"filter":{"source":"mail","tier":"fyi"},"as_of":%q}`, action, asOf)
							code, competing = inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", body)
							if code != 200 {
								t.Errorf("competing filter status = %d", code)
							}
						})
					}}
					h.store = func() Store { return probe }
					path, firstBody := "/api/v1/inbox/item/done", `{"done":false}`
					if bulk {
						path, firstBody = "/api/v1/inbox/bulk", `{"action":"reopen","ids":["item"]}`
					}
					code, first := inboxCall(t, mux, "POST", path, firstBody)
					wantCode := 200
					if fail && !bulk {
						wantCode = 500
					}
					if code != wantCode || bulk && (len(jsonIDs(first["failed"])) == 1) != fail {
						t.Fatalf("first reopen = HTTP %d %v, want failure %v", code, first, fail)
					}
					if !slices.Equal(jsonIDs(competing["failed"]), []string{it.ID}) || len(competing["changed"].([]any)) != 0 ||
						len(jsonIDs(competing["unchanged"])) != 0 || len(jsonIDs(competing["ids"])) != 0 || competing["undo"] != nil {
						t.Fatalf("filter omitted the listed open pending row: %v", competing)
					}
					wantSkipped := []string{"arrived:changed", "late:changed"}
					if !slices.Equal(bulkIDs(competing["skipped"].([]any)), wantSkipped) {
						t.Fatalf("source/tier/as_of protections changed: %v", competing)
					}
					code, retry := inboxCall(t, mux, "POST", "/api/v1/inbox/bulk", body) // use the exact same list cutoff
					wantChanged := 0
					if !fail {
						wantChanged = 1
					}
					if code != 200 || len(jsonIDs(retry["failed"])) != 0 || len(retry["changed"].([]any)) != wantChanged ||
						!slices.Equal(bulkIDs(retry["skipped"].([]any)), wantSkipped) {
						t.Fatalf("retry after reopen resolution = HTTP %d %v", code, retry)
					}
					wantBy := "user"
					if !fail && action == bulkDismiss {
						wantBy = "dismissed"
					}
					if it.open() || it.DoneBy != wantBy || st.doneAt(it.ID) == nil || !arrived.open() || !late.open() || !source.open() || !tier.open() {
						t.Fatalf("retry filed the wrong rows: item=%s, other open=%v/%v/%v/%v", stateOf(h, it), arrived.open(), late.open(), source.open(), tier.open())
					}
					if st.doneAt(arrived.ID) != nil || st.doneAt(source.ID) != nil || st.doneAt(tier.ID) != nil || st.hasRow(late.ID) {
						t.Fatal("a protected row was filed or written")
					}
				})
			}
		}
	}
}
