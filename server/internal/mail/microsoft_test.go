package mail

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/emersion/go-smtp"
)

const testMSClient = "11111111-2222-3333-4444-555555555555"

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
