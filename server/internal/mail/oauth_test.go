package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testClientID = "1234-abcd.apps.googleusercontent.com"

func newOAuthService(t *testing.T) (*Service, *memRecords) {
	t.Helper()
	recs := newMemRecords()
	s, err := New(t.TempDir(), recs)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	s.verify = func(context.Context, *MailAccount) error { return nil }
	return s, recs
}

func TestStoredGoogleCredentialsRefresh(t *testing.T) {
	s, recs := newOAuthService(t)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "refresh_token" ||
			r.Form.Get("client_id") != testClientID || r.Form.Get("refresh_token") != "rt-secret" {
			w.WriteHeader(http.StatusBadRequest)
			return
func TestStoredGoogleCredentialRevocation(t *testing.T) {
	s, _ := newOAuthService(t)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
		_, _ = w.Write([]byte(`{"access_token":"at-2","expires_in":3600}`))
	}))
	defer endpoint.Close()
	defer srv.Close()
	old := googleTokenURL
	googleTokenURL = endpoint.URL
	defer func() { googleTokenURL = old }()
	a := &MailAccount{ID: "acc_test", Email: "me@gmail.com", Provider: ProviderGmail, Auth: AuthOAuth2,
		OAuth: &OAuthCreds{ClientID: testClientID, RefreshToken: "rt-secret"}}
	if err := s.store.save([]*MailAccount{a}); err != nil {
	defer func() { googleTokenURL = old }()
		t.Fatal(err)
	if err != nil {
		t.Fatal(err)
	}
}

func TestXOAUTH2(t *testing.T) {
	if got := string(xoauth2Payload("me@gmail.com", "tok")); got != "user=me@gmail.com\x01auth=Bearer tok\x01\x01" {
		t.Fatalf("payload = %q", got)
	}
	s, _ := newOAuthService(t)
	s.refreshAccess = func(*OAuthCreds) (string, error) { return "tok", nil }
	c, err := s.smtpAuth(context.Background(), &MailAccount{Auth: AuthOAuth2, Username: "me@gmail.com", OAuth: &OAuthCreds{RefreshToken: "r"}})
	if err != nil {
		t.Fatal(err)
	}
	mech, ir, _ := c.Start()
	if mech != "XOAUTH2" || string(ir) != string(xoauth2Payload("me@gmail.com", "tok")) {
		t.Fatalf("mech %s ir %q", mech, ir)
	}
}

func TestGmailAPISendAndCheck(t *testing.T) {
	s, _ := newOAuthService(t)
	s.refreshAccess = func(*OAuthCreds) (string, error) { return "tok", nil }
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"bad token"}}`))
			return
		}
		switch r.URL.Path {
		case "/gmail/v1/users/me/profile":
			w.Write([]byte(`{"emailAddress":"Me@gmail.com"}`))
		case "/gmail/v1/users/me/messages/send":
			var body struct{ Raw string }
			json.NewDecoder(r.Body).Decode(&body)
			b, _ := base64.RawURLEncoding.DecodeString(body.Raw)
			sent = string(b)
			w.Write([]byte(`{"id":"1"}`))
		}
	}))
	defer srv.Close()
	old := gmailAPIBase
	gmailAPIBase = srv.URL
	defer func() { gmailAPIBase = old }()

	a := &MailAccount{ID: "acc_g", Email: "me@gmail.com", Username: "me@gmail.com", Provider: ProviderGmail, Auth: AuthOAuth2,
		OAuth: &OAuthCreds{ClientID: testClientID, RefreshToken: "r"}}
	if err := s.gmailAPICheck(context.Background(), a); err != nil {
		t.Fatalf("check: %v", err)
	}
	s.mu.Lock()
	s.accts[a.ID] = a
	s.mu.Unlock()
	err := s.Send(context.Background(), SendRequest{AccountID: a.ID, To: []Address{{Email: "you@example.com"}},
		Bcc: []Address{{Name: "Hidden", Email: "bcc@example.com"}}, Subject: "hi", Text: "hello"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.HasPrefix(sent, "Bcc: \"Hidden\" <bcc@example.com>\r\n") || !strings.Contains(sent, "you@example.com") {
		t.Fatalf("raw = %q", sent)
	}

	a.Email = "other@gmail.com"
	if err := s.gmailAPICheck(context.Background(), a); err == nil {
		t.Fatal("profile of another address accepted")
	}
}
