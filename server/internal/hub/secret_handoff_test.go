package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/extwire"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// handoffManager is the manager on the bus in these tests: it answers every relayed request 200 and keeps it, and
// keeps every audit the hub hands it.
type handoffManager struct {
	mu      sync.Mutex
	relayed []assistantwire.HTTPRequest
	audits  []extwire.Audit
	raw     []string // the audits' params as they came
	fail    error    // what an audit is answered, when set (nothing recorded)
	tries   int      // the audits asked
	sends   []assistantwire.SendParams
}

func (m *handoffManager) handle(_ context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case assistantwire.MethodHTTP:
		var req assistantwire.HTTPRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		m.mu.Lock()
		m.relayed = append(m.relayed, req)
		m.mu.Unlock()
		return assistantwire.HTTPResponse{Status: http.StatusOK, Header: map[string][]string{"Content-Type": {"application/json"}},
			Body: []byte(`{"ok":true}`)}, nil
	case assistantwire.MethodSendParams:
		var p assistantwire.SendParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		m.mu.Lock()
		m.sends = append(m.sends, p)
		m.mu.Unlock()
		return map[string]any{"params": p.Params}, nil
	case extwire.MethodHubCall:
		var c extwire.HubCall
		if err := json.Unmarshal(params, &c); err != nil {
			return nil, err
		}
		if c.Method == extwire.HubAudit {
			var a extwire.Audit
			if err := json.Unmarshal(c.Params, &a); err != nil {
				return nil, err
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			m.tries++
			if m.fail != nil {
				return nil, m.fail
			}
			m.audits, m.raw = append(m.audits, a), append(m.raw, string(c.Params))
		}
		return map[string]any{}, nil
	}
	return nil, &rpc.Error{Code: "no_such_thing", Message: "the test manager has no " + method}
}

// took is what the manager was handed since the last call: the relayed requests and the audits.
func (m *handoffManager) took() ([]assistantwire.HTTPRequest, []extwire.Audit, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, a, raw := m.relayed, m.audits, m.raw
	m.relayed, m.audits, m.raw = nil, nil, nil
	return r, a, raw
}

// handoffBusHub is a hub with handoffManager on the bus as the assistant.
func handoffBusHub(t *testing.T) (*Hub, *handoffManager) {
	h, m, _ := handoffBusHubWithCapabilities(t, nil)
	return h, m
}

func handoffBusHubWithCapabilities(t *testing.T, capabilities []string) (*Hub, *handoffManager, *rawBus) {
	t.Helper()
	h := newTestHub(t)
	h.store = &tokenStore{memStore: *newMemStore(), rows: map[string]db.DeviceToken{}, rev: map[string]bool{}}
	if h.busStore() == nil {
		h.bus.idents = &memIdents{}
	}
	busMux := http.NewServeMux()
	h.busRoutes(busMux)
	busSrv := httptest.NewServer(busMux)
	t.Cleanup(busSrv.Close)
	m := &handoffManager{}
	raw := dialRawBusWith(t, busSrv, busToken(t, h, assistantwire.AssistantID, "assistant"), m.handle)
	if err := raw.call("manager.hello", map[string]any{"id": assistantwire.AssistantID, "capabilities": capabilities}, nil); err != nil {
		t.Fatal(err)
	}
	return h, m, raw
}

