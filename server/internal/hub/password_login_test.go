package hub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/compat"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/passwordpake"
)

type passwordMemStore struct {
	*adminMemStore
	password *db.ClientPassword
	tokens   map[string]db.DeviceToken
}

func (s *passwordMemStore) ClientPassword(context.Context) (*db.ClientPassword, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.password == nil {
		return nil, nil
	}
	p := *s.password
	return &p, nil
}
func (s *passwordMemStore) SaveClientPassword(_ context.Context, p db.ClientPassword, previous string, revoke bool) (bool, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if passwordRevision(s.password) != previous {
		return false, nil, nil
	}
	s.password = &p
	var ids []string
	if revoke {
		for hash, device := range s.tokens {
			ids = append(ids, device.ID)
			delete(s.tokens, hash)
		}
	}
	return true, ids, nil
}
func (s *passwordMemStore) IssuePasswordDevice(_ context.Context, revision string, d db.DeviceToken, hash string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.password == nil || !s.password.Enabled || s.password.Revision != revision {
		return false, nil
	}
	s.tokens[hash] = d
	return true, nil
}
func (s *passwordMemStore) DeviceTokenByHash(_ context.Context, hash string) (*db.DeviceToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.tokens[hash]
	if !ok {
		return nil, nil
	}
	return &d, nil
}
func (s *passwordMemStore) TouchDeviceToken(context.Context, string) error { return nil }

func passwordTestHub(t *testing.T) (*Hub, *passwordMemStore, *adminClient, *adminClient) {
	t.Helper()
	h, parent, _ := adminTestHub(t)
	s := &passwordMemStore{adminMemStore: parent, tokens: map[string]db.DeviceToken{}}
	h.store = s
	setAdmin(h, AdminConfig{Access: accessLoopback})
	cookie := "admin-test-cookie"
	s.sessions[sha256Hex(cookie)] = db.AdminSession{IDHash: sha256Hex(cookie), CSRF: "csrf-test", ExpiresAt: time.Now().Add(time.Hour)}
	// The existing admin guard also requires a configured administrator password.
	s.hash = "configured-admin-password"
	admin := &adminClient{t: t, h: h.routes(), remote: "127.0.0.1:1234", tls: true, csrf: "csrf-test", cookie: &http.Cookie{Name: compat.AdminCookieNames(true)[1], Value: cookie}}
	client := &adminClient{t: t, h: h.routes(), remote: "192.0.2.9:1234", tls: true}
	return h, s, admin, client
}

func passwordCall(t *testing.T, c *adminClient, method, path string, in any, want int) map[string]any {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	status, out, _ := c.do(method, path, string(b))
	if status != want {
		t.Fatalf("%s %s: got %d, want %d: %+v", method, path, status, want, out)
	}
	return out
}

