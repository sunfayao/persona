package assistant

import (
	"context"
	"fmt"
	"github.com/sunfayao/persona/server/internal/prompts"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/ai"
)

// logBuffer keeps what the inbox logged.
type logBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuffer) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.b, format+"\n", args...)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// A judge call put off by a held connection leaves the item unlinked and says nothing in the log.
func TestInboxJudgePutOffQuietly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"type":"service_unavailable","code":"account_concurrency_full","message":"account concurrency is full"}}`))
	}))
	t.Cleanup(srv.Close)
	h := agentHub(t)
	var logs logBuffer
	h.logf = logs.logf
	h.in.model = ai.New(ai.Config{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	for _, id := range []string{"x", "y"} {
		it := actionItem(id, time.Now())
		h.inbox.mu.Lock()
		h.inbox.items[id] = it
		h.inbox.mu.Unlock()
		it.Agent = &InboxAgent{State: AgentQueued}
		if h.linkTask(context.Background(), it, []prompts.InboxTask{{ID: "todo_1", Title: "示例"}}) || it.Agent.TodoID != "" {
			t.Fatalf("linked on a put-off call: %+v", it.Agent)
		}
	}
	if s := logs.String(); s != "" {
		t.Fatalf("logged:\n%s", s)
	}
}
