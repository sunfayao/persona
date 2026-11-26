package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/extwire"
)

// The To-dos view across managers (todo_view.go, #10226): the hub adds the managers' counts,
// lists the features only while every manager whose to-dos it merges names them, and never computes a view itself.

// todoReplies provides responses for a test manager's list and item routes.
type todoReplies struct {
	mu    sync.Mutex
	list  string // the list's body
	items map[string]string
}

func (r *todoReplies) set(list string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.list = list
}

func (r *todoReplies) answer(method string, params json.RawMessage) (any, error) {
	if method != assistantwire.MethodHTTP {
		return map[string]any{}, nil
	}
	var req assistantwire.HTTPRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	json200 := func(body string) assistantwire.HTTPResponse {
		return assistantwire.HTTPResponse{Status: http.StatusOK, Header: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(body)}
	}
	switch {
	case req.Path == "/api/v1/todos":
		return json200(r.list), nil
	case r.items[req.Path] != "":
		return json200(r.items[req.Path]), nil

	}
	return assistantwire.HTTPResponse{Status: http.StatusNotFound, Body: []byte(`{"error":"Not Found"}`)}, nil
}

// todoViewHub is a hub with two managers on its bus, the default (assistant) and m-0c0c0c, each answering the to-dos
// with its replies.
func todoViewHub(t *testing.T) (*Hub, *httptest.Server, *guardManager, *guardManager, *todoReplies, *todoReplies) {
	t.Helper()
	h, srv, _ := newBusHub(t)
	ra, rb := &todoReplies{}, &todoReplies{}
	a := connectGuardManager(t, h, srv.URL, assistantwire.AssistantID, ra.answer)
	b := connectGuardManager(t, h, srv.URL, "m-0c0c0c", rb.answer)
	return h, srv, a, b, ra, rb
}

// todoViewCapabilities is the manager's hello again, naming the To-dos view's capabilities it is given.
func todoViewCapabilities(t *testing.T, m *guardManager, capabilities ...string) {
	t.Helper()
	if err := m.call(t, busHello, map[string]any{"id": m.id, "capabilities": append([]string{assistantwire.CapabilityConfigOwn}, capabilities...)}, nil); err != nil {
		t.Fatal(err)
	}
}

func todoViewGet(t *testing.T, h *Hub, srv *httptest.Server, path string, header map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

// todoCountsJSON is counts with every section, n in the ones named.
func todoCountsJSON(open, needsYou, inProgress int, sections map[string]int) string {
	c := assistantwire.TodoCounts{Open: open, NeedsYou: needsYou, InProgress: inProgress, Sections: map[string]int{}}
	for _, s := range assistantwire.TodoListSections {
		c.Sections[s] = sections[s]
	}
	b, _ := json.Marshal(c)
	return string(b)
}

// Each feature is listed only while every manager whose to-dos the list merges is connected and names it.
func TestTodoViewFeaturesWhileEveryManagerNamesThem(t *testing.T) {
	h, _, a, b, _, _ := todoViewHub(t)
	listed := func() bool { return slices.Contains(h.features(), featureTodoView) }
	if listed() {
		t.Fatal("listed before a manager announced it")
	}
	todoViewCapabilities(t, a, assistantwire.CapabilityTodoView)
	if listed() {
		t.Fatal("listed while the second manager lacks it")
	}
	todoViewCapabilities(t, b, assistantwire.CapabilityTodoView)
	if !listed() {
		t.Fatal("both managers announced todo_view")
	}
	if err := h.busConnOf(a.id).ws.CloseNow(); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "the default offline", func() bool { return h.busConnOf(a.id) == nil })
	if listed() {
		t.Fatal("listed with the default offline")
	}
}

