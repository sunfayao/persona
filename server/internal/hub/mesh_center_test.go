package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/devices"
)

// The single-center mesh end to end: hubs in this process on 127.0.0.1 over TLS, nodes in
// the process, apps on WebSockets, a connector played by a TLS server here.

func init() {
	devices.ShortenTimings(200*time.Millisecond, 300*time.Millisecond, 2*time.Second) // the manager's
	revocationResendEvery = 200 * time.Millisecond
	credsHandoverRetryEvery = 200 * time.Millisecond
}

// joinHubs has a use b's code: a (and its group) joins b's group.
func joinHubs(t *testing.T, a, b *testHub) {
	t.Helper()
	pairHubs(t, a, b)
}

func (mh *testHub) waitCenter(t *testing.T, center string) {
	t.Helper()
	eventually(t, 20*time.Second, mh.h.cfg.Name+" takes "+shortID(center)+" for the center", func() bool {
		return mh.h.centerID() == center
	})
}

func (mh *testHub) waitReachable(t *testing.T, target string) {
	t.Helper()
	eventually(t, 20*time.Second, mh.h.cfg.Name+" sees "+shortID(target)+" reachable", func() bool {
		return mh.h.meshView().reachable(target)
	})
}

// The headers a hub opens a node stream with mean nothing from outside: a request that carries them is still an app's,
// and without a credential a member answers it 401 like any other.
func TestCenterMemberDropsNodeStreamHeaders(t *testing.T) {
	box := newMeshNode("uid-box", "box", "s1")
	lap := newMeshNode("uid-lap", "lap", "s2")
	box.resident, lap.resident = true, true
	center := startTestHub(t, hubB, "Office", []MachineConfig{{ID: "box", Name: "Box", Kind: "ssh"}}, []*meshNode{box})
	member := startTestHub(t, hubA, "家里", []MachineConfig{{ID: "lap", Name: "Laptop", Kind: "link"}}, []*meshNode{lap})
	joinHubs(t, member, center)
	member.waitCenter(t, hubB)
	center.waitCenter(t, hubB)
	tok, _ := center.deviceToken(t, roleFull)
	// The way to both computers is there: a refusal below is not a route still settling.
	eventually(t, 15*time.Second, "both machines online on the center", func() bool {
		ms := center.machines(t, tok)
		return ms["box"].Online && ms["lap"].Online
	})
	for _, target := range []string{hubA + "/lap", hubB + "/box"} {
		req, err := http.NewRequest(http.MethodGet, member.srv.URL+"/api/v1/anything", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", nodeUpgrade)
		req.Header.Set(hdrPeerNode, target)
		req.Header.Set(hdrPeerFrom, hubB)
		resp, err := insecureClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("no credential, node stream headers for %s: %d, want 401", target, resp.StatusCode)
		}
	}
}

