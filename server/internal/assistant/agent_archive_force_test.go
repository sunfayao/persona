package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestForceArchiveHTTPAndQuestionBoundary(t *testing.T) {
	ft, row := closeTestService(t, func(s *proto.Session) { s.Status = proto.StatusBlocked; s.State = proto.StateNeedsYou })
	ix := proto.Interaction{ID: "waiting-question", MachineID: "mac", SessionID: row.SessionID, Kind: proto.IxQuestion}
	ft.s.ixMapLocked()[ix.ID] = &pending{ix: ix}
	_, err := archiveTool(t, ft, "agent_archive", map[string]any{"machine": "mac", "session_id": row.SessionID, "force": true})
	if err == nil || len(ft.d.callsTo("mac", "session.close")) != 0 {
		t.Fatal("force archive bypassed an ordinary question")
	}
	ft.s.finishInteraction(ix.ID)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/?force=1", nil)
	r.SetPathValue("sid", row.SessionID)
	r.Header.Set(RoleHeader, roleFull)
	ft.s.setAgentSessionArchive(w, r, "mac", true)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"closed":true`) {
		t.Fatalf("forced HTTP close: %d %s", w.Code, w.Body)
	}
	if !archivedIDs(t, ft.s)[row.SessionID] {
		t.Fatal("HTTP close did not retain archive")
	}
	row.Status, row.State = proto.StatusWorking, proto.StateWorking
	ft.d.setSessions("mac", []proto.Session{row})
	if archivedIDs(t, ft.s)[row.SessionID] {
		t.Fatal("new work did not restore the archived session")
	}
}

func TestForceArchiveKeepsOtherBusyGuards(t *testing.T) {
	for name, edit := range map[string]func(*proto.Session){
		"working":            func(s *proto.Session) { s.Status = proto.StatusWorking; s.State = proto.StateWorking },
		"starting":           func(s *proto.Session) { s.Starting = true },
		"background":         func(s *proto.Session) { s.BackgroundWorking = true },
		"background waiting": func(s *proto.Session) { s.BackgroundWaiting = true },
		"background tasks":   func(s *proto.Session) { s.Background = []proto.BackgroundTask{{ID: "job"}} },
		"workflow":           func(s *proto.Session) { s.Workflows = []proto.Workflow{{Running: true}} },
		"subagent":           func(s *proto.Session) { s.Subagents = []proto.Subagent{{Running: true}} },
		"loop":               func(s *proto.Session) { s.Loop = &proto.LoopInfo{} },
		"queued":             func(s *proto.Session) { s.Queued = []string{"next"} },
		"server":             func(s *proto.Session) { s.Ports = []proto.Port{{Port: 5173}} },
		"assistant":          func(s *proto.Session) { s.Assistant = true },
	} {
		t.Run(name, func(t *testing.T) {
			ft, row := closeTestService(t, func(s *proto.Session) { s.Status = proto.StatusBlocked; s.State = proto.StateNeedsYou; edit(s) })
			_, err := archiveTool(t, ft, "agent_archive", map[string]any{"machine": "mac", "session_id": row.SessionID, "force": true})
			if err == nil {
				t.Fatal("force accepted unsafe state")
			}
			if len(ft.d.callsTo("mac", "session.close")) != 0 || len(archivedIDs(t, ft.s)) != 0 {
				t.Fatal("refusal changed the Agent")
			}
		})
	}
}

func TestForceArchiveCloseFailureAndHTTPRole(t *testing.T) {
	ft, row := closeTestService(t, func(s *proto.Session) { s.Status = proto.StatusBlocked; s.State = proto.StateNeedsYou })
	ft.d.machines["mac"].call = func(string, json.RawMessage) (any, error) { return nil, errors.New("close failed") }
	for _, role := range []string{roleAnswer, roleFull} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/?force=1", nil)
		r.SetPathValue("sid", row.SessionID)
		r.Header.Set(RoleHeader, role)
		ft.s.setAgentSessionArchive(w, r, "mac", true)
		if role == roleAnswer && w.Code != http.StatusForbidden {
			t.Fatalf("answer-only: %d", w.Code)
		}
		if role == roleFull && w.Code == http.StatusOK {
			t.Fatal("failed close claimed success")
		}
	}
	if len(archivedIDs(t, ft.s)) != 0 {
		t.Fatal("failed close hid the blocked Agent")
	}
}

func TestForceArchiveFenceWriteFailureKeepsAgent(t *testing.T) {
	ft, row := closeTestService(t, func(s *proto.Session) { s.Status = proto.StatusBlocked; s.State = proto.StateNeedsYou })
	store := &archiveFailStore{fleetMem: ft.st, failWrite: true}
	ft.s.store = func() Store { return store }
	ix := proto.Interaction{ID: "keep-card", MachineID: "mac", SessionID: row.SessionID, Kind: proto.IxScreen}
	ft.s.ixMapLocked()[ix.ID] = &pending{ix: ix}
	if err := ft.s.forceArchiveConfirmation(context.Background(), "mac", row.SessionID); err == nil {
		t.Fatal("failed fence write claimed success")
	}
	if len(ft.d.callsTo("mac", "session.close")) != 0 || len(ft.s.pendingInteractions()) != 1 || archivedIDs(t, ft.s)[row.SessionID] {
		t.Fatal("failed fence write touched Agent or card")
	}
}

// #4941: a forced archive of a confirmation-blocked Agent denies its permission and plan cards and dismisses its screen
// card, approving nothing; a working Agent with the same cards is refused and keeps them.
func TestForceArchiveCardOutcomes(t *testing.T) {
	for _, status := range []string{proto.StatusBlocked, proto.StatusWorking} {
		t.Run(status, func(t *testing.T) {
			ft, row := closeTestService(t, func(s *proto.Session) {
				s.Status, s.State = status, proto.StateNeedsYou
				if status == proto.StatusWorking {
					s.State = proto.StateWorking
				}
			})
			got := map[string]proto.Resolution{}
			for _, kind := range []string{proto.IxPermission, proto.IxPlan, proto.IxScreen} {
				ix := proto.Interaction{ID: "card-" + kind, MachineID: "mac", SessionID: row.SessionID, Kind: kind, ExpiresAt: time.Now().Add(time.Hour)}
				ft.s.ixMapLocked()[ix.ID] = &pending{ix: ix, local: func(_ context.Context, r proto.Resolution) error {
					got[kind] = r
					return nil
				}}
			}
			_, err := archiveTool(t, ft, "agent_archive", map[string]any{"machine": "mac", "session_id": row.SessionID, "force": true})
			if status == proto.StatusWorking {
				if err == nil || len(ft.d.callsTo("mac", "session.close")) != 0 || len(got) != 0 || len(ft.s.pendingInteractions()) != 3 {
					t.Fatalf("working Agent: err %v, closes %d, resolved %+v", err, len(ft.d.callsTo("mac", "session.close")), got)
				}
				return
			}
			if err != nil {
				t.Fatalf("force archive: %v", err)
			}
			no := false
			want := map[string]proto.Resolution{
				proto.IxPermission: {Allow: &no},
				proto.IxPlan:       {Approve: &no},
				proto.IxScreen:     {Dismiss: true},
			}
			for kind, w := range want {
				r, ok := got[kind]
				if !ok {
					t.Fatalf("%s card left unresolved: %+v", kind, got)
				}
				if r.Dismiss != w.Dismiss || (r.Allow == nil) != (w.Allow == nil) || r.Allow != nil && *r.Allow ||
					(r.Approve == nil) != (w.Approve == nil) || r.Approve != nil && *r.Approve || r.Always || r.Mode != "" || r.Option != "" || len(r.Keys) != 0 {
					t.Fatalf("%s card resolved as %+v", kind, r)
				}
			}
			if len(ft.s.pendingInteractions()) != 0 {
				t.Fatal("cards remain pending")
			}
		})
	}
}