// todo_timeline (#10465) is listed only while every manager whose to-dos the list merges is connected and names it,
// whatever the other To-dos features.
func TestTodoTimelineFeatureWhileEveryManagerNamesIt(t *testing.T) {
	h, _, a, b, _, _ := todoViewHub(t)
	listed := func() bool { return slices.Contains(h.features(), featureTodoTimeline) }
	todoViewCapabilities(t, a, assistantwire.CapabilityTodoView, assistantwire.CapabilityTodoTimeline)
	todoViewCapabilities(t, b, assistantwire.CapabilityTodoView)
	if listed() {
		t.Fatal("listed while m-0c0c0c does not name it")
	}
	todoViewCapabilities(t, b, assistantwire.CapabilityTodoTimeline)
	if !listed() {
		t.Fatal("not listed with every manager naming it")
	}
	if f := h.features(); slices.Contains(f, featureTodoView) {
		t.Fatalf("todo_view listed with m-0c0c0c not naming it: %v", f)
	}
	if err := h.busConnOf(b.id).ws.CloseNow(); err != nil {
		t.Fatal(err)
	}
	eventually(t, 5*time.Second, "m-0c0c0c offline", func() bool { return h.busConnOf(b.id) == nil })
	if !listed() {
		t.Fatal("not listed with only the default left, naming it") // an offline manager other than the default leaves the list
	}
	todoViewCapabilities(t, a, assistantwire.CapabilityTodoView)
	if listed() {
		t.Fatal("listed after the default stopped naming it")
	}
}

// An extension's report cannot add the To-dos view's features: only the managers' hellos promise them.
func TestTodoViewFeaturesNotFromAReport(t *testing.T) {
	h := newTestHub(t)
	h.keepExtReport(assistantwire.AssistantID, &extwire.Report{Features: []string{featureTodoView,
		featureTodoTimeline, "fa"}})
	features := h.features()
	if !slices.Contains(features, "fa") {
		t.Fatalf("the report's own feature is missing: %v", features)
	}
	for _, f := range []string{featureTodoView, featureTodoTimeline} {
		if slices.Contains(features, f) {
			t.Errorf("a report added %s", f)
		}
	}
}

// The merged list's counts are the managers' added up, field by field and section by section, before the validator is
// taken, so the tag is the hash of exactly the bytes sent; an older manager without counts adds nothing, and with none
// at all the list has none.
func TestTodosListCountsAddedUp(t *testing.T) {
	h, srv, _, _, ra, rb := todoViewHub(t)
	ca := todoCountsJSON(3, 1, 1, map[string]int{"needs_you": 1, "working": 1, "todo": 1, "done": 2})
	cb := todoCountsJSON(2, 1, 2, map[string]int{"needs_you": 2, "waiting": 2, "done": 1})
	ra.set(`{"todos":[{"id":"todo_a1","title":"Water the roses"}],"counts":` + ca + `}`)
	rb.set(`{"counts":` + cb + `,"todos":[{"id":"todo_b1","title":"Book the ferry"}]}`)

	read := func() (*http.Response, []byte, *assistantwire.TodoCounts, []map[string]any) {
		t.Helper()
		resp, body := todoViewGet(t, h, srv, "/api/v1/todos", nil)
		var out struct {
			Todos  []map[string]any          `json:"todos"`
			Counts *assistantwire.TodoCounts `json:"counts"`
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &out) != nil {
			t.Fatalf("list: %d %s", resp.StatusCode, body)
		}
		return resp, body, out.Counts, out.Todos
	}
	resp, body, counts, todos := read()
	want := assistantwire.TodoCounts{Open: 5, NeedsYou: 2, InProgress: 3, Sections: map[string]int{}}
	for _, s := range assistantwire.TodoListSections {
		want.Sections[s] = 0
	}
	want.Sections["needs_you"], want.Sections["working"], want.Sections["waiting"], want.Sections["todo"], want.Sections["done"] = 3, 1, 2, 1, 3
	if counts == nil || !reflect.DeepEqual(*counts, want) {
		t.Fatalf("counts %+v, want %+v", counts, want)
	}
	if len(todos) != 2 || todos[0]["manager"] != assistantwire.AssistantID || todos[1]["manager"] != "m-0c0c0c" {
		t.Fatalf("the rows: %v", todos)
	}
	sum := sha256.Sum256(body)
	tag := resp.Header.Get("ETag")
	if tag != `W/"t1-`+hex.EncodeToString(sum[:16])+`"` {
		t.Fatalf("the tag %q is not the hash of the bytes sent: %s", tag, body)
	}
	if again, _ := todoViewGet(t, h, srv, "/api/v1/todos", map[string]string{"If-None-Match": tag}); again.StatusCode != http.StatusNotModified {
		t.Fatalf("the same lists again: %d, want 304", again.StatusCode)
	}

	// m-0c0c0c changes only its counts: the merged list's bytes and tag change with them.
	rb.set(`{"counts":` + todoCountsJSON(2, 0, 2, map[string]int{"can_wait": 2, "waiting": 2, "done": 1}) + `,"todos":[{"id":"todo_b1","title":"Book the ferry"}]}`)
	if changed, _ := todoViewGet(t, h, srv, "/api/v1/todos", map[string]string{"If-None-Match": tag}); changed.StatusCode != http.StatusOK ||
		changed.Header.Get("ETag") == tag {
		t.Fatalf("other counts: %d %q, want 200 with a new tag", changed.StatusCode, changed.Header.Get("ETag"))
	}

	// An older m-0c0c0c sends no counts: the default's alone, though its list still merges.
	rb.set(`{"todos":[{"id":"todo_b1","title":"Book the ferry"}]}`)
	if _, _, counts, todos := read(); counts == nil || counts.Open != 3 || counts.Sections["done"] != 2 || len(todos) != 2 {
		t.Fatalf("with an older manager: %+v, %d rows", counts, len(todos))
	}
	// An older default and a newer m-0c0c0c: its counts, though the rest of the answer is the default's.
	ra.set(`{"todos":[{"id":"todo_a1","title":"Water the roses"}],"as_of":"t"}`)
	rb.set(`{"counts":` + cb + `,"todos":[{"id":"todo_b1","title":"Book the ferry"}],"as_of":"u"}`)
	if _, body, counts, _ := read(); counts == nil || counts.Open != 2 || counts.Sections["waiting"] != 2 {
		t.Fatalf("with an older default: %s", body)
	}
	// Nobody sends counts: the list has none.
	rb.set(`{"todos":[{"id":"todo_b1","title":"Book the ferry"}]}`)
	if _, body, counts, _ := read(); counts != nil {
		t.Fatalf("counts from nowhere: %s", body)
	}
}

