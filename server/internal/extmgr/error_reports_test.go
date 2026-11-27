package extmgr

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/sdk/go/wire"
	"github.com/sunfayao/persona/server/internal/errreport"
	pserve "github.com/sunfayao/persona/server/internal/extension/serve"
)

// serveErrorsExtensionForTest takes error reports and writes each delivery as a line of JSON to ERRORS_OUT.
func serveErrorsExtensionForTest() {
	var mu sync.Mutex
	out := os.Getenv("ERRORS_OUT")
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-errors", Version: "1.0.0",
		Namespaces: map[string]pserve.Handler{
			"error_reports": func(_ context.Context, method string, params json.RawMessage) (any, error) {
				if method != "deliver" {
					return nil, pserve.UnknownMethod(method)
				}
				mu.Lock()
				defer mu.Unlock()
				f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
				if err == nil {
					_, _ = f.Write(append(append([]byte{}, params...), '\n'))
					_ = f.Close()
				}
				return nil, nil
			}}})
}

// deliveries reads what the extension was given so far.
func deliveries(t *testing.T, out string) []wire.ErrorReportsDeliver {
	t.Helper()
	raw, _ := os.ReadFile(out)
	var got []wire.ErrorReportsDeliver
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if l == "" {
			continue
		}
		var d wire.ErrorReportsDeliver
		if err := json.Unmarshal([]byte(l), &d); err != nil {
			t.Fatalf("%q: %v", l, err)
		}
		got = append(got, d)
	}
	return got
}

// A report the gate let on that finds the queue full is not lost from the counts: the next one of its problem is
// not folded into it and carries it.
func TestErrorReportsFullQueueKeepsTheFold(t *testing.T) {
	h := &Manager{}
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	h.errReports.once.Do(func() {
		h.errReports.gate = errreport.NewGate(func() time.Time { return now })
		h.errReports.queue = make(chan wire.ErrorReport, 1)
	})
	h.errReports.queue <- wire.ErrorReport{ID: "filler"}
	mk := func(id string) wire.ErrorReport {
		return wire.ErrorReport{ID: id, Source: wire.ErrorSourceHub, Kind: "log", Message: "inbox: round failed"}
	}
	hub := []string{wire.ErrorSourceHub}
	if h.takeErrorReport(mk("a"), "hub", hub, nil) {
		t.Fatal("no room: not taken")
	}
	<-h.errReports.queue
	now = now.Add(time.Second)
	if !h.takeErrorReport(mk("b"), "hub", hub, nil) {
		t.Fatal("the next one of the problem is not folded into one never delivered")
	}
	if r := <-h.errReports.queue; r.ID != "b" || r.Count != 2 {
		t.Fatalf("it carries the dropped one: %s %d", r.ID, r.Count)
	}
}
