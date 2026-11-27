package extmgr

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/extension"
)

// signedReadHub is a signed market already read once (its index cached) behind a server whose catalog
// answer the test chooses; packages are always served.
func signedReadHub(t *testing.T) (h *Manager, key marketKey, setCatalog func(http.HandlerFunc)) {
	t.Helper()
	key = newMarketKey(t)
	dir := uiPackage(t)
	pkg := tarDir(t, dir)
	var mu sync.Mutex
	var catalog http.HandlerFunc
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			_, _ = w.Write(pkg)
			return
		}
		mu.Lock()
		serve := catalog
		mu.Unlock()
		serve(w, r)
	}))
	t.Cleanup(srv.Close)
	raw := key.sign(marketIndex(t, "Example", marketEntry(t, dir, srv.URL+"/pkg.tar.gz", pkg)))
	setCatalog = func(f http.HandlerFunc) { mu.Lock(); catalog = f; mu.Unlock() }
	setCatalog(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(raw) })
	h = provenanceTestManager(t, map[string]*marketState{
		"example": {rec: extension.MarketRecord{ID: "example", Spec: "github:example/plugins", SigningKey: key.text, AddedAt: time.Now()}},
	}, srv.Client(), srv.URL)
	h.extensions.reg = extension.NewRegistry(h.extensions.root, h.store.(extension.Store), nil, 202611100000)
	if _, err := h.marketRead(context.Background(), "example"); err != nil {
		t.Fatal(err)
	}
	if st := storedSignature(h); st.State != extension.SigSigned {
		t.Fatalf("first read: %+v", st)
	}
	return h, key, setCatalog
}

func storedSignature(h *Manager) extension.MarketSignature {
	ms := h.marketsState()
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.byID["example"].rec.Signature
}

// installFromCache previews and installs example-ui from the signed market's kept index, as the install route does.
func installFromCache(t *testing.T, h *Manager) error {
	t.Helper()
	ctx := context.Background()
	rec, e, err := h.marketEntry("example", "example-ui")
	if err != nil {
		return err
	}
	mk, err := h.market(rec)
	if err != nil {
		t.Fatal(err)
	}
	pv, err := h.extensions.reg.PreviewMarket(ctx, mk, "example", e, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.extensions.reg.Install(ctx, pv.ID, pv.Digest, "test"); err != nil {
		t.Fatal(err)
	}
	return nil
}

// A catalog that was read (HTTP 200) and does not verify quarantines the market: unsigned, signed by another (old)
// signer, a broken signature, or not a catalog at all. Nothing installs, not even from the last good index.
func TestSignedMarketUnverifiedCatalogQuarantines(t *testing.T) {
	other := newMarketKey(t)
	for _, c := range []struct {
		name string
		body func(signed marketKey) []byte
	}{
		{"unsigned", func(marketKey) []byte { return []byte(`{"market_version":1,"name":"Example","extensions":[]}`) }},
		{"old signer", func(marketKey) []byte {
			return other.sign([]byte(`{"market_version":1,"name":"Example","extensions":[]}`))
		}},
		{"bad signature", func(k marketKey) []byte {
			return []byte(strings.Replace(string(k.sign([]byte(`{"market_version":1,"name":"Example","extensions":[]}`))), "Example", "Examp1e", 1))
		}},
		{"malformed", func(marketKey) []byte { return []byte(`{"payload":`) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, key, setCatalog := signedReadHub(t)
			body := c.body(key)
			served := make(chan struct{}, 4)
			setCatalog(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
				served <- struct{}{}
			})
			_, err := h.marketRead(context.Background(), "example")
			select {
			case <-served:
			default:
				t.Fatal("the negative case never received an HTTP 200 catalog")
			}
			if !errors.Is(err, errMarketSignature) || !errors.Is(err, extension.ErrCatalogUnverified) || errors.Is(err, extension.ErrCatalogUnavailable) {
				t.Fatalf("an unverified catalog was not a signature failure: %v", err)
			}
			if st := storedSignature(h); st.State != extension.SigInvalid {
				t.Fatalf("an unverified catalog left the market %+v", st)
			}
			if err := installFromCache(t, h); err == nil {
				t.Fatal("installed from a quarantined market")
			}
			// A network failure afterwards does not lift the quarantine.
			setCatalog(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
			if _, err := h.marketRead(context.Background(), "example"); errors.Is(err, errMarketSignature) {
				t.Fatalf("a later network failure was a signature failure: %v", err)
			}
			if st := storedSignature(h); st.State != extension.SigInvalid {
				t.Fatalf("a later network failure lifted the quarantine: %+v", st)
			}
		})
	}
}
