package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/connector"
)

// auditRow is one plugin_audit row as auditingStore keeps it.
type auditRow struct {
	ID, Action, Actor string
	Detail            map[string]any
}

// auditingStore is keepStore that also keeps the audit (plugin_audit), or refuses it when fail is set.
type auditingStore struct {
	*keepStore
	mu   sync.Mutex
	rows []auditRow
	fail bool
}

func newAuditingStore() *auditingStore { return &auditingStore{keepStore: newKeepStore()} }

func (s *auditingStore) AddExtensionAudit(_ context.Context, id, action, actor string, detail json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("the database is away")
	}
	var d map[string]any
	if err := json.Unmarshal(detail, &d); err != nil {
		return err
	}
	s.rows = append(s.rows, auditRow{ID: id, Action: action, Actor: actor, Detail: d})
	return nil
}

// take gives the rows recorded since the last take.
func (s *auditingStore) take() []auditRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.rows
	s.rows = nil
	return out
}

// actions is rows' actions in order.
func actions(rows []auditRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Action
	}
	return out
}

func sameActions(rows []auditRow, want ...string) bool {
	got := actions(rows)
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The record names the device that asked; a record that cannot be written does not undo or refuse the action.
func TestConnectorAuditActorAndFailure(t *testing.T) {
	h := newTestManager(t)
	st := newAuditingStore()
	h.store = st
	h.connectors = connector.NewRegistry(nil)
	_ = h.addConnector(&exampleConnector{})
	ctx := withPrincipal(context.Background(), principal{ID: "dev_phone", Role: roleFull})
	if err := h.setConnectorEnabled(ctx, "example", false); err != nil {
		t.Fatal(err)
	}
	if rows := st.take(); len(rows) != 1 || rows[0].Actor != "device:dev_phone" {
		t.Fatalf("recorded %v", rows)
	}

	st.fail = true
	if err := h.setConnectorEnabled(ctx, "example", true); err != nil || !h.connectors.Enabled("example") {
		t.Fatalf("the switch failed with the audit: %v", err)
	}

	// A store without an audit (no database) records nothing and still works.
	h.store = newKeepStore()
	if err := h.setConnectorEnabled(ctx, "example", false); err != nil || h.connectors.Enabled("example") {
		t.Fatalf("without an audit: %v", err)
	}
}