// The merge writes the added-up counts byte for byte as decoding every answer and encoding the merge again does, on
// every shape the scan meets.
func TestTodoCountsMergeMatchesDecoding(t *testing.T) {
	ca := todoCountsJSON(1, 1, 0, map[string]int{"needs_you": 1})
	cb := todoCountsJSON(2, 0, 2, map[string]int{"working": 1, "waiting": 1})
	for _, c := range []struct {
		name    string
		answers []listAnswer
	}{
		{"both", []listAnswer{listOf("assistant", 200, `{"todos":[{"id":"a"}],"counts":`+ca+`}`), listOf("m-b", 200, `{"counts":`+cb+`,"todos":[{"id":"b"}]}`)}},
		{"the second only", []listAnswer{listOf("assistant", 200, `{"todos":[{"id":"a"}],"as_of":"t"}`), listOf("m-b", 200, `{"counts":`+cb+`,"todos":[]}`)}},
		{"null counts", []listAnswer{listOf("assistant", 200, `{"todos":[{"id":"a"}],"counts":null}`), listOf("m-b", 200, `{"counts":`+cb+`,"todos":[]}`)}},
		{"a failed default", []listAnswer{listOf("assistant", 503, `{"code":"assistant_offline","counts":`+ca+`}`), listOf("m-b", 200, `{"counts":`+cb+`,"todos":[]}`)}},
		{"not an object", []listAnswer{listOf("assistant", 200, `[]`), listOf("m-b", 200, `{"counts":`+cb+`,"todos":null}`)}},
		{"a list that is no array", []listAnswer{listOf("assistant", 200, `{"todos":{"id":"a"},"counts":`+ca+`}`), listOf("m-b", 200, `{"counts":`+cb+`,"todos":[]}`)}},
		{"one needing decoding", []listAnswer{listOf("assistant", 200, `{"todos":[{"id":"a"}],"a":1,"a":2,"counts":`+ca+`}`), listOf("m-b", 200, `{"counts":`+cb+`,"todos":[{"id":"b"}]}`)}},
		{"escaped counts key", []listAnswer{listOf("assistant", 200, `{"todos":[],"\u0063ounts":`+ca+`}`), listOf("m-b", 200, `{"counts":`+cb+`,"todos":[]}`)}},
		{"nobody", []listAnswer{listOf("assistant", 200, `{"todos":[{"id":"a"}],"z":"<&>"}`), listOf("m-b", 200, `{"todos":[{"id":"b"}]}`)}},
	} {
		got, want := httptest.NewRecorder(), httptest.NewRecorder()
		answers := cloneAnswers(c.answers)
		for i := range answers {
			if answers[i].ok() {
				(&Hub{}).noteTodoOwners(&answers[i]) // the merge's own scan, as mergeList takes it
			}
		}
		writeMergedSet(got, "todos", answers, todoListSums(answers))
		plain := cloneAnswers(c.answers)
		writeDecoded(want, "todos", plain, todoListSums(plain))
		sameResponse(t, c.name, got, want)
	}
	// What they add up to: one answer's counts as it sent them; two, key by key.
	sums := todoListSums([]listAnswer{listOf("assistant", 200, `{"todos":[],"counts":`+ca+`}`), listOf("m-b", 200, `{"todos":[],"counts":`+cb+`}`)})
	if got, want := string(sums["counts"]), todoCountsJSON(3, 1, 2, map[string]int{"needs_you": 1, "working": 1, "waiting": 1}); !sameJSONValue(got, want) {
		t.Fatalf("summed %s, want %s", got, want)
	}
	if got := string(todoListSums([]listAnswer{listOf("assistant", 200, `{"todos":[],"counts":`+ca+`}`)})["counts"]); got != ca {
		t.Fatalf("one manager's %s, want %s", got, ca)
	}
	// Members a newer manager adds, which this hub does not know: passed on as sent from one manager; from two, a
	// number and a map added up, anything else the first's.
	newer := `{"open":1,"needs_you":1,"in_progress":0,"sections":{"needs_you":1,"later":2},"assistant_needs":3,"note":"a"}`
	if got := string(todoListSums([]listAnswer{listOf("assistant", 200, `{"todos":[],"counts":`+newer+`}`)})["counts"]); got != newer {
		t.Fatalf("one newer manager's %s, want %s", got, newer)
	}
	sums = todoListSums([]listAnswer{listOf("assistant", 200, `{"todos":[],"counts":`+newer+`}`),
		listOf("m-b", 200, `{"todos":[],"counts":{"open":2,"sections":{"later":1,"todo":2},"assistant_needs":4,"note":"b","x":1}}`)})
	if got, want := string(sums["counts"]), `{"open":3,"needs_you":1,"in_progress":0,"sections":{"needs_you":1,"later":3,"todo":2},"assistant_needs":7,"note":"a","x":1}`; !sameJSONValue(got, want) {
		t.Fatalf("two newer managers' %s, want %s", got, want)
	}
}

