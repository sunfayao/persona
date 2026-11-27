package extmgr

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/extension"
)

// waitMarketsRead waits for the background reads of the markets to end.
func waitMarketsRead(t *testing.T, h *Manager) {
	t.Helper()
	ms := h.marketsState()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		ms.mu.Lock()
		n := len(ms.reading)
		ms.mu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the markets are still being read")
		}
	}
}

// reloadMarkets makes h read its markets from the store again, as a restarted (or upgraded) hub does.
func reloadMarkets(h *Manager) {
	ms := h.marketsState()
	ms.mu.Lock()
	ms.loaded, ms.byID = false, nil
	ms.mu.Unlock()
}

func marketList(t *testing.T, h *Manager, phone string) []map[string]any {
	t.Helper()
	code, out := call(t, h.routes(), "GET", "/api/v1/extensions/-/markets", phone, "")
	if code != 200 {
		t.Fatalf("markets: %d %v", code, out)
	}
	var list []map[string]any
	b, _ := json.Marshal(out["markets"])
	_ = json.Unmarshal(b, &list)
	return list
}

func storeSignedMarket(t *testing.T, h *Manager, key marketKey) {
	t.Helper()
	row := extension.MarketRecord{ID: "example-market", Spec: "github:example/plugins", SigningKey: key.text, AddedAt: time.Now()}
	if err := h.marketStore().PutExtensionMarket(t.Context(), row); err != nil {
		t.Fatal(err)
	}
}
