package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

// A card's push carries the manager's level: with the switch on a P1 escalation is held (recorded, no alert) while a
// P0 one and a card that names no level (P0) alert.
func TestQuietCardPushHonoursTheManagersLevel(t *testing.T) {
	h := gradingHub(t)
	if err := h.push.setPushGrading(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	attempts := gradingAttempts(h)
	card := func(id string) proto.Interaction {
		return proto.Interaction{ID: id, MachineID: "m1", SessionID: "s1", Kind: proto.IxPermission, Summary: "git push"}
	}
	h.notifyInteractionAt("", card("ix_p1"), "box", false, assistantwire.LevelP1, "")
	if attempts() != 0 {
		t.Fatalf("a P1 escalation alerted %d times", attempts())
	}
	view := h.push.heldPushesView("", 0)
	if len(view) != 1 || view[0].Reason != interceptReasonP1 || view[0].RelatedKey != "ix:ix_p1" {
		t.Fatalf("intercepts %+v, want the held card", view)
	}
	h.notifyInteractionAt("", card("ix_p0"), "box", false, assistantwire.LevelP0, "")
	h.notifyInteraction("", card("ix_plain"), "box", false)
	h.notifyInteractionAt("", card("ix_bad"), "box", false, "P9", "") // a level this hub does not know: a card interrupts
	if attempts() != 3 {
		t.Fatalf("got %d alerts for P0, plain and unknown-level cards, want 3", attempts())
	}
}

// With the switch off every card alerts, whatever level the manager named.
func TestQuietOffCardPushIgnoresLevel(t *testing.T) {
	h := gradingHub(t)
	attempts := gradingAttempts(h)
	h.notifyInteractionAt("", proto.Interaction{ID: "ix1", MachineID: "m1", SessionID: "s1", Kind: proto.IxPermission, Summary: "ls"},
		"box", false, assistantwire.LevelP1, "")
	if attempts() != 1 {
		t.Fatalf("got %d alerts, want 1", attempts())
	}
	if view := h.push.heldPushesView("", 0); len(view) != 0 {
		t.Fatalf("held with the switch off: %+v", view)
	}
}

func TestPushGradingHelloAndLiveBroadcast(t *testing.T) {
	h := newTestHub(t)
	h.push.store = newGradingStore()
	c := &client{device: "web-1", out: make(chan []byte, 4)}
	addTestClient(h, c)
	if got, _ := h.hello()["push_grading"].(bool); got {
		t.Fatal("hello push_grading = true before enabling")
	}
	mux := http.NewServeMux()
	h.pushGradingRoutes(mux)
	put := func(body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/push/ln-mode", strings.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	put(`{"enabled":true}`)
	if got, _ := h.hello()["push_grading"].(bool); !got {
		t.Fatal("hello push_grading = false after enabling")
	}
	var msg map[string]any
	if err := json.Unmarshal(<-c.out, &msg); err != nil || msg["type"] != "push_grading" || msg["enabled"] != true {
		t.Fatalf("broadcast = %+v, err=%v", msg, err)
	}
	put(`{"enabled":false}`)
	if got, _ := h.hello()["push_grading"].(bool); got {
		t.Fatal("hello push_grading = true after disabling")
	}
	if err := json.Unmarshal(<-c.out, &msg); err != nil || msg["type"] != "push_grading" || msg["enabled"] != false {
		t.Fatalf("broadcast = %+v, err=%v", msg, err)
	}
}
