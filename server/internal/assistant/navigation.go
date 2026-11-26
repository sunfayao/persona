package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/lazyre"
)

// Overview layout is shared across the owner's devices and kept in the settings table.
const navigationKey = "navigation"

// navigation preserves the old layout fields when saving Overview.
type navigation struct {
	Start    json.RawMessage  `json:"start,omitempty"`
	Pages    json.RawMessage  `json:"pages,omitempty"`
	Overview *[]overviewPlace `json:"overview,omitempty"`
}

type navigationView struct {
	Overview *[]overviewPlace `json:"overview,omitempty"`
}

// overviewPlace is one of Overview's cards (an extension's overview_card, docs/extensions/extensions.md "UI slots") as
// the user placed it: the extension's id and the card's, whether it is hidden, and its width: half a row ("" or "half",
// the default) or the whole row ("full"). Or one of Overview's own sections (#10905): its id in Section, whether it
// is hidden, and nothing else. Which cards and sections there are is the apps' business: one the list names that is
// not there now is passed over, and one it does not name keeps its place by default (web/src/lib/overview-cards.ts).
type overviewPlace struct {
	Extension string `json:"extension,omitempty"`
	ID        string `json:"id,omitempty"`
	Section   string `json:"section,omitempty"`
	Hidden    bool   `json:"hidden,omitempty"`
	Span      string `json:"span,omitempty"`
}

const overviewCardsMost = 256

// overviewSection is a section's id: the web's (needs_you, in_progress, …) and, so a newer web's is kept, any of that shape.
var overviewSection = lazyre.New(`^[a-z][a-z0-9_]{0,31}$`)

// checkOverview says what in cards is not of the shape the apps write: a card's ids as an extension's and a UI
// contribution's (extension.ValidID) and a width of "half" or "full"; a section's id of its shape and no card's fields;
// each card and section once.
func checkOverview(cards []overviewPlace) error {
	if len(cards) > overviewCardsMost {
		return fmt.Errorf("overview: at most %d cards and sections", overviewCardsMost)
	}
	seen := make(map[string]bool, len(cards))
	for _, c := range cards {
		k := c.Extension + "/" + c.ID
		switch {
		case c.Section != "":
			if !overviewSection.MatchString(c.Section) || c.Extension != "" || c.ID != "" || c.Span != "" {
				return fmt.Errorf("overview: %q is not one of Overview's sections", c.Section)
			}
			k = c.Section // a card's key has a "/"
		case !extension.ValidID(c.Extension) || !extension.ValidID(c.ID):
			return fmt.Errorf("overview: %q / %q is not an extension's card", c.Extension, c.ID)
		case c.Span != "" && c.Span != "half" && c.Span != "full":
			return fmt.Errorf("overview: a card's span is half or full, not %q", c.Span)
		}
		if seen[k] {
			return fmt.Errorf("overview: %s more than once", k)
		}
		seen[k] = true
	}
	return nil
}

// keepSections is what a change to list leaves of Overview: list, unless it names cards and no section while the stored
// one names sections. Such a list comes from an app that does not know the sections (#9745's web): the sections stay
// where they are, the cards take the stored cards' places in list's order, and one the stored list did not have goes
// after the last of them (none: where the cards are by default, last, under the sections, #11147). An empty list
// (Reset) is taken as it is, as is one that would hold more than the most there may be.
func keepSections(stored, list []overviewPlace) []overviewPlace {
	if len(list) == 0 || slices.ContainsFunc(list, func(c overviewPlace) bool { return c.Section != "" }) ||
		!slices.ContainsFunc(stored, func(c overviewPlace) bool { return c.Section != "" }) {
		return list
	}
	out := make([]overviewPlace, 0, len(stored)+len(list))
	next, after := 0, -1
	for _, c := range stored {
		if c.Section == "" {
			if next == len(list) {
				continue
			}
			c, next = list[next], next+1
			after = len(out) + 1
		}
		out = append(out, c)
	}
	if after < 0 {
		after = len(out)
	}
	out = slices.Insert(out, after, list[next:]...)
	if len(out) > overviewCardsMost {
		return list
	}
	return out
}

func (s *Service) navigationRoutes() {
	s.handle("GET /api/v1/navigation", s.getNavigation)
	s.handle("PUT /api/v1/navigation", s.putNavigation)
}

func (s *Service) getNavigation(w http.ResponseWriter, r *http.Request) {
	var n navigation
	if st := s.store(); st != nil {
		if _, err := st.Setting(r.Context(), navigationKey, &n); err != nil {
			apierr.Write(w, http.StatusServiceUnavailable, err)
			return
		}
	}
	apierr.WriteJSON(w, navigationView{Overview: n.Overview})
}

func (s *Service) putNavigation(w http.ResponseWriter, r *http.Request) {
	if !fullRole(r) {
		apierr.Write(w, http.StatusForbidden, errors.New("only a device with full access may change Overview"))
		return
	}
	var body navigationView
	if err := decodeBody(w, r, &body, 64<<10); err != nil || body.Overview == nil {
		writeBodyErr(w, err, errors.New("overview is required"))
		return
	}
	if err := checkOverview(*body.Overview); err != nil {
		apierr.Write(w, http.StatusBadRequest, err)
		return
	}
	n, err := s.saveNavigation(r.Context(), *body.Overview)
	if err != nil {
		apierr.Write(w, http.StatusServiceUnavailable, err)
		return
	}
	apierr.WriteJSON(w, n)
}

func (s *Service) saveNavigation(ctx context.Context, cards []overviewPlace) (navigationView, error) {
	st := s.store()
	if st == nil {
		return navigationView{}, errors.New("Overview needs a database")
	}
	s.navMu.Lock()
	defer s.navMu.Unlock()
	var n navigation
	if _, err := st.Setting(ctx, navigationKey, &n); err != nil {
		return navigationView{}, err
	}
	var stored []overviewPlace
	if n.Overview != nil {
		stored = *n.Overview
	}
	kept := keepSections(stored, cards)
	n.Overview = &kept
	if err := st.PutSetting(ctx, navigationKey, n); err != nil {
		return navigationView{}, err
	}
	s.event(ctx, map[string]any{"type": "navigation", "overview": kept})
	return navigationView{Overview: n.Overview}, nil
}
