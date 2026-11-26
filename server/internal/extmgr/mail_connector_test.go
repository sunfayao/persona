package extmgr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sunfayao/persona/server/internal/mail"
)

// pagedMail is one account's inbox, paged by before_uid like the IMAP listing: newest first, NextBeforeUID the
// lowest UID of the page whenever there is more.
type pagedMail struct {
	inboxOnlyMail
	validity uint32
	msgs     []mail.MailSummary // ascending UID
	failOld  bool               // pages below the newest fail, as a slow server's would time out
}

var mailTestBase = time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

func (m *pagedMail) add(from, to uint32) {
	for uid := from; uid <= to; uid++ {
		m.msgs = append(m.msgs, mail.MailSummary{AccountID: "acc", Mailbox: "INBOX", UID: uid, Subject: fmt.Sprint("mail ", uid),
			From: mail.Address{Email: "sender@example.com"}, Date: mailTestBase.Add(time.Duration(uid) * time.Minute).Format(time.RFC3339),
			Snippet: "snippet"})
	}
}

func (m *pagedMail) ListAccounts() ([]mail.MailAccount, error) {
	return []mail.MailAccount{{ID: "acc", Email: "me@example.com"}}, nil
}

func (m *pagedMail) ListMessages(_ context.Context, opts mail.ListOptions) (*mail.ListResult, error) {
	if m.failOld && opts.BeforeUID != 0 {
		return nil, errors.New("i/o timeout")
	}
	var below []mail.MailSummary
	for _, s := range m.msgs {
		if opts.BeforeUID == 0 || s.UID < opts.BeforeUID {
			below = append(below, s)
		}
	}
	res := &mail.ListResult{UIDValidity: m.validity}
	if len(below) > opts.Limit {
		below = below[len(below)-opts.Limit:]
		res.HasMore, res.NextBeforeUID = true, below[0].UID
	}
	for i := len(below) - 1; i >= 0; i-- {
		res.Messages = append(res.Messages, below[i])
	}
	return res, nil
}

func (m *pagedMail) GetMessage(context.Context, string, uint32, string) (*mail.MailMessage, error) {
	return nil, errors.New("not in this test")
}
