package mail

import (
	"context"
	"strings"

	"github.com/emersion/go-imap/v2"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
	snippetRunes     = 160
	// snippetBytes is how much of the message body a list fetch asks for.
	// It is a BODY.PEEK partial section, so the rest never leaves the server.
	snippetBytes = 2048
)

// accountLock serializes IMAP use of one account: connections are stateful
// (SELECT), so commands must not interleave. Unlike a sync.Mutex it is taken
// with a ctx: a caller whose ctx ends while it waits leaves the queue with
// ctx.Err() instead of taking its turn only to find out. Before (#2344) such
// a caller held everyone up for nothing, and on its turn dropped the cached
// connection on the way out, so the next live caller had to reconnect.
type accountLock chan struct{}

// acquire takes the lock, or returns ctx's error once ctx ends first. A ctx
// that is already over never takes the lock, even a free one.
func (l accountLock) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case l <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release gives the lock back; the caller must hold it.
func (l accountLock) release() { <-l }

// accountLock returns the lock serializing IMAP use of one account.
func (s *Service) accountLock(id string) accountLock {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opMu == nil {
		s.opMu = make(map[string]accountLock)
	}
	l, ok := s.opMu[id]
	if !ok {
		l = make(accountLock, 1)
		s.opMu[id] = l
	}
	return l
}

// listOptions asks LIST only for the return options the server can take. RETURN
// needs LIST-EXTENDED (implied by IMAP4rev2): QQ Mail advertises none of these
// and never answers a LIST that carries RETURN, so such servers get a plain LIST
// and roles come from mailbox names, counters from one STATUS per mailbox.
// SPECIAL-USE is still asked of IMAP4rev2 servers, as it always was. status may
// be nil.
func listOptions(caps imap.CapSet, status *imap.StatusOptions) *imap.ListOptions {
	opts := &imap.ListOptions{}
	if !caps.Has(imap.CapListExtended) {
		return opts
	}
	opts.ReturnSpecialUse = caps.Has(imap.CapSpecialUse) || caps.Has(imap.CapIMAP4rev2)
	if status != nil && caps.Has(imap.CapListStatus) {
		opts.ReturnStatus = status
	}
	return opts
}

// roleOf maps a mailbox name and its SPECIAL-USE attributes to a role.
func roleOf(name string, attrs []imap.MailboxAttr) string {
	if strings.EqualFold(name, "INBOX") {
		return RoleInbox
	}
	for _, attr := range attrs {
		switch attr {
		case imap.MailboxAttrSent:
			return RoleSent
		case imap.MailboxAttrDrafts:
			return RoleDrafts
		case imap.MailboxAttrTrash:
			return RoleTrash
		case imap.MailboxAttrArchive:
			return RoleArchive
		case imap.MailboxAttrJunk:
			return RoleJunk
		case imap.MailboxAttrAll:
			// Gmail's "All Mail": not one of the roles the protocol defines.
			continue
		}
	}
	// Providers that do not set SPECIAL-USE: guess from the name.
	switch strings.ToLower(name) {
	case "sent", "sent items", "sent messages", "sent mail":
		return RoleSent
	case "drafts", "draft":
		return RoleDrafts
	case "trash", "deleted items", "deleted messages", "bin":
		return RoleTrash
	case "archive", "archives":
		return RoleArchive
	case "junk", "spam", "junk e-mail", "bulk mail":
		return RoleJunk
	}
	return ""
}

func hasAttr(attrs []imap.MailboxAttr, want imap.MailboxAttr) bool {
	for _, a := range attrs {
		if a == want {
			return true
		}
	}
	return false
}
