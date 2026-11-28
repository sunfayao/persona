package assistant

import (
	"context"
	"strings"
)

// acceptGuardEvidence is the text a bare-accept guard scans: what the supervisor put in the review and, best effort,
// what its agents said last. Agent text is added only when the hub can read it right now; an offline node never blocks
// an accept by itself.
func (s *Service) acceptGuardEvidence(ctx context.Context, t Todo, note, reason string, checks []ReviewCheck) string {
	var b strings.Builder
	for _, part := range []string{note, reason} {
		if part = strings.TrimSpace(part); part != "" {
			b.WriteString(part)
			b.WriteByte('\n')
		}
	}
	for _, c := range checks { // never c.Criterion: the wording of a criterion says nothing about this result
		b.WriteString(c.Evidence)
		b.WriteByte('\n')
	}
	if replies, sideBlocked := s.taskReplies(ctx, t); !sideBlocked {
		for _, r := range replies {
			b.WriteString(r.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
