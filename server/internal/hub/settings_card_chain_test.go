package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/extmgr"
	"github.com/sunfayao/persona/server/internal/mail"
	"github.com/sunfayao/persona/server/internal/proto"
)

type chainTestMail struct {
	extmgr.Mail
	added atomic.Int32
}

func (f *chainTestMail) Handler() http.Handler                     { return http.NotFoundHandler() }
func (f *chainTestMail) Tools() []proto.Tool                       { return nil }
func (f *chainTestMail) ListAccounts() ([]mail.MailAccount, error) { return nil, nil }
func (f *chainTestMail) AddAccount(req mail.AddAccountRequest) (*mail.MailAccount, error) {
	f.added.Add(1)
	return &mail.MailAccount{ID: "acc_1", Email: req.Email, Provider: req.Provider}, nil
}

// The manager's side of #5907 with nothing played: the real manager (assistant.Service with its interaction queue,
// extmgr.Manager with its settings, wired as assistant/main.go wires them) on the hub's real bus. A secret-input
// persona_settings call (mail.accounts add) goes to the hub (app.call), which hands it to the manager's extensions
// (extensions.hub_call), which put its password card in the assistant's queue. When the tool call stops, the card
// goes with it, and approving it afterwards changes nothing.
func TestManagerSettingCardGoesWithItsToolCall(t *testing.T) {
	h := newTestHub(t)
	h.bus.idents = &memIdents{}
	registerMachine(h, newMachine(h, MachineConfig{ID: "m1", Name: "m1"}))
	srv := httptest.NewServer(h.routes())
	t.Cleanup(srv.Close)

	store := &computerAppsSettings{values: map[string]json.RawMessage{}}
	host := &busHost{}
	fm := &chainTestMail{}
	var svc atomic.Pointer[assistant.Service]
	ext := extmgr.New(extmgr.Options{Dir: t.TempDir(), Config: &extmgr.Config{}, Store: store, Hub: host, Mail: fm,
		Ask: func(ctx context.Context, ix proto.Interaction) (proto.Resolution, error) {
			return svc.Load().AskCard(ctx, ix)
		}})
	run, stop := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- ext.Run(run) }()
	t.Cleanup(func() {
		stop()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Error("the manager's extensions did not stop")
		}
	})
	select {
	case <-ext.Started():
	case <-time.After(10 * time.Second):
		t.Fatal("the manager's extensions did not start")
	}
	a := dialAssistant(t, h, srv.URL, false, func(o *assistant.BusOptions) { o.Extensions = ext })
	svc.Store(a.svc)
	host.bh.Store(a.bh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	told := make(chan proto.ToolResult, 1)
	go func() {
		told <- a.svc.Tool(ctx, assistantwire.ToolCall{Name: "persona_settings", MachineID: "m1", SessionID: "s1",
			Arguments: json.RawMessage(`{"action":"add","key":"mail.accounts","value":{"email":"me@example.com"}}`)})
	}()
	eventually(t, 10*time.Second, "the card of the change, from the manager's queue", func() bool { return len(h.pendingInteractions()) == 1 })
	card := h.pendingInteractions()[0]
	if good := card.ExpiresAt.Sub(card.CreatedAt).Round(time.Second); card.ToolName != "persona_settings" || good != 15*time.Minute {
		t.Fatalf("the card is %s's, good for %s", card.ToolName, good)
	}

	cancel() // the assistant's tool call stops: its caller went away
	select {
	case res := <-told:
		if !res.IsError {
			t.Errorf("the stopped tool call was told %q", textOf(res))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the stopped tool call did not return")
	}
	withdrawn := func() bool { // not eventually: when it stays, the approval below shows what it then does
		limit := time.After(5 * time.Second)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for len(h.pendingInteractions()) > 0 {
			select {
			case <-tick.C:
			case <-limit:
				return false
			}
		}
		return true
	}
	if !withdrawn() {
		t.Error("the tool call stopped, and the card of its change is still on the phone")
	}
	yes := true
	if err := h.resolve(card.ID, proto.Resolution{Allow: &yes, Values: map[string]string{"password": "pw"}}); err == nil {
		t.Error("the card was still there to approve after the call that asked for the change had ended")
	}
	if fm.added.Load() != 0 {
		t.Errorf("mail account was added after the assistant's call had ended")
	}
}
