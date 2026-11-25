package assistant

import (
	"sync"
)

// supAgent is the Agent the tests drive: its id is long enough for agent_supervise to name it.
const supAgent = "s1agent0"

// supervisedEnv is a manager whose assistant runs on "server" with the main conversation conv1, the leader conversation
// lead1, the side conversation side1 and the background supervisor bg1 (on unless background is false: then the main
// conversation follows its own tasks up). Tasks are not quiet (main_tasks_quiet off), so nothing but supervision keeps
// a finished turn from the pushes. The Lark bot is paired with its pushes switch on, and the Agent supAgent is listed on mac.
// logs keeps what the assistant logged.
type supervisedEnv struct {
	*fleetTest
	mu   sync.Mutex
	logs []string
}
