package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/db"
)

type creationLogStore struct {
	*memStore
	err error
}

func (s creationLogStore) PutNodeSession(context.Context, string, string, db.NodeSession) error {
	return s.err
}
func (s creationLogStore) NodeSessions(context.Context, string) (map[string]db.NodeSession, error) {
	return nil, nil
}

func TestShareCreationLogManagerPersistence(t *testing.T) {
	for _, mode := range []string{"receipt", "no_receipt", "failed", "no_store", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			var logs []string
			d := New(Options{Logf: func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }})
			if mode != "no_store" {
				s := creationLogStore{memStore: newMemStore()}
				if mode == "failed" {
					s.err = errors.New("token=private-error-content")
				}
				d.opts.Store = s
			}
			p := map[string]any{"session_id": "synthetic-persistence", "state": map[string]any{"creation_id": "private-creation", "incarnation": "private-incarnation", "cwd": "/private/workspace"}}
			if mode == "no_receipt" {
				p["state"] = map[string]any{}
			}
			raw, _ := json.Marshal(p)
			if mode == "invalid" {
				raw = json.RawMessage(`not-json-private-content`)
			}
			(&Machine{d: d, cfg: MachineConfig{ID: "private-machine"}}).onEvent("state.put", raw)
			want := map[string]string{"receipt": "committed_with_receipt", "no_receipt": "committed_without_receipt", "failed": "store_error", "no_store": "store_unavailable", "invalid": "invalid_state"}[mode]
			joined := strings.Join(logs, "\n")
			if !strings.Contains(joined, `"result":"`+want+`"`) {
				t.Fatal("missing durable-write outcome")
			}
			if strings.Contains(joined, "private-") || strings.Contains(joined, "/private/") {
				t.Fatal("private diagnostic data")
			}
			if mode == "failed" && strings.Contains(joined, `"result":"committed_`) {
				t.Fatal("failed write claimed committed")
			}
		})
	}
}
