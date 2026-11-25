package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ownerActionTestStore has the same revision-checked replacement semantics as
// db.CompareSetting. The mutex covers the read/compare/write sequence, so two
// independent books exercise cross-manager arbitration rather than a book lock.
type ownerActionTestStore struct {
	noPlans
	mu      sync.Mutex
	values  map[string][]byte
	casErr  error
	casCall atomic.Int32
}

func (st *ownerActionTestStore) Setting(_ context.Context, key string, out any) (bool, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	raw, ok := st.values[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (st *ownerActionTestStore) PutSetting(_ context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.values == nil {
		st.values = map[string][]byte{}
	}
	st.values[key] = raw
	return nil
}

func (st *ownerActionTestStore) CompareSetting(_ context.Context, key string, revision int64, value any) (bool, error) {
	st.casCall.Add(1)
	raw, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.casErr != nil {
		return false, st.casErr
	}
	var current struct {
		Revision int64 `json:"revision"`
	}
	if len(st.values[key]) > 0 {
		if err := json.Unmarshal(st.values[key], &current); err != nil {
			return false, err
		}
	}
	if current.Revision != revision {
		return false, nil
	}
	if st.values == nil {
		st.values = map[string][]byte{}
	}
	st.values[key] = raw
	return true, nil
}

func newOwnerActionTestBook(t *testing.T) (*ownerActionBook, *ownerActionTestStore) {
	t.Helper()
	st := &ownerActionTestStore{}
	book, ok := newOwnerActionBook(st)
	if !ok {
		t.Fatal("a revision-aware store must support owner-action grants")
	}
	return book, st
}

func validOwnerProof(batch, item string) ownerReplyProof {
	return ownerReplyProof{
		AuthenticatedOwner: true,
		DirectParent:       true,
		ExactPhrase:        true,
		EventID:            "event-1",
		BatchID:            batch,
		ItemID:             item,
	}
}

func TestOwnerActionCanonicalPayload(t *testing.T) {
	a, err := canonicalOwnerAction(json.RawMessage(` {"subject":"hello","to":[{"email":"a@example.test"}],"n":1} `))
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalOwnerAction(json.RawMessage(`{"n":1,"to":[{"email":"a@example.test"}],"subject":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("equivalent objects must have one canonical form:\n%s\n%s", a, b)
	}
	changed, err := canonicalOwnerAction(json.RawMessage(`{"n":1,"to":[{"email":"b@example.test"}],"subject":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) == string(changed) {
		t.Fatal("a changed recipient must change the canonical payload")
	}
	if _, err := canonicalOwnerAction(json.RawMessage(`{"to":"a@example.test","to":"b@example.test"}`)); err == nil {
		t.Fatal("duplicate object keys are ambiguous and must be rejected")
	}
}











func (book *ownerActionBook) putForTest(ctx context.Context, batch ownerActionBatch) error {
	for range 10 {
		doc, err := book.read(ctx)
		if err != nil {
			return err
		}
		found := false
		for i := range doc.Batches {
			if doc.Batches[i].ID == batch.ID {
				doc.Batches[i] = batch
				found = true
				break
			}
		}
		if !found {
			return errors.New("test batch not found")
		}
		doc.Revision++
		saved, err := book.cas.CompareSetting(ctx, ownerActionBookKey, doc.Revision-1, doc)
		if err != nil {
			return err
		}
		if saved {
			return nil
		}
	}
	return errors.New("test batch CAS conflict")
}
