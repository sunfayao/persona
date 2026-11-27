package assistant

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestCompletionChildrenUseScopedAncestry(t *testing.T) {
	ft := newFleetTestNoStore(t)
	root := proto.Session{SessionID: "home-a-root", SourceThreadID: "raw-root"}
	for _, tc := range []struct {
		name string
		list []proto.Session
		want bool
	}{
		{"nested", []proto.Session{{SessionID: "middle", ParentSessionID: root.SessionID}, {SessionID: "child", ParentSessionID: "middle", Status: proto.StatusWorking}}, true},
		{"missing intermediate", []proto.Session{{SessionID: "child", ParentSessionID: "missing", RootSessionID: root.SessionID, Status: proto.StatusWorking}}, true},
		{"child approval", []proto.Session{{SessionID: "child", ParentSessionID: root.SessionID, Status: proto.StatusBlocked}}, true},
		{"other home", []proto.Session{{SessionID: "child", ParentSessionID: "home-b-root", ParentThreadID: "raw-root", Status: proto.StatusWorking}}, false},
		{"cycle", []proto.Session{{SessionID: "a", ParentSessionID: "b", Status: proto.StatusWorking}, {SessionID: "b", ParentSessionID: "a"}}, false},
		{"finished", []proto.Session{{SessionID: "child", ParentSessionID: root.SessionID, Status: proto.StatusDone}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ft.d.setSessions("mac", append([]proto.Session{root}, tc.list...))
			if got := ft.s.completionChildrenRunning("mac", root); got != tc.want {
				t.Fatalf("running children = %v, want %v", got, tc.want)
			}
		})
	}
}
