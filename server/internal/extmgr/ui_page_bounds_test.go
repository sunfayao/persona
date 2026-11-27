package extmgr

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/extwire"
)

func TestPageRequestsWaitForManagerStartup(t *testing.T) {
	h := New(Options{Config: &Config{}})
	envelope := json.RawMessage(`{"type":"extension_ui_stream","request":{"request":"open","protocol":1,"request_id":"startup","extension":"example-page","contribution":"home","context":{"stream":{"protocol":1,"components":["text"]}}}}`)
	raw, _ := json.Marshal(extwire.UIStreamCall{Who: pageWho, Envelope: envelope})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, handled, err := h.Request(ctx, extwire.MethodUIStream, raw)
	if !handled || err == nil {
		t.Fatal("page request reached uninitialized manager state")
	}
}

type quotaPageProcess struct{}

func (quotaPageProcess) Call(context.Context, string, any, any) error { return nil }
func (quotaPageProcess) Hold() func()                                 { return func() {} }
func (quotaPageProcess) Generation() uint64                           { return 1 }

func TestPageSessionConfiguredExtensionAndRuntimeTotals(t *testing.T) {
	for _, limit := range []PageSessionLimits{{Extension: 1}, {Runtime: 1}} {
		h := New(Options{Config: &Config{UIPageSessions: limit}})
		who := pageWho
		if _, err := h.pageReserve(who, "first", "home", quotaPageProcess{}, extension.UISlot{}, extension.UIParams{}); err != nil {
			t.Fatal(err)
		}
		who.Principal = "another-device"
		ext := "first"
		if limit.Runtime == 1 {
			ext = "second"
		}
		if _, err := h.pageReserve(who, ext, "home", quotaPageProcess{}, extension.UISlot{}, extension.UIParams{}); err == nil {
			t.Fatal("configured total quota bypassed")
		}
	}
	if boundedLimit(900, 8) != 8 || boundedLimit(-1, 8) != 8 || boundedLimit(4, 8) != 4 {
		t.Fatal("unsafe quota configuration")
	}
}