// A message for an assistant conversation is marked as a person's own (SendParams.Human) only when the hub's auth
// found a full device's credential: never an extension's, an answer-only device's, the hub's own token, or a client's
// word for it.
func TestAssistantSendsSayWhetherAPersonSentThem(t *testing.T) {
	h, m := handoffBusHub(t)
	for _, c := range []struct {
		name       string
		who        *principal
		human      bool
		replyInput bool
	}{
		{"a full device", &principal{ID: "d1", Name: "iPhone", Role: roleFull}, true, false},
		{"an extension's token", &principal{ID: "x1", Role: roleExtension, Extension: "helper"}, false, false},
		{"an answer-only device", &principal{ID: "w1", Role: roleRetiredAnswer}, false, false},
		{"a computer's link", &principal{ID: "l1", Role: roleLink, Machine: "box"}, false, false},
		{"the hub's own token", &principal{Role: roleFull}, false, true},
		{"no credential", nil, false, false},
	} {
		ctx := context.Background()
		if c.who != nil {
			ctx = context.WithValue(ctx, principalKey{}, *c.who)
		}
		if _, err := h.asstOf("").SendParams(ctx, "box", "s1", map[string]any{"client_msg_id": "c1", "human": true, "reply_input": true}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		m.mu.Lock()
		got := m.sends[len(m.sends)-1]
		m.mu.Unlock()
		if got.Human != c.human {
			t.Fatalf("%s: human %v, want %v", c.name, got.Human, c.human)
		}
		if got.ReplyInput != c.replyInput {
			t.Fatalf("%s: reply input %v, want %v", c.name, got.ReplyInput, c.replyInput)
		}
	}
}

// A new conversation's start (its first message) is marked as a person's own (HTTPRequest.Human) by the same test as a
// message: a full device's credential only, never an extension's, a computer link's, an answer-only device's or the hub's
// own token, nor a client's word for it (a header or the body); and no other relayed request carries the mark.
func TestAConversationsFirstMessageIsAPersonsOnlyFromAFullDevice(t *testing.T) {
	h, m := handoffBusHub(t)
	start := h.startConversationOnce()
	for _, c := range []struct {
		name       string
		who        *principal
		human      bool
		replyInput bool
	}{
		{"a full device", &principal{ID: "d1", Name: "iPhone", Role: roleFull}, true, false},
		{"the hub's own token", &principal{Role: roleFull}, false, true},
		{"an extension's token", &principal{ID: "x1", Role: roleExtension, Extension: "helper", Manager: assistantwire.AssistantID}, false, false},
		{"a computer's link", &principal{ID: "l1", Role: roleLink, Machine: "box"}, false, false},
		{"an answer-only device", &principal{ID: "w1", Role: roleRetiredAnswer}, false, false},
		{"no credential", nil, false, false},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/assistant/conversations", strings.NewReader(`{"prompt":"fix CI","human":true,"reply_input":true}`))
		req.Header.Set("X-Persona-Human", "1")
		if c.who != nil {
			req = req.WithContext(context.WithValue(req.Context(), principalKey{}, *c.who))
		}
		w := httptest.NewRecorder()
		start(w, req)
		relayed, _, _ := m.took()
		if (c.human || c.replyInput) && (w.Code != http.StatusOK || len(relayed) != 1) {
			t.Fatalf("%s: %d, relayed %d", c.name, w.Code, len(relayed))
		}
		for _, r := range relayed {
			if r.Human != c.human {
				t.Fatalf("%s: human %v, want %v", c.name, r.Human, c.human)
			}
			if r.ReplyInput != c.replyInput {
				t.Fatalf("%s: reply input %v, want %v", c.name, r.ReplyInput, c.replyInput)
			}
		}
	}
	// Any other relayed request of a full device: not marked.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/assistant/conversations/s1/compact", strings.NewReader(`{}`))
	req = req.WithContext(context.WithValue(req.Context(), principalKey{}, principal{ID: "d1", Role: roleFull}))
	h.forwardSend(httptest.NewRecorder(), req, assistantwire.AssistantID)
	if relayed, _, _ := m.took(); len(relayed) != 1 || relayed[0].Human || relayed[0].ReplyInput {
		t.Fatalf("another request was marked: %+v", relayed)
	}
}

// The client_msg_id the manager gives a conversation's first message (assistantwire.FirstMessageIDPrefix) is never an
// app's: a send that names one is refused before anything is typed, so no later line can carry it.
func TestAnAppNeverSendsAFirstMessagesID(t *testing.T) {
	h := newTestHub(t)
	var sends atomic.Int32
	pipeNode(t, h, "box", func(context.Context) error { sends.Add(1); return nil })
	m := h.byID["box"]
	m.mu.Lock()
	m.sessions = []proto.Session{{SessionID: "s1", Capabilities: &proto.SessionCapabilities{Send: true}}}
	m.mu.Unlock()
	for _, prefix := range []string{assistantwire.FirstMessageIDPrefix, proto.A2ADeliveryPrefix} {
		code, out := call(t, h.routes(), "POST", sendPath, h.cfg.Token, `{"text":"fix CI","client_msg_id":"`+prefix+`0123"}`)
		if code != http.StatusBadRequest || out["code"] != "client_msg_id_reserved" || sends.Load() != 0 {
			t.Fatalf("a send with a reserved id: %d %v, typed %d", code, out, sends.Load())
		}
	}
	code, out := call(t, h.routes(), "POST", "/api/v1/sessions", h.cfg.Token,
		`{"machine_id":"box","resume_session_id":"s1","prompt":"fix CI","client_msg_id":"`+proto.A2ADeliveryPrefix+`0123"}`)
	if code != http.StatusBadRequest || out["code"] != "client_msg_id_reserved" || sends.Load() != 0 {
		t.Fatalf("a resume with a reserved id: %d %v, typed %d", code, out, sends.Load())
	}
	if code, out := call(t, h.routes(), "POST", sendPath, h.cfg.Token, `{"text":"fix CI","client_msg_id":"c-1"}`); code != http.StatusOK || sends.Load() != 1 {
		t.Fatalf("an app's own id: %d %v, typed %d", code, out, sends.Load())
	}
}

// handoffCard is a hand-off card of two providers' values (one named twice); its items' titles end in a marker that no
// record may carry.
func handoffCard(id, kind string) proto.Interaction {
	input, _ := json.Marshal(proto.SecretHandoffInput{Variables: []proto.SecretHandoffVariable{
		{Name: "GITHUB_TOKEN", Provider: "vault", Title: "GitHub › credential REFMARK"},
		{Name: "NPM_TOKEN", Provider: "vault", Title: "npm › credential REFMARK"},
		{Name: "CLOUD_KEY", Provider: "keys", Title: "cloud REFMARK"},
	}})
	return proto.Interaction{ID: id, MachineID: "box", SessionID: "s1", Kind: kind, ToolName: proto.SecretHandoffTool,
		ToolInput: input, Summary: "Approve in the web app"}
}

// An extension's token never approves a hand-off card, even with agents.control: refused and recorded on the providers
// with the extension as who tried. Its no goes through.
func TestAnExtensionNeverApprovesAHandoffCard(t *testing.T) {
	h, _, fx, srv := relayTestHub(t)
	tok := fx.issue("helper", "Helper", []string{extension.ScopeAgentsControl})
	var mu sync.Mutex
	var audits []extwire.Audit
	fx.on(extwire.HubAudit, func(params json.RawMessage) (any, error) {
		var a extwire.Audit
		if err := json.Unmarshal(params, &a); err != nil {
			return nil, err
		}
		mu.Lock()
		audits = append(audits, a)
		mu.Unlock()
		return map[string]any{}, nil
	})
	reportInteraction(t, h, handoffCard("ix_handoff", proto.IxPermission))
	code, out := call(t, srv.Config.Handler, "POST", "/api/v1/interactions/ix_handoff/resolve", tok, `{"allow":true}`)
	if code != http.StatusForbidden || out["code"] != proto.CodeSecretHandoffElsewhere {
		t.Fatalf("the extension's approval: %d %v", code, out)
	}
	mu.Lock()
	got := append([]extwire.Audit(nil), audits...)
	mu.Unlock()
	if len(got) != 2 || got[0].Actor != "extension:helper" || got[0].Detail["reason"] != handoffExtension ||
		got[0].Detail["device_name"] != nil || got[0].Extension != "vault" || got[1].Extension != "keys" {
		t.Fatalf("recorded %+v", got)
	}
	if _, ok := h.pendingInteraction("ix_handoff"); !ok {
		t.Fatal("the refused approval took the card")
	}
	// Its no reaches the manager (which holds no such card here).
	if code, out := call(t, srv.Config.Handler, "POST", "/api/v1/interactions/ix_handoff/resolve", tok, `{"allow":false}`); code == http.StatusForbidden {
		t.Fatalf("the extension's no: %d %v", code, out)
	}
}

// A refusal of the same card, credential and reason is recorded once in handoffRecordEvery; what is remembered is
// bounded, the stale first.
func TestHandoffRefusalsRecordedOnce(t *testing.T) {
	var m handoffRecorded
	now := time.Now()
	if !m.first("ix\x00device:d\x00client", now) || m.first("ix\x00device:d\x00client", now.Add(time.Minute)) {
		t.Fatal("the second try within the window was recorded again")
	}
	if !m.first("ix\x00device:d\x00answer_only", now) || !m.first("ix2\x00device:d\x00client", now) {
		t.Fatal("another reason or card was not recorded")
	}
	if !m.first("ix\x00device:d\x00client", now.Add(handoffRecordEvery)) {
		t.Fatal("not recorded again after the window")
	}
	at := now.Add(handoffRecordEvery)
	for i := range handoffRecordMost {
		m.first(fmt.Sprint("fill", i), at.Add(time.Duration(i)*time.Millisecond))
	}
	full := at.Add(time.Second)
	if !m.first("one more", full) || len(m.seen) != handoffRecordMost {
		t.Fatalf("a new key when full: %d remembered", len(m.seen))
	}
	if _, kept := m.seen["fill0"]; kept {
		t.Fatal("the oldest was not the one forgotten")
	}
	if m.first("one more", full.Add(time.Second)) || m.first("fill1", full.Add(time.Second)) {
		t.Fatal("a key remembered was recorded again")
	}
	if !m.first("last", full.Add(3*handoffRecordEvery)) || len(m.seen) != 1 {
		t.Fatalf("the stale were not dropped: %d remembered", len(m.seen))
	}
	m.forget("last")
	if !m.first("last", full.Add(3*handoffRecordEvery)) {
		t.Fatal("a forgotten key was not recorded again")
	}
}
