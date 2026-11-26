package extmgr

import (
	"context"

	"github.com/nwverse/persona/server/internal/i18n"
)

// contextLang is the language of what ctx is doing for people: its request's (i18n.With), else the user's.
func (h *Manager) contextLang(ctx context.Context) i18n.Lang {
	if l := i18n.From(ctx); i18n.Valid(l) {
		return l
	}
	if h == nil {
		return i18n.Default
	}
	return h.userLang()
}

// kindErr is a localized message that is also kind (errors.Is), for a sentinel error with detail of its own.
type kindErr struct {
	msg  *i18n.Message
	kind error
}

func (e *kindErr) Error() string   { return e.msg.Error() }
func (e *kindErr) Unwrap() []error { return []error{e.msg, e.kind} }

// localizedKind is key's text as an error that is kind.
func localizedKind(kind error, key string, args i18n.Args) error {
	return &kindErr{msg: &i18n.Message{Key: key, Args: args}, kind: kind}
}
