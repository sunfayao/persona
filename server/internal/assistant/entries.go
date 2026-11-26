package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/extwire"
	"github.com/sunfayao/persona/server/internal/nodeclient"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

type entriesStore interface {
	AppendAssistantEntry(context.Context, proto.AssistantEntry) (proto.AssistantEntry, error)
	ListAssistantEntries(context.Context, proto.AssistantEntriesParams, bool) (proto.AssistantEntriesResult, error)
	AssistantEntry(context.Context, string) (proto.AssistantEntry, error)
}

func (s *Service) entriesDB() entriesStore {
	if s.store == nil {
		return nil
	}
	st, _ := s.store().(entriesStore)
	return st
}

// localEntriesReader proves physical co-location through the same-user node
// socket. An old/offline node or an unknown owner machine never grants access.
func (s *Service) localEntriesReader(ctx context.Context) bool {
	owner := s.settings(ctx).MachineID
	if owner == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	dial := s.entriesDial
	if dial == nil {
		dial = nodeclient.DialInterruptible
	}
	raw, err := dial(ctx, nodeclient.Request{Kind: "ping"})
	var pong struct {
		Machine string `json:"machine_id"`
		OK      bool   `json:"ok"`
	}
	return err == nil && json.Unmarshal(raw, &pong) == nil && pong.OK && pong.Machine == owner
}

func (s *Service) listEntries(ctx context.Context, p proto.AssistantEntriesParams, personal bool) (proto.AssistantEntriesResult, error) {
	if err := db.ValidateAssistantEntriesParams(&p); err != nil {
		return proto.AssistantEntriesResult{}, conversationsInvalid(assistantwire.MethodAssistantEntriesList, err.Error())
	}
	st := s.entriesDB()
	if st == nil {
		return proto.AssistantEntriesResult{}, errors.New("entries require the manager database")
	}
	return st.ListAssistantEntries(ctx, p, personal)
}

func (b *BusHub) EntriesEvent(event string, params any) error { return b.notifyHub(event, params) }

// The hub supplies the authenticated extension ID; this live registry lookup is
// deliberately separate from the token's initial scope snapshot.
type entriesAuthorizer interface{ EntriesReadAccess(string) (bool, bool) }

func (s *Service) entriesReadAccess(id string) (bool, bool) {
	if id == "" {
		return false, false
	}
	if a, ok := s.hub.(entriesAuthorizer); ok {
		return a.EntriesReadAccess(id)
	}
	return false, false
}
func (b *BusHub) EntriesReadAccess(id string) (bool, bool) {
	if a, ok := b.opts.Extensions.(entriesAuthorizer); ok {
		return a.EntriesReadAccess(id)
	}
	return false, false
}
func (s *Service) extensionEntriesPersonal(ctx context.Context) (bool, error) {
	id := assistantwire.EntriesReader(ctx)
	if id == "" {
		return s.localEntriesReader(ctx), nil
	}
	allowed, personal := s.entriesReadAccess(id)
	if !allowed {
		data, _ := json.Marshal(map[string]string{"scope": extwire.ScopeAssistantEntriesRead})
		return false, &rpc.Error{Code: "permission_denied", Message: "assistant.entries.list requires assistant.entries.read", Data: data}
	}
	return personal || s.localEntriesReader(ctx), nil
}