// A member from before the headers were dropped still passes them on, naming the center as the opener: the center knows
// it opened no such stream.
func TestCenterRefusesNodeStreamNamingItAsOpener(t *testing.T) {
	center := startTestHub(t, hubB, "Office", nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/anything", nil)
	req.Header.Set(hdrPeerTTL, "8")
	req.Header.Set(hdrPeerNode, hubA+"/lap")
	req.Header.Set(hdrPeerFrom, hubB)
	rec := httptest.NewRecorder()
	center.h.servePeerHTTP(rec, req)
	if rec.Code != http.StatusLoopDetected {
		t.Fatalf("got %d, want 508", rec.Code)
	}
}

// A credential revoked on the center stays revoked when the center moves to a hub that still had a copy.
func TestCenterRevocationSurvivesCenterMove(t *testing.T) {
	center := startTestHub(t, hubB, "Office", nil, nil)
	member := startTestHub(t, hubA, "家里", nil, nil)
	tok, id := member.deviceToken(t, roleFull) // paired with the member before it joined
	admin, _ := center.deviceToken(t, roleFull)
	joinHubs(t, member, center)
	member.waitCenter(t, hubB)
	center.waitCenter(t, hubB)
	center.waitReachable(t, hubA)
	eventually(t, 20*time.Second, "the credential handed over", func() bool {
		st, _ := center.do(t, http.MethodGet, "/api/v1/machines", tok, "")
		return st == http.StatusOK
	})
	if st, b := center.do(t, http.MethodDelete, "/api/v1/auth/tokens/"+id, admin, ""); st != http.StatusOK {
		t.Fatalf("revoke: %d %s", st, b)
	}
	eventually(t, 20*time.Second, "the member's copy revoked", func() bool {
		dt, err := member.store.DeviceTokenByHash(context.Background(), hashToken(tok))
		return err == nil && dt == nil
	})
	center.waitReachable(t, hubA)
	if _, err := center.h.setCenter(context.Background(), hubA, false); err != nil {
		t.Fatal(err)
	}
	member.waitCenter(t, hubA)
	center.waitCenter(t, hubA)
	eventually(t, 20*time.Second, "a revoked credential on the new center: 401", func() bool {
		st, _ := member.do(t, http.MethodGet, "/api/v1/machines", tok, "")
		return st == http.StatusUnauthorized
	})
}

// A member that was away when a credential was revoked hears of it when it is back.
func TestCenterRevocationReachesHubThatWasAway(t *testing.T) {
	aStore, aDir := newMeshStore(), t.TempDir()
	center := startTestHub(t, hubB, "Office", nil, nil)
	member := startTestHubWith(t, hubA, "家里", aStore, aDir, nil, nil)
	tok, id := member.deviceToken(t, roleFull)
	admin, _ := center.deviceToken(t, roleFull)
	joinHubs(t, member, center)
	member.waitCenter(t, hubB)
	center.waitCenter(t, hubB)
	center.waitReachable(t, hubA)
	eventually(t, 20*time.Second, "the credential handed over", func() bool {
		st, _ := center.do(t, http.MethodGet, "/api/v1/machines", tok, "")
		return st == http.StatusOK
	})
	member.stop()
	eventually(t, 20*time.Second, "the center sees the member gone", func() bool { return !center.h.meshView().reachable(hubA) })
	if st, b := center.do(t, http.MethodDelete, "/api/v1/auth/tokens/"+id, admin, ""); st != http.StatusOK {
		t.Fatalf("revoke: %d %s", st, b)
	}
	if dt, _ := aStore.DeviceTokenByHash(context.Background(), hashToken(tok)); dt == nil {
		t.Fatal("the member heard while it was away")
	}
	startTestHubWith(t, hubA, "家里", aStore, aDir, nil, nil)
	eventually(t, 20*time.Second, "the member's copy revoked", func() bool {
		dt, err := aStore.DeviceTokenByHash(context.Background(), hashToken(tok))
		return err == nil && dt == nil
	})
}

// What a hub takes from another: only an app's credential. One it does not know stays revoked, so a copy handed over
// later does not bring it back.
func TestCredsRevokedTakesOnlyAppCredentials(t *testing.T) {
	center := startTestHub(t, hubB, "Office", nil, nil)
	member := startTestHub(t, hubA, "家里", nil, nil)
	joinHubs(t, member, center)
	member.waitCenter(t, hubB)
	center.waitCenter(t, hubB)
	ctx := context.Background()
	hash := func(s string) string { return hashToken(s) }
	linkDT := db.DeviceToken{ID: "dev_link", Name: "link", Kind: "link", Role: roleLink, MachineID: "mac"}
	if err := center.store.CreateDeviceToken(ctx, linkDT, hash("link")); err != nil {
		t.Fatal(err)
	}
	send := func(from string, list ...db.ExportedToken) error {
		raw, _ := json.Marshal(map[string]any{"tokens": list})
		_, err := center.h.meshLocalCall(ctx, from, "creds.revoked", raw)
		return err
	}
	gone := db.ExportedToken{DeviceToken: db.DeviceToken{ID: "dev_gone", Name: "old", Kind: "phone", Role: roleFull}, Hash: hash("gone")}
	asLink := db.ExportedToken{DeviceToken: linkDT, Hash: hash("link")}
	asFull := asLink
	asFull.Role = roleFull // a hub's word for the role does not reach a credential that is not an app's
	if err := send(hubA, asLink, asFull, gone); err != nil {
		t.Fatal(err)
	}
	if dt, _ := center.store.DeviceTokenByHash(ctx, hash("link")); dt == nil {
		t.Fatal("a persona-link credential was revoked")
	}
	if n, err := center.store.ImportDeviceTokens(ctx, []db.ExportedToken{gone}); err != nil || n != 0 {
		t.Fatalf("handing over a revoked credential: %d %v", n, err)
	}
	if dt, _ := center.store.DeviceTokenByHash(ctx, hash("gone")); dt != nil {
		t.Fatal("a copy handed over brought a revoked credential back")
	}
}
