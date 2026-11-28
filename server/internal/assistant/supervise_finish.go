package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/assistant/quietturn"
)

// newQuietTurns makes the book of supervision batches and push holds, kept in the settings store (quietturn).
func (s *Service) newQuietTurns() *quietturn.Book {
	return quietturn.New(quietturn.Dependencies{Max: superviseHoldMax, Logf: s.logf, Store: func() quietturn.Store {
		if st := s.store(); st != nil {
			return st
		}
		return nil
	}})
}

// promoteSupervision is monotone within a wake-up: a later silent finish cannot hide a user action.
func (s *Service) promoteSupervision(self string) {
	s.turns.Promote(context.Background(), s.supervisionHoldKey(context.Background(), self))
}

// toolSupervisionFinish records the caller's disposition in its persisted MCP result.
// The transcript remains intact; clients fold the entire triage turn, not just its input.
func (s *Service) toolSupervisionFinish(ctx context.Context, self string, args json.RawMessage) (string, error) {
	var a struct {
		Visibility string `json:"visibility"`
		Summary    string `json:"summary"`
		Batch      string `json:"batch"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", err
	}
	a.Summary = strings.TrimSpace(a.Summary)
	if (a.Visibility != "silent" && a.Visibility != "user") || a.Summary == "" || utf8.RuneCountInString(a.Summary) > 1000 {
		return "", errors.New("visibility must be silent or user, with a summary of 1–1000 characters")
	}
	r, err := s.turns.Finish(ctx, s.supervisionHoldKey(ctx, self), strings.TrimSpace(a.Batch), a.Visibility)
	if err != nil {
		return "", err
	}
	out := map[string]string{"visibility": a.Visibility, "summary": a.Summary, "level": r.Level}
	if r.Level == "user" {
		out["visibility"] = "user" // clients fold a turn only on a silent receipt: this one stays visible
	}
	if r.Note != "" {
		out["note"] = r.Note
	}
	body, err := json.Marshal(out)
	return string(body), err
}
