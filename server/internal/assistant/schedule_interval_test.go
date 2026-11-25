package assistant

import (
	"strconv"

	"github.com/nwverse/persona/server/internal/proto"
)

// Regression for #7619: an open supervised todo in a leader conversation must not shorten an interval
// follow-up. The task runs on its configured `every`, the views show that schedule, and the run prompt
// states the configured interval. The former leader cadence (#3811, #3870, #4014) fired such tasks every
// 10 minutes while a supervised todo was open.

// workingSupervisedTodo seeds an open todo supervised by "leader" with a working agent.
func workingSupervisedTodo(f *fakeHub, id string) Todo {
	f.setSessions("box", proto.Session{SessionID: "agent", Status: proto.StatusWorking, State: proto.StateWorking})
	return Todo{ID: id, Supervision: &Supervision{Supervisor: "leader"}, Agents: []TodoAgent{{MachineID: "box", SessionID: "agent"}}}
}

func ruleName(minutes int) string {
	return strconv.Itoa(minutes) + "m"
}
