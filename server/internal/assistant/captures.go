package assistant

import (
	"context"

	"github.com/sunfayao/persona/server/internal/db"
)

// Quick capture (#13502): what the user jots down from the share sheet, the
// watch, Siri or the web goes into a list of its own. Taking one in is a single write, answered at once: no
// conversation gets a message or a turn. The work loop (capture_sort.go) sorts it into a track and tags it later, in
// one model call, and files the ones it is sure of into that track's inbox/.

// captureStore keeps the captures (assistant_captures).
type captureStore interface {
	Captures(ctx context.Context, f db.CaptureFilter) ([]db.Capture, error)
	CapturesToSort(ctx context.Context, maxAttempts, limit int) ([]db.Capture, error)
	SetCaptureResult(ctx context.Context, id, status string, sort, filedTo []byte, errText string, tried bool) (bool, error)
}

var _ captureStore = (*db.DB)(nil)

// Limits of what one capture holds.
const (
	captureListMax = 200
)

// captureTarget is track slug when a capture may be filed into it: a track with a directory of its own here
// (spaceTrack: never the health space, nor an archived one) whose config takes captures.
func (s *Service) captureTarget(ctx context.Context, slug string) (trackEntry, bool) {
	e, ok := s.spaceTrack(ctx, slug)
	if !ok || !e.Config.CaptureTarget {
		return trackEntry{}, false
	}
	return e, true
}

// captureTargets are the tracks a capture may be filed into, by slug.
func (s *Service) captureTargets(ctx context.Context) []trackEntry {
	var out []trackEntry
	for _, slug := range s.spaceTracks(ctx) {
		if e, ok := s.captureTarget(ctx, slug); ok {
			out = append(out, e)
		}
	}
	return out
}

// captureCats are a capture's information categories and whether it was tagged at all.
type captureCats struct {
	cats   []string
	tagged bool
}

// sensitive: outside the capture list it is used as sensitive information: it carries a category, or it was not
// tagged (yet, or the tagging failed).
func (c captureCats) sensitive() bool { return len(c.cats) > 0 || !c.tagged }

// captureCategories are the categories of captures rows, by id (captureCats{} for one never tagged or unreadable).
func (s *Service) captureCategories(ctx context.Context, rows []db.Capture) map[string]captureCats {
	out := map[string]captureCats{}
	st, ok := s.store().(infoCategoryStore)
	if !ok || len(rows) == 0 {
		return out
	}
	ids := make([]string, 0, len(rows))
	for _, c := range rows {
		ids = append(ids, c.ID)
	}
	rctx, cancel := context.WithTimeout(ctx, infoCategoryStoreTimeout)
	defer cancel()
	tags, err := st.InfoCategories(rctx, db.InfoCategoryCapture, ids)
	if err != nil {
		s.logf("captures: read categories: %v", err)
		return out
	}
	for id, list := range tags {
		var c captureCats
		for _, t := range list {
			c.tagged = true
			if !t.RemovedByUser {
				c.cats = append(c.cats, t.Category)
			}
		}
		c.cats, _ = normalCategories(c.cats)
		out[id] = c
	}
	return out
}
