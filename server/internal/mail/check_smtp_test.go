package mail

import (
	"context"
	"net"
	"strconv"
	"testing"
)

// smtpAccount points the test IMAP account's SMTP side at addr with the given security.
func smtpAccount(t *testing.T, srv *testIMAP, addr, security string) *MailAccount {
	t.Helper()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	a := srv.testAccount()
	a.SMTPHost, a.SMTPPort, a.SMTPSecurity = host, port, security
	return a
}

// A server that accepted the login and then hung up on QUIT has a working account: adding it must not fail
// because of the goodbye (#4616).
func TestCheckSMTPSucceedsWhenQuitFailsAfterLogin(t *testing.T) {
	srv := newTestIMAP(t)
	s := newServiceWithAccount(t, srv)
	addr, _ := hangUpSMTP(t)
	a := smtpAccount(t, srv, addr, "none")
	if err := s.checkSMTP(context.Background(), a); err != nil {
		t.Fatalf("checkSMTP = %v, want success: the login was accepted", err)
	}
}
