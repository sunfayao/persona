package mail

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/emersion/go-smtp"
)

const testMSClient = "11111111-2222-3333-4444-555555555555"

func TestStoredMicrosoftCredentialsRotate(t *testing.T) {
	s, recs := newOAuthService(t)
	var refreshes int
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes++
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "refresh_token" ||
			r.Form.Get("client_id") != testMSClient || r.Form.Get("refresh_token") != "rt-"+strconv.Itoa(refreshes) ||
			!strings.Contains(r.Form.Get("scope"), "IMAP.AccessAsUser.All") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at-" + strconv.Itoa(refreshes+1),
			"refresh_token": "rt-" + strconv.Itoa(refreshes+1), "expires_in": 3599})
	}))
	defer endpoint.Close()
	old := microsoftTokenURL
	microsoftTokenURL = endpoint.URL
	defer func() { microsoftTokenURL = old }()

	a := &MailAccount{ID: "acc_test", Email: "me@example.com", Provider: ProviderOutlook, Auth: AuthOAuth2,
		OAuth: &OAuthCreds{Provider: oauthMicrosoft, ClientID: testMSClient, RefreshToken: "rt-1"}}
	if err := s.store.save([]*MailAccount{a}); err != nil {
		t.Fatal(err)
	}
	s.accts[a.ID] = a
	ctx := context.Background()
	if tok, err := s.oauthToken(ctx, a); err != nil || tok != "at-2" {
		t.Fatalf("refresh: %q %v", tok, err)
	}
	s2, err := New(s.dir, recs)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	reloaded, err := s2.account(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.OAuth.RefreshToken != "rt-2" {
		t.Fatalf("the rotated refresh token was not kept: %q", reloaded.OAuth.RefreshToken)
	}
	if tok, err := s2.oauthToken(ctx, reloaded); err != nil || tok != "at-3" {
		t.Fatalf("refresh after a restart: %q %v", tok, err)
	}
}

func TestMicrosoftErrorsSayWhatToDo(t *testing.T) {
	for _, tc := range []struct {
		code, desc string
		refreshing bool
		want       string
	}{
		{"invalid_grant", "AADSTS65001: The user or administrator has not consented", false, "IT 管理员"},
		{"access_denied", "AADSTS90094: Admin consent is required", false, "管理员同意"},
		{"access_denied", "", false, "没有同意授权"},
		{"unauthorized_client", "AADSTS700016: Application with identifier was not found", false, "支持的帐户类型"},
		{"invalid_request", "AADSTS50011: The redirect URI does not match", false, DefaultMicrosoftRedirect},
		{"invalid_client", "AADSTS7000218: The request body must contain client_assertion", false, "允许公共客户端流"},
		{"invalid_grant", "AADSTS70008: The refresh token has expired", true, "重新"},
		{"invalid_grant", "AADSTS50173: The provided grant has expired due to it being revoked", true, "使用 Microsoft 账号登录"},
		{"temporarily_unavailable", "AADSTS90002: Tenant not found", false, "Microsoft 登录失败：temporarily_unavailable：AADSTS90002"},
	} {
		if err := microsoftError(tc.code, tc.desc, DefaultMicrosoftRedirect, tc.refreshing); !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %q → %v, want %q", tc.code, tc.desc, err, tc.want)
		}
	}
	for _, tc := range []struct{ err, want string }{
		{"imap: XOAUTH2: AUTHENTICATE failed.", "打开「允许设备和应用使用 IMAP」"},
		{"smtp: 535 5.7.139 Authentication unsuccessful, SmtpClientAuthentication is disabled for the Tenant.", "经过身份验证的 SMTP"},
		{"imap: dial: connection refused", "imap: dial: connection refused"},
	} {
		if got := outlookHint(errors.New(tc.err)); !strings.Contains(got.Error(), tc.want) {
			t.Errorf("%q → %v", tc.err, got)
		}
	}
}

// An Outlook account sends over SMTP with XOAUTH2 and nothing is appended to Sent (Exchange files it itself); a
// company that turned SMTP AUTH off gets told what to ask for.
func TestOutlookSendsOverSMTPWithXOAUTH2(t *testing.T) {
	srv := newTestIMAP(t)
	fs := newFakeSMTPServer(t)
	fs.token = "at-ok"
	s := newServiceWithAccount(t, srv)
	s.refreshAccess = func(*OAuthCreds) (string, error) { return "at-ok", nil }
	host, port, _ := net.SplitHostPort(fs.addr)
	a := srv.testAccount()
	a.Provider, a.Auth, a.Password = ProviderOutlook, AuthOAuth2, ""
	a.OAuth = &OAuthCreds{Provider: oauthMicrosoft, ClientID: testMSClient, RefreshToken: "rt"}
	a.SMTPHost, a.SMTPSecurity = host, "none"
	a.SMTPPort, _ = strconv.Atoi(port)
	s.mu.Lock()
	s.accts[a.ID] = a
	s.mu.Unlock()
	ctx := context.Background()
	if err := s.Send(ctx, SendRequest{AccountID: a.ID, To: []Address{{Email: "li@example.com"}}, Subject: "你好", Text: "正文"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if fs.authUser != a.Username {
		t.Fatalf("XOAUTH2 user %q", fs.authUser)
	}
	// Read Sent with the test server's password (it does not speak XOAUTH2): still empty.
	check := srv.testAccount()
	check.ID = "acc_check"
	s.mu.Lock()
	s.accts[check.ID] = check
	s.mu.Unlock()
	if res, err := s.ListMessages(ctx, ListOptions{AccountID: check.ID, Mailbox: "Sent"}); err != nil || len(res.Messages) != 0 {
		t.Fatalf("appended to Sent: %v %v", res, err)
	}
	fs.authErr = &smtp.SMTPError{Code: 535, EnhancedCode: smtp.EnhancedCode{5, 7, 139},
		Message: "Authentication unsuccessful, SmtpClientAuthentication is disabled for the Tenant."}
	if err := s.Send(ctx, SendRequest{AccountID: a.ID, To: []Address{{Email: "li@example.com"}}, Subject: "x", Text: "y"}); err == nil ||
		!strings.Contains(err.Error(), "经过身份验证的 SMTP") {
		t.Fatalf("SMTP AUTH off: %v", err)
	}
}
