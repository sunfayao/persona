package extmgr

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/extension"
)

type unavailableMarketStore struct{ *extensionStore }

func (s unavailableMarketStore) ExtensionMarkets(context.Context) ([]extension.MarketRecord, error) {
	return nil, errors.New("market registrations unavailable")
}

func TestExtensionAutoUpgradeMarketRegistration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		loaded    bool
		state     *marketState
		loadFails bool
		paused    string
	}{
		{name: "registered but not loaded"},
		{name: "registration load failed", loadFails: true},
		{name: "removed", loaded: true, paused: "market_removed"},
		{name: "catalog unreadable", loaded: true, state: &marketState{rec: extension.MarketRecord{Error: "catalog unavailable"}}},
		{name: "entry absent", loaded: true, state: &marketState{index: &extension.MarketIndex{}}},
		{name: "bad signature", loaded: true, state: &marketState{rec: extension.MarketRecord{Signature: extension.MarketSignature{State: extension.SigInvalid}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestManager(t)
			st := newExtensionStore()
			h.store = st
			if tc.loadFails {
				h.store = unavailableMarketStore{st}
			}
			h.extensions = &extensions{h: h, root: t.TempDir()}
			h.extensions.reg = extension.NewRegistry(h.extensions.root, st, nil, 0)
			h.extensions.markets = markets{loaded: tc.loaded, byID: map[string]*marketState{}}
			if tc.state != nil {
				h.extensions.markets.byID["source"] = tc.state
			}
			if !tc.loaded && !tc.loadFails {
				now := time.Now()
				if err := st.PutExtensionMarket(t.Context(), extension.MarketRecord{ID: "source", Spec: "https://market.example.com/", FetchedAt: &now}); err != nil {
					t.Fatal(err)
				}
			}
			p := &extension.Extension{Record: extension.Record{ID: "example-ui", Source: extension.SourceInstalled, Origin: "market:source/example-ui", Enabled: true}}
			var v ExtensionView
			h.extensionUpgrades().decorate(t.Context(), &v, p)
			if v.AutoUpgrade == nil || !*v.AutoUpgrade || v.AutoUpgradePaused != tc.paused || v.Upgrade != nil {
				t.Fatalf("registration state: %+v", v)
			}
		})
	}
}
