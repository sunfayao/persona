package assistant

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/identity"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Spaces: the assistant's conversations of a track run in a directory of their own next to
// the assistant's, so Claude Code's auto memory and the instructions are the space's own. Its conversations are listed
// apart (GET /api/v1/assistant?space=<slug>), and the conversation tools of one space see only that space's
// conversations.

// spaceCwd is the directory of space ("" the assistant's own) on d: a track's whose template is space (spaceTrack,
// #13472); "" for a space that has none here, so nothing starts in the assistant's directory in its stead.
func (s *Service) spaceCwd(ctx context.Context, d Device, space string) string {
	if space == "" {
		return s.assistantCwd(d)
	}
	if e, ok := s.spaceTrack(ctx, space); ok {
		return stateDirOn(d.Home, d.DataDir, e.DirName)
	}
	return ""
}

// spaceOf is the space of an assistant conversation: its own Space when the hub set it, else its directory's.
func spaceOf(c proto.Session) string {
	if c.Space != "" {
		return c.Space
	}
	return identity.SpaceOf(c.Cwd)
}

// runSpace is the space session c works in for hiding and confirming: its own Space or its directory's, a directory
// inside a space counting as the space (identity.SpaceAt: this only hides, so a subfolder counts).
func runSpace(c proto.Session) string {
	if sp := spaceOf(c); sp != "" {
		return sp
	}
	return identity.SpaceAt(c.Cwd)
}

// What leaves a conversation (#13246): the user may share what
// is sensitive with anyone they like (a doctor, a coach, family), so nothing is refused for being sensitive. What must
// not happen is it leaving without them knowing: a push or a channel says only that there is news, and a hand-off is
// confirmed on the phone each time. A text on its way out is judged by its own categories and its track's default
// categories (exitPolicy), never by what its conversation said before; a track sensitive by default asks no model and
// keeps every switch on.

// selfPolicy is the policy of the space conversation self runs in (the assistant's own, all off, for one that cannot
// be told: as before, the tools that reach other conversations then reach the assistant's only).
func (s *Service) selfPolicy(ctx context.Context, self string) TrackPolicy {
	space, _ := s.selfSpace(ctx, self)
	return s.trackPolicy(ctx, space)
}

// todoPolicy is the policy of the space a task was opened from (its origin conversation's); all off for none.
func (s *Service) todoPolicy(ctx context.Context, t Todo) TrackPolicy {
	if t.Origin == "" {
		return TrackPolicy{}
	}
	return s.selfPolicy(ctx, t.Origin)
}

// ixSpace is the space interaction ix belongs to: its directory's (a directory inside a space counts,
// identity.SpaceAt: this only hides, so a `cd` that left the hook in a subfolder must not show the card, #2368), else
// its conversation's space. spaceOf, which classifies, stays exact.
func (s *Service) ixSpace(ctx context.Context, ix proto.Interaction) string {
	if ix.Cwd != "" {
		return identity.SpaceAt(ix.Cwd)
	}
	if ix.SessionID != "" && ix.MachineID == s.settings(ctx).MachineID {
		space, _ := s.selfSpace(ctx, ix.SessionID)
		return space
	}
	return ""
}

// ixPolicy is the policy of the space interaction ix belongs to (ixSpace).
func (s *Service) ixPolicy(ctx context.Context, ix proto.Interaction) TrackPolicy {
	return s.trackPolicy(ctx, s.ixSpace(ctx, ix))
}

// redactedNotice is what a push or a channel says for something redacted from track space: only that there is news,
// not even which track.
func (s *Service) redactedNotice(ctx context.Context, l i18n.Lang, space string) (title, body string) {
	return i18n.T(l, "assistant-spaces.notice_title"), i18n.T(l, "assistant-spaces.notice_body")
}

// confirmOutbound asks the phone before conversation self hands text (said as summary) to somewhere outside it
// (tool: another agent, a new one): true when the user allows it, at once when text needs no confirming (exitPolicy:
// its own categories and its track's default categories).
func (s *Service) confirmOutbound(ctx context.Context, machine, self, tool, text, summary string, input json.RawMessage) (bool, error) {
	space, _ := s.selfSpace(ctx, self)
	if _, ok := ctx.Value(ownerConfirmedKey{}).(bool); ok {
		text = "" // the owner just confirmed these very words: only the track decides, as before tagging
	}
	if !s.exitPolicy(ctx, text, self, space).ConfirmOutbound {
		return true, nil
	}
	l := s.lang(ctx)
	now := time.Now().UTC()
	ix := proto.Interaction{ID: "ix_" + randomHex(6), MachineID: machine, SessionID: self, Kind: proto.IxPermission, ToolName: tool,
		Summary: s.outboundSummary(ctx, l, space, summary), ToolInput: input, SessionTitle: "Persona",
		CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute)}
	if c, ok := s.session(ctx, machine, self); ok {
		ix.Cwd = c.Cwd
	}
	r, err := s.ask(ctx, ix)
	if err != nil {
		return false, err
	}
	return r.Allow != nil && *r.Allow, nil
}

// ownerConfirmedKey marks a tool call carrying words the owner confirmed on the phone just before (an Inbox
// suggestion's instruction): confirmOutbound does not ask about them again by what they say.
type ownerConfirmedKey struct{}

// ownerConfirmed is ctx for a tool call whose words the owner just confirmed.
func ownerConfirmed(ctx context.Context) context.Context {
	return context.WithValue(ctx, ownerConfirmedKey{}, true)
}

// outboundSummary is what the phone is asked before a hand-off of what (said as summary) from a track.
func (s *Service) outboundSummary(ctx context.Context, l i18n.Lang, space, what string) string {
	return i18n.T(l, "assistant-spaces.outbound", i18n.Args{"what": what})
}
