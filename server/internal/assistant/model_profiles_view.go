package assistant

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// ---- comparing with the configuration ----

// profileRecords reads the records of the categories any of ps names, each once: the view compares every profile
// with them while it holds models.mu.
func (s *Service) profileRecords(ctx context.Context, ps []ModelProfile) (map[string]modelRecord, error) {
	records := map[string]modelRecord{}
	for _, id := range modelCategoryIDs {
		if !slices.ContainsFunc(ps, func(p ModelProfile) bool { _, ok := p.Purposes[id]; return ok }) {
			continue
		}
		r, err := s.modelRecord(ctx, id)
		if err != nil {
			return nil, err
		}
		records[id] = r
	}
	return records, nil
}

// modelProfileChanges is what switching to p would change now: the categories whose choice differs from their records
// (profileRecords) as ids, and the launcher fields (<launcher>.<field>). For the profile in use: where the
// configuration has moved away from it.
func modelProfileChanges(p ModelProfile, records map[string]modelRecord, stored []storedLauncher) []string {
	changes := []string{}
	for _, id := range modelCategoryIDs {
		b, ok := p.Purposes[id]
		if !ok {
			continue
		}
		r := records[id] // a copy: the new conversations' profile is cleared on it only
		r.Binding.ConfigDir = ""
		if r.Binding != b {
			changes = append(changes, id)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(p.Launchers)) {
		i := slices.IndexFunc(stored, func(l storedLauncher) bool { return l.Name == name })
		var l *proto.ManagedLauncher
		if i >= 0 {
			l = &stored[i].ManagedLauncher
		}
		m := p.Launchers[name]
		for _, f := range m.fields(l) {
			if *f.set != nil && (l == nil || *f.launcher != **f.set) { // a launcher no longer there: all of it
				changes = append(changes, name+"."+f.name)
			}
		}
	}
	return changes
}

// ---- the view ----

// ModelProfileView is a profile in the model profile tool output.
type ModelProfileView struct {
	ModelProfile
	Active bool `json:"active,omitempty"`
	// Changes: what switching to it would change now (for the one in use: where the configuration moved away from it).
	Changes []string `json:"changes"`
}

// ModelProfilesView is the model profile tool output.
type ModelProfilesView struct {
	Revision int64               `json:"revision"`
	Active   string              `json:"active,omitempty"`
	At       *time.Time          `json:"at,omitempty"`
	By       *ProfileActor       `json:"by,omitempty"`
	Rollback *ModelProfileUndo   `json:"rollback,omitempty"`
	Profiles []ModelProfileView  `json:"profiles"`
	Events   []ModelProfileEvent `json:"events"` // newest first
}

// ModelProfileUndo is what a rollback would do: back to profile To ("" none), changing Changes.
type ModelProfileUndo struct {
	To      string    `json:"to,omitempty"`
	At      time.Time `json:"at"`
	Changes []string  `json:"changes"`
}

func (s *Service) modelProfilesView(ctx context.Context) (ModelProfilesView, error) {
	s.models.mu.Lock()
	defer s.models.mu.Unlock()
	ps, seeded, err := s.modelProfilesLocked(ctx)
	if err != nil {
		return ModelProfilesView{}, err
	}
	state, err := s.readModelProfileState(ctx)
	if err != nil {
		return ModelProfilesView{}, err
	}
	if seeded {
		state.seed()
	}
	stored, err := s.readStoredLaunchers(ctx)
	if err != nil {
		return ModelProfilesView{}, err
	}
	ctx = withManagedSnapshot(ctx, stored)
	records, err := s.profileRecords(ctx, ps.Profiles)
	if err != nil {
		return ModelProfilesView{}, err
	}
	v := ModelProfilesView{Revision: ps.Revision, Active: state.Active, Profiles: []ModelProfileView{}, Events: []ModelProfileEvent{}}
	if state.Active != "" {
		at, by := state.At, state.By
		v.At, v.By = &at, &by
	}
	for _, p := range ps.Profiles {
		v.Profiles = append(v.Profiles, ModelProfileView{ModelProfile: p, Active: p.ID == state.Active, Changes: modelProfileChanges(p, records, stored)})
	}
	if prev := state.Previous; prev != nil {
		undo := &ModelProfileUndo{To: prev.Profile, At: prev.At, Changes: slices.Sorted(maps.Keys(prev.Purposes))}
		for _, name := range slices.Sorted(maps.Keys(prev.Launchers)) {
			m := prev.Launchers[name]
			for _, f := range m.fields(nil) {
				if *f.set != nil {
					undo.Changes = append(undo.Changes, name+"."+f.name)
				}
			}
		}
		v.Rollback = undo
	}
	for i := len(state.Events) - 1; i >= 0 && len(v.Events) < shownProfileEvents; i-- {
		v.Events = append(v.Events, state.Events[i])
	}
	if seeded {
		if err := s.putModelProfileSettings(ctx, map[string]any{modelProfilesKey: ps, modelProfileStateKey: state}); err != nil {
			return ModelProfilesView{}, err
		}
	}
	return v, nil
}