func registerClientPassword(t *testing.T, admin *adminClient, password, previous string, revoke bool) {
	t.Helper()
	c, _ := passwordpake.Configuration().Client()
	request, err := c.RegistrationInit([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	start := passwordCall(t, admin, "POST", "/api/v1/admin/password-login/register/start", map[string]any{"message": request.Serialize(), "revision": previous}, 200)
	m, _ := base64.StdEncoding.DecodeString(start["message"].(string))
	response, err := c.Deserialize.RegistrationResponse(m)
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := c.RegistrationFinalize(response, passwordpake.ClientIdentity, []byte(start["identity"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	passwordCall(t, admin, "POST", "/api/v1/admin/password-login/register/finish", map[string]any{"session": start["session"], "message": record.Serialize(), "revoke_devices": revoke}, 200)
}

func passwordProof(t *testing.T, client *adminClient, password string) (string, []byte, error) {
	t.Helper()
	c, _ := passwordpake.Configuration().Client()
	ke1, err := c.GenerateKE1([]byte(password))
	if err != nil {
		t.Fatal(err)
	}
	start := passwordCall(t, client, "POST", "/api/v1/auth/password/start", map[string]any{"message": ke1.Serialize(), "name": "测试设备", "kind": "web"}, 200)
	m, _ := base64.StdEncoding.DecodeString(start["message"].(string))
	ke2, err := c.Deserialize.KE2(m)
	if err != nil {
		t.Fatal(err)
	}
	ke3, _, _, err := c.GenerateKE3(ke2, passwordpake.ClientIdentity, []byte(start["identity"].(string)))
	if err != nil {
		return start["session"].(string), nil, err
	}
	return start["session"].(string), ke3.Serialize(), nil
}

func TestPasswordLoginAdminCookies(t *testing.T) {
	_, _, legacy, _ := passwordTestHub(t)
	names := compat.AdminCookieNames(true)
	if len(names) != 2 || names[0] != "persona_admin_tls" || names[0] == names[1] {
		t.Fatalf("unexpected secure admin cookie names: %v", names)
	}
	passwordCall(t, legacy, "GET", "/api/v1/admin/password-login", nil, 200)
	current := *legacy
	current.cookie = &http.Cookie{Name: names[0], Value: legacy.cookie.Value}
	passwordCall(t, &current, "GET", "/api/v1/admin/password-login", nil, 200)
	for _, invalid := range []string{"", "invalid"} {
		mixed := *legacy
		mixed.header = http.Header{"Cookie": {(&http.Cookie{Name: names[0], Value: invalid}).String()}}
		passwordCall(t, &mixed, "GET", "/api/v1/admin/password-login", nil, 401)
	}
	current.header = http.Header{"Cookie": {(&http.Cookie{Name: names[1], Value: "invalid"}).String()}}
	passwordCall(t, &current, "GET", "/api/v1/admin/password-login", nil, 200)
}

func TestPasswordLoginProxyTLS(t *testing.T) {
	h, _, admin, client := passwordTestHub(t)
	registerClientPassword(t, admin, "client password", "", false)
	h.admin.gateways = func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("fd00::1")} }
	for _, tc := range []struct {
		name, remote, proto, forwarded string
		tls                            bool
		want                           int
	}{
		{"native TLS", "192.0.2.9:1234", "", "", true, 200},
		{"loopback", "127.0.0.1:1234", "", "", false, 200},
		{"LAN HTTPS proxy", "[fd00::20]:1234", "https", "", false, 200},
		{"LAN Forwarded proxy", "[fd00::20]:1234", "", "proto=https", false, 200},
		{"LAN plaintext", "[fd00::20]:1234", "", "", false, 403},
		{"LAN HTTP proxy", "[fd00::20]:1234", "http", "", false, 403},
		{"public plaintext", "192.0.2.9:1234", "", "", false, 403},
		{"public spoofed HTTPS", "192.0.2.9:1234", "https", "", false, 403},
		{"public spoofed Forwarded", "192.0.2.9:1234", "", "proto=https", false, 403},
		{"untrusted container gateway", "[fd00::1]:1234", "https", "", false, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := *client
			c.t, c.remote, c.tls = t, tc.remote, tc.tls
			c.header = http.Header{"X-Forwarded-Proto": {tc.proto}, "Forwarded": {tc.forwarded}}
			state := passwordCall(t, &c, "GET", "/api/v1/auth/password/state", nil, tc.want)
			if tc.want == 200 && state["enabled"] != true {
				t.Fatal(state)
			}
			if tc.want == 403 && state["code"] != "tls_required" {
				t.Fatal(state)
			}
		})
	}
	// Complete the password exchange through the proxy, not just discovery.
	client.remote, client.tls = "[fd00::20]:1234", false
	client.header = http.Header{"X-Forwarded-Proto": {"https"}}
	session, proof, err := passwordProof(t, client, "client password")
	if err != nil {
		t.Fatal(err)
	}
	result := passwordCall(t, client, "POST", "/api/v1/auth/password/finish", map[string]any{"session": session, "message": proof}, 200)
	principal, ok := h.authenticate(context.Background(), result["token"].(string))
	if !ok || principal.Role != roleFull {
		t.Fatal("proxy login did not issue a usable device token")
	}
	client.header.Set("Origin", "https://attacker.example")
	passwordCall(t, client, "POST", "/api/v1/auth/password/start", map[string]any{}, 403)
}
