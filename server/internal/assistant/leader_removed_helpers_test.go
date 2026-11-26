package assistant

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
)

// planMemStore is memStore with the task plans and the feed, in memory.
type planMemStore struct {
	*memStore
	pmu   sync.Mutex
	plans map[string]db.TaskPlanRow
	feed  []db.FeedRow
}

func (m *planMemStore) CreateTaskPlan(_ context.Context, p db.TaskPlanRow) error {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	m.plans[p.ID] = p
	return nil
}

func (m *planMemStore) TaskPlan(_ context.Context, id string) (*db.TaskPlanRow, error) {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	p, ok := m.plans[id]
	if !ok {
		return nil, db.ErrTaskPlanNotFound
	}
	return &p, nil
}

func (m *planMemStore) TaskPlans(_ context.Context, conversation string) ([]db.TaskPlanRow, error) {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	var out []db.TaskPlanRow
	for _, p := range m.plans {
		if p.Conversation == conversation {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *planMemStore) TaskPlansByID(_ context.Context, ids []string) ([]db.TaskPlanRow, error) {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	var out []db.TaskPlanRow
	for _, id := range ids {
		if p, ok := m.plans[id]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *planMemStore) TaskPlansMadeSince(_ context.Context, since time.Time) ([]db.TaskPlanRow, error) {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	var out []db.TaskPlanRow
	for _, p := range m.plans {
		if p.CreatedAt.Before(since) || p.State != planStarted && p.State != planDismissed {
			continue
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b db.TaskPlanRow) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func (m *planMemStore) AppendFeed(_ context.Context, r db.FeedRow) error {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	m.feed = append(m.feed, r)
	return nil
}

func (m *planMemStore) FeedItem(_ context.Context, id string) (*db.FeedRow, error) {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	for i := len(m.feed) - 1; i >= 0; i-- {
		if m.feed[i].ID == id {
			r := m.feed[i]
			return &r, nil
		}
	}
	return nil, nil
}

func (m *planMemStore) Feed(_ context.Context, conversation string, since time.Time, limit int) ([]db.FeedRow, error) {
	m.pmu.Lock()
	defer m.pmu.Unlock()
	var out []db.FeedRow
	for _, r := range m.feed {
		if r.Conversation == conversation && r.At.After(since) && (limit <= 0 || len(out) < limit) {
			out = append(out, r)
		}
	}
	return out, nil
}

// noticesTo are the texts sent into assistant conversation id.
func (f *fakeHub) noticesTo(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, a := range f.sends {
		if a["session_id"] == id {
			text, _ := a["text"].(string)
			out = append(out, text)
		}
	}
	return out
}

func mustTodo(t *testing.T, s *Service, title string) Todo {
	t.Helper()
	td, ok := todoByTitle(s, title)
	if !ok {
		t.Fatalf("no to-do %q", title)
	}
	return td
}
