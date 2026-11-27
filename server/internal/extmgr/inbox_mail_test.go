package extmgr

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/connector"
	"github.com/sunfayao/persona/server/internal/mail"
	"github.com/sunfayao/persona/server/internal/proto"
)

// slowMail answers each account's inbox page after a fixed delay, the way a slow IMAP server does; an account in
// fail can't be read. With release set, a read waits for it to close instead of delay. It records how many reads
// ran at once.
type slowMail struct {
	inboxOnlyMail
	accounts []mail.MailAccount
	delay    time.Duration
	release  chan struct{}
	fail     map[string]bool
	running  atomic.Int32
	peak     atomic.Int32
}

func (*slowMail) Tools() []proto.Tool { return nil } // registering the mail connector lists them

func (m *slowMail) ListAccounts() ([]mail.MailAccount, error) { return m.accounts, nil }

func (m *slowMail) ListMessages(ctx context.Context, opts mail.ListOptions) (*mail.ListResult, error) {
	n := m.running.Add(1)
	defer m.running.Add(-1)
	for {
		p := m.peak.Load()
		if n <= p || m.peak.CompareAndSwap(p, n) {
			break
		}
	}
	var elapsed <-chan time.Time // stays nil, never ready, while release holds the read
	if m.release == nil {
		elapsed = time.After(m.delay)
	}
	select {
	case <-elapsed:
	case <-m.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if m.fail[opts.AccountID] {
		return nil, errors.New("imap: operation timed out")
	}
	return &mail.ListResult{Messages: []mail.MailSummary{{Mailbox: "INBOX", UID: 1, Subject: "from " + opts.AccountID}}}, nil
}

func newSlowMailHub(t *testing.T, m *slowMail) *Manager {
	t.Helper()
	h := newTestManager(t)
	h.connectors = connector.NewRegistry(nil)
	h.mail = m
	if err := h.addConnector(mailConnector{h: h}); err != nil {
		t.Fatal(err)
	}
	return h
}

// More accounts than run at once still all get read, never more than inboxMailParallel of them together.
func TestReadMailBoundsParallelReads(t *testing.T) {
	m := &slowMail{release: make(chan struct{})}
	for i := 0; i < 3*inboxMailParallel; i++ {
		id := string(rune('a' + i))
		m.accounts = append(m.accounts, mail.MailAccount{ID: id, Email: id + "@example.com"})
	}
	h := newSlowMailHub(t, m)

	done := make(chan *assistantwire.InboxMailRead, 1)
	go func() { done <- h.readMail(context.Background()) }()

	// Held reads pile up to the bound however late their goroutines start; then no more may begin.
	deadline := time.Now().Add(10 * time.Second)
	for m.running.Load() < inboxMailParallel {
		if time.Now().After(deadline) {
			close(m.release)
			t.Fatalf("only %d accounts read at once, want %d", m.running.Load(), inboxMailParallel)
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if n := m.running.Load(); n != inboxMailParallel {
		close(m.release)
		t.Fatalf("%d accounts read at once, want %d", n, inboxMailParallel)
	}
	close(m.release)

	r := <-done
	if r == nil || len(r.Accounts) != len(m.accounts) {
		t.Fatalf("read %v, want %d accounts", r, len(m.accounts))
	}
	for i, a := range r.Accounts {
		if a.ID != m.accounts[i].ID {
			t.Fatalf("account %d is %s, want %s", i, a.ID, m.accounts[i].ID)
		}
	}
	if p := m.peak.Load(); p != inboxMailParallel {
		t.Fatalf("%d accounts read at once, want %d", p, inboxMailParallel)
	}
}

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}
