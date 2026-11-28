package hub

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/compat"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/sessionlist"
)

type originRow struct {
	proto.Session
	PaneSource string `json:"pane_source"`
}

type originPage struct {
	Sessions []originRow `json:"sessions"`
	Remove   []string    `json:"remove"`
	HasMore  bool        `json:"has_more"`
	Cursor   string      `json:"next_cursor"`
	Revision string      `json:"revision"`
	Total    int         `json:"total"`
}

func getOriginPage(t *testing.T, h *Hub, q url.Values) originPage {
	t.Helper()
	w := httptest.NewRecorder()
	h.listSessions(w, httptest.NewRequest("GET", "/api/v1/sessions?"+q.Encode(), nil))
	var p originPage
	if w.Code != 200 {
		t.Fatalf("page: %d %s", w.Code, w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func getOriginCounts(t *testing.T, h *Hub, q url.Values) sessionlist.Counts {
	t.Helper()
	q = cloneOriginQuery(q)
	q.Set("counts", "1")
	w := httptest.NewRecorder()
	h.listSessions(w, httptest.NewRequest("GET", "/api/v1/sessions?"+q.Encode(), nil))
	var c sessionlist.Counts
	if w.Code != 200 {
		t.Fatalf("counts: %d %s", w.Code, w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.PaneSource == nil {
		// pane_source is omitempty: a query no row matches leaves it out, as an empty count.
		c.PaneSource = map[string]int{}
	}
	return c
}

func TestRuntimePaneSourceFrozenAndDelta(t *testing.T) {
	h := newTestHub(t)
	m := newMachine(h, MachineConfig{ID: "demo"})
	h.machines, h.byID["demo"] = []*Machine{m}, m
	m.sessions = []proto.Session{
		{MachineID: "demo", SessionID: "a", SourceKind: "runtime", PaneID: "w1:p1"},
		{MachineID: "demo", SessionID: "b", SourceKind: "runtime", PaneID: "w1:p2"},
		{MachineID: "demo", SessionID: "c", SourceKind: "runtime", SpaceKind: "codex_app"},
	}
	q := url.Values{"pane_source": {"terminal"}, "limit": {"1"}, "summary": {"1"}}
	first := getOriginPage(t, h, q)
	before := getOriginCounts(t, h, q)
	if first.Total != 2 || before.Total != 2 || first.Revision != before.Revision {
		t.Fatal("baseline")
	}
	// Only origin metadata changes: transport, time, state and identity stay put.
	m.mu.Lock()
	m.sessions[1].SpaceKind = "codex_app"
	m.sessions[2].SpaceKind = ""
	m.sessions[2].PaneID = "w1:p3"
	m.mu.Unlock()
	h.sessionsChanged(m)
	frozen := cloneOriginQuery(q)
	frozen.Set("cursor", first.Cursor)
	p := getOriginPage(t, h, frozen)
	if p.Revision != first.Revision || p.Total != 2 || len(p.Sessions) != 1 || p.Sessions[0].SessionID != "b" || p.Sessions[0].PaneSource != "terminal" {
		t.Fatalf("frozen: %+v", p)
	}
	if c := getOriginCounts(t, h, frozen); !reflect.DeepEqual(c, before) {
		t.Fatalf("frozen counts: %+v", c)
	}
	deltaQ := cloneOriginQuery(q)
	deltaQ.Set("since", first.Revision)
	delta := getOriginPage(t, h, deltaQ)
	if delta.Revision == first.Revision || delta.Total != 2 || !delta.HasMore {
		t.Fatalf("delta: %+v", delta)
	}
	current := getOriginCounts(t, h, deltaQ)
	if current.Total != 2 || current.Revision != delta.Revision {
		t.Fatalf("current counts: %+v", current)
	}
	// Freeze the incremental view as well, then switch the entering row out again.
	m.mu.Lock()
	m.sessions[2].SpaceKind = "codex_app"
	m.mu.Unlock()
	h.sessionsChanged(m)
	deltaQ.Set("cursor", delta.Cursor)
	next := getOriginPage(t, h, deltaQ)
	if next.Revision != delta.Revision || next.Total != 2 {
		t.Fatal("delta continuation drifted")
	}
	rows := append(delta.Sessions, next.Sessions...)
	removes := append(delta.Remove, next.Remove...)
	if len(rows) != 1 || rows[0].SessionID != "c" || rows[0].PaneSource != "terminal" || !reflect.DeepEqual(removes, []string{runtimeListID("demo", "b")}) {
		t.Fatalf("enter/leave: %+v %v", rows, removes)
	}
	if c := getOriginCounts(t, h, deltaQ); !reflect.DeepEqual(c, current) {
		t.Fatalf("frozen delta counts: %+v", c)
	}
	for _, source := range []string{"desktop", ""} {
		bad := cloneOriginQuery(frozen)
		if source == "" {
			bad.Del("pane_source")
		} else {
			bad.Set("pane_source", source)
		}
		for _, counts := range []bool{false, true} {
			if counts {
				bad.Set("counts", "1")
			}
			w := httptest.NewRecorder()
			h.listSessions(w, httptest.NewRequest("GET", "/api/v1/sessions?"+bad.Encode(), nil))
			if w.Code != 400 {
				t.Fatalf("cursor origin not bound: %d", w.Code)
			}
		}
	}
	if !slices.Contains(h.features(), featureSessionsPaneSource) || slices.Contains(proto.Features, featureSessionsPaneSource) {
		t.Fatal("runtime origin feature must be advertised only by the hub")
	}
}

func TestRuntimePaneSourceInvalidQueries(t *testing.T) {
	h := newTestHub(t)
	for _, query := range []string{"pane_source=", "pane_source=runtime", "pane_source=Terminal", "pane_source=terminal&pane_source=terminal", "pane_source=terminal,feishu"} {
		w := httptest.NewRecorder()
		h.listSessions(w, httptest.NewRequest("GET", "/api/v1/sessions?"+query, nil))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_query") {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body)
		}
	}
}

func cloneOriginQuery(q url.Values) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = slices.Clone(v)
	}
	return out
}

// Session has compatibility decoders (UnmarshalJSONFrom, UnmarshalJSON); do not promote them over the added scalar.
// encoding/json prefers UnmarshalJSONFrom, so originRow declares both.
func (s *originRow) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	raw, err := dec.ReadValue()
	if err != nil {
		return err
	}
	return s.UnmarshalJSON(raw)
}

func (s *originRow) UnmarshalJSON(raw []byte) error {
	if err := json.Unmarshal(raw, &s.Session); err != nil {
		return err
	}
	var fields struct {
		PaneSource string `json:"pane_source"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	s.PaneSource = fields.PaneSource
	return nil
}

func TestRuntimePaneSourceExtensionScope(t *testing.T) {
	h := newTestHub(t)
	h.cfg.Machines = []MachineConfig{{ID: "own-a"}, {ID: "own-b"}, {ID: "foreign"}}
	h.devices.assign = map[string]string{"own-a": "manager-a", "own-b": "manager-a", "foreign": "manager-b"}
	for _, cfg := range h.cfg.Machines {
		m := newMachine(h, cfg)
		m.sessions = []proto.Session{{MachineID: cfg.ID, SessionID: "same", PaneID: "w1:p1"}, {MachineID: cfg.ID, SessionID: "second", PaneID: "w1:p2"}}
		h.machines, h.byID[cfg.ID] = append(h.machines, m), m
	}
	request := func(q string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/sessions?"+q, nil)
		r = r.WithContext(context.WithValue(r.Context(), principalKey{}, principal{Role: roleExtension, Manager: "manager-a"}))
		w := httptest.NewRecorder()
		h.listSessions(w, r)
		return w
	}
	for _, q := range []string{"pane_source=terminal", "pane_source=terminal&counts=1"} {
		if w := request(q); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_query") {
			t.Fatalf("unsupported aggregate: %d %s", w.Code, w.Body)
		}
	}
	if w := request("pane_source=terminal&machine_id=foreign"); w.Code != 403 {
		t.Fatalf("foreign machine: %d %s", w.Code, w.Body)
	}
	q := "pane_source=terminal&machine_id=own-a&limit=1"
	w := request(q)
	var first originPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &first) != nil || first.Total != 2 || len(first.Sessions) != 1 || first.Sessions[0].MachineID != "own-a" {
		t.Fatalf("authorized page: %d %s", w.Code, w.Body)
	}
	m := h.byID["own-a"]
	m.mu.Lock()
	m.sessions[1].SpaceKind = "codex_app"
	m.mu.Unlock()
	h.sessionsChanged(m)
	w = request(q + "&counts=1&cursor=" + url.QueryEscape(first.Cursor))
	var c sessionlist.Counts
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &c) != nil || c.Total != 2 || c.Revision != first.Revision || c.PaneSource["terminal"] != 2 {
		t.Fatalf("authorized frozen counts: %d %s", w.Code, w.Body)
	}
}

func TestRuntimeOriginSummaryCompatibility(t *testing.T) {
	summary := runtimeSummary(proto.Session{StartedByPersona: true, SpaceKind: "codex_app", Host: "herdr", Background: []proto.BackgroundTask{{ID: "bg"}}})
	// By value, by pointer and as a slice element: the last two would pick Session's promoted MarshalJSONTo.
	for _, v := range []any{summary, &summary, []runtimeRowSummary{summary}} {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if raw[0] == '[' {
			raw = raw[1 : len(raw)-1]
		}
		checkOriginSummaryWire(t, raw)
	}
}

func checkOriginSummaryWire(t *testing.T, raw []byte) {
	t.Helper()
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"started_by_persona": true, compat.StartedField: true, "pane_source": "desktop", "host": "herdr", "waiting": true, "background_count": float64(1)} {
		if wire[key] != want {
			t.Fatalf("%s=%v want %v", key, wire[key], want)
		}
	}
}