// sameJSONValue: a and b are the same JSON value, whatever the order of their members.
func sameJSONValue(a, b string) bool {
	var x, y any
	return json.Unmarshal([]byte(a), &x) == nil && json.Unmarshal([]byte(b), &y) == nil && reflect.DeepEqual(x, y)
}

// One to-do, and its timeline, is relayed to the manager that listed it (byTodo), as the other to-do routes are, with
// no hub code: the to-do's answer is passed on as its manager wrote it, an older manager's (the row alone) and a newer
// one's (with the paper's rounds, #10465) alike, and so is an older manager's answer to the timeline (its mux has no
// such route: 404).
func TestTodoItemRelayedToItsManager(t *testing.T) {
	h, srv, _, _, ra, rb := todoViewHub(t)
	ra.set(`{"todos":[{"id":"todo_a1"}]}`)
	rb.set(`{"todos":[{"id":"todo_b1"},{"id":"todo_b2"}]}`)
	older, newer := `{"todo":{"id":"todo_b1","title":"Water the roses"}}`,
		`{"todo":{"id":"todo_b2","title":"Prune the hedge"},"rounds":[],"rounds_total":0}`
	rb.mu.Lock()
	rb.items = map[string]string{"/api/v1/todos/todo_b1": older, "/api/v1/todos/todo_b2": newer}
	rb.mu.Unlock()
	if resp, body := todoViewGet(t, h, srv, "/api/v1/todos", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", resp.StatusCode, body)
	}
	for path, want := range map[string]string{"/api/v1/todos/todo_b1": older, "/api/v1/todos/todo_b2": newer} {
		if resp, body := todoViewGet(t, h, srv, path, nil); resp.StatusCode != http.StatusOK || !sameJSONValue(string(body), want) {
			t.Errorf("%s: %d %s, want %s", path, resp.StatusCode, body, want)
		}
	}
}
