package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// The Lark card reports the actual decision delivery result.
func TestCheckDecisionWordsCardDelivery(t *testing.T) {
	for _, tc := range []struct {
		name, kind, supervisor string
		stop                   func(*fakeHub)
		status                 int
	}{
		{name: "delivered", status: http.StatusOK},
		{name: "no agent", stop: func(f *fakeHub) { f.setSessions("box") }, status: http.StatusOK},
		{name: "no supervisor configured", supervisor: "missing", stop: func(f *fakeHub) { f.setSessions("box"); f.setSessions("server") }, status: http.StatusOK},
		{name: "supervisor unreachable", supervisor: "unreachable", stop: func(f *fakeHub) { f.setSessions("box"); f.setSessions("server") }, status: http.StatusOK},
		{name: "stopped refusal", stop: func(f *fakeHub) {
			f.mu.Lock()
			f.failSend = &rpc.Error{Code: proto.CodeAgentStopped, Message: "the agent has stopped"}
			f.mu.Unlock()
		}, status: http.StatusOK},
		{name: "retryable refusal", stop: func(f *fakeHub) { f.mu.Lock(); f.failSend = errors.New("temporary network break"); f.mu.Unlock() }, status: http.StatusBadGateway},
		{name: "words equal an option", kind: "label", status: http.StatusOK},
		{name: "recommended option", kind: "recommended", status: http.StatusOK},
		{name: "other option", kind: "option", status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := todoTestDB(t)
			s, f, todo, _, _ := conclusionServiceOn(t, database, false)
			ctx := context.Background()
			s.todoAskAfter = time.Nanosecond
			s.superviseTimes.gather = time.Hour
			task := todoProgressTask(todo.ID)
			setTasks(s, task)
			assessTodoBaseline(t, s, task.ID)
			assessTodoRun(t, s, task.ID, nil)
			assessTodoRun(t, s, task.ID, nil)
			a := askOf(s, todo.ID)
			if a == nil || a.Check != task.ID {
				t.Fatalf("check ask: %+v", a)
			}
			agentAtWork(f, todo)
			if tc.supervisor != "" {
				setSettings(t, s, func(v *Settings) {
					v.Main = ""
					v.BackgroundSupervisor = ""
				})
				if tc.supervisor == "missing" {
					if _, _, err := s.updateSupervision(ctx, todo.ID, func(v *Supervision) bool { v.Supervisor = ""; return true }); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tc.stop != nil {
				tc.stop(f)
			}
			words := "Run only the unit tests first, then look again"
			round := a.AskedAt.UTC().Format(timeKeyLayout)
			wantKey, delivered, sends := "assistant-reports.decision_chosen_noted", "", 1
			switch tc.kind {
			case "label":
				words = a.Options[1].Label
			case "recommended", "option":
				words = a.Recommend
				wantKey = "assistant-reports.verdict_taken_supervisor"
				if tc.kind == "option" {
					words = a.Options[1].Label
					wantKey = "assistant-reports.decision_chosen_supervisor"
				}
				sends = 0
			}
			if tc.stop != nil {
				wantKey = "assistant-reports.decision_chosen_for_supervisor"
				delivered = "none"
				sends = 0
			}
			var mu sync.Mutex
			var cards []string
			f.answerApp(AppFeishu, FeishuUpdateCard, func(raw json.RawMessage) (any, error) {
				var in struct {
					Card string `json:"card"`
				}
				_ = json.Unmarshal(raw, &in)
				mu.Lock()
				cards = append(cards, in.Card)
				mu.Unlock()
				return map[string]any{}, nil
			})
			before := len(sentToAgent(f))
			reply := func() (acceptResult, error) {
				if tc.kind == "option" || tc.kind == "recommended" {
					return s.chooseOption(ctx, todo.ID, round, words)
				}
				return s.answerDecisionText(ctx, todo.ID, round, words)
			}
			answer, err := reply()
			if (err == nil && answer.taken) != (tc.status == http.StatusOK) {
				t.Fatalf("decision result: %+v, %v", answer, err)
			}
			if got := len(sentToAgent(f)) - before; got != sends {
				t.Fatalf("Agent sends: %d, want %d", got, sends)
			}
			got := askOf(s, todo.ID)
			if tc.status != http.StatusOK {
				if !got.AcceptedAt.IsZero() || todoNow(s, todo.ID).Supervision.State != reviewNeedsUser {
					t.Fatalf("retryable refusal answered: %+v", got)
				}
				s.waitWorkEvents()
				return
			}
			if (answer.undelivered != nil) != (delivered != "") {
				t.Errorf("decision delivery: %+v, want %q", answer, delivered)
			}
			if got.AcceptedAt.IsZero() || todoNow(s, todo.ID).Supervision.State == reviewNeedsUser {
				t.Fatalf("still waits: %+v", got)
			}
			rows, err := database.ListTodoDecisions(ctx, todo.ID, true)
			if err != nil || len(rows) != 1 || rows[0].Choice != words {
				t.Fatalf("decisions: %+v, %v", rows, err)
			}
			if tc.supervisor != "" {
				// Make the synthetic signals due and attempt the real digest; no reachable recipient can take them.
				s.supervise.mu.Lock()
				if s.supervise.timer != nil {
					s.supervise.timer.Stop()
				}
				s.supervise.last = time.Time{}
				for key, g := range s.supervise.pending {
					g.At = time.Now().Add(-2 * time.Hour)
					s.supervise.pending[key] = g
				}
				s.supervise.mu.Unlock()
				s.sendDigest(ctx)
				s.supervise.mu.Lock()
				told := s.supervise.sent[todo.ID+"|"+sigOwnerChoice] == "undelivered|"+a.AskedAt.UTC().Format(time.RFC3339Nano)
				s.supervise.mu.Unlock()
				if told {
					t.Fatal("the unreachable supervisor was marked told")
				}
			}
			if tc.stop != nil {
				found := false
				for _, g := range pendingFor(s, todo.ID) {
					if g.Kind == sigOwnerChoice && strings.Contains(g.Key, "undelivered|") && strings.Contains(g.Detail, words) {
						found = true
					}
				}
				if !found {
					t.Fatal("no pending record for supervisor follow-up")
				}
			}
			want := i18n.T(s.botLang(ctx), wantKey, i18n.Args{"option": words})
			if note := acceptedNote(s.botLang(ctx), got); note != want {
				t.Errorf("note: %q, want %q", note, want)
			}
			mu.Lock()
			updated := append([]string(nil), cards...)
			mu.Unlock()
			if len(updated) == 0 || !strings.Contains(updated[len(updated)-1], want) {
				t.Errorf("updated card lacks %q: %v", want, updated)
			}
			// Reload through the acceptance book's normal reader, as a restarted service does.
			s.accept.mu.Lock()
			s.accept.asks = nil
			s.accept.loaded = false
			s.accept.mu.Unlock()
			restored, ok := s.askCopy(todo.ID)
			if !ok {
				t.Fatal("stored ask was lost")
			}
			if card := s.conclusionCard(ctx, todo, restored).JSON(); !strings.Contains(card, want) {
				t.Errorf("restored card lacks %q: %s", want, card)
			}
			if again, err := reply(); again.taken || len(sentToAgent(f)) != before+sends {
				t.Fatalf("duplicate decision: %+v, %v", again, err)
			}
			s.waitWorkEvents()
		})
	}
}
