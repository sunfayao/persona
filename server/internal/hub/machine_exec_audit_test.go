package hub

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// execNode is a node answering machine.exec with answer or err; it notes how many audit rows there were when asked.
type execNode struct {
	database *db.DB
	answer   proto.MachineExecResult
	err      error
	mu       sync.Mutex
	rows     []int
}

func (n *execNode) Call(ctx context.Context, method string, params, out any) error {
	rows, _ := n.database.MachineAudits(ctx, "exec-box")
	n.mu.Lock()
	n.rows = append(n.rows, len(rows))
	n.mu.Unlock()
	if n.err != nil {
		return n.err
	}
	b, _ := json.Marshal(n.answer)
	return json.Unmarshal(b, out)
}

func (n *execNode) Notify(string, any) error { return nil }
func (n *execNode) Done() <-chan struct{}    { return make(chan struct{}) }
func (n *execNode) Close(error)              {}

// A machine.exec a manager relays is in the computer's audit: who asked, the command scrubbed and the directory and
// timeout before the node is asked, then the exit code and duration, or the error; never the output.
func TestMachineExecIsAudited(t *testing.T) {
	h, database := trustHub(t, MachineConfig{ID: "exec-box", Name: "Exec Box", Kind: "ssh"})
	ctx := context.Background()
	node := &execNode{database: database, answer: proto.MachineExecResult{ExitCode: 3, DurationMS: 42, Stdout: "out-a1b2c3"}}
	earlier, _ := database.MachineAudits(ctx, "exec-box") // the rows of an earlier run in this database
	m := h.localMachine("exec-box")
	m.mu.Lock()
	m.peer = node
	m.mu.Unlock()
	call := func(command string) (json.RawMessage, error) {
		params, _ := json.Marshal(proto.MachineExecParams{Command: command, Cwd: "~/src", TimeoutMS: 20000, Caller: "main conversation s1"})
		return assistantLink{h}.DeviceCall(ctx, assistantwire.DeviceCall{Device: "exec-box", Method: proto.MethodMachineExec, Params: params})
	}
	out, err := call("curl -H 'Authorization: Bearer 0123456789abcdef0123' https://example.com/x")
	if err != nil || !strings.Contains(string(out), "out-a1b2c3") {
		t.Fatalf("relayed %s, %v", out, err)
	}
	node.err = &rpc.Error{Message: "unknown method machine.exec"}
	if _, err := call("uptime"); err == nil || !strings.Contains(err.Error(), "unknown method") {
		t.Fatalf("the node's error came back as %v", err)
	}

	rows, err := database.MachineAudits(ctx, "exec-box")
	if err != nil {
		t.Fatal(err)
	}
	rows = rows[len(earlier):]
	var all []string
	details := make([]map[string]any, len(rows))
	for i, r := range rows {
		all = append(all, r.Action+" "+r.Actor+" "+string(r.Detail))
		_ = json.Unmarshal(r.Detail, &details[i])
	}
	if len(rows) != 4 || rows[0].Action != "exec" || rows[1].Action != "exec_result" || rows[2].Action != "exec" ||
		rows[3].Action != "exec_result" || rows[0].Actor != "manager:"+assistantwire.AssistantID {
		t.Fatalf("audit:\n%s", strings.Join(all, "\n"))
	}
	run, ran, failed := details[0], details[1], details[3]
	if run["caller"] != "main conversation s1" || run["cwd"] != "~/src" || run["timeout_ms"] != 20000.0 ||
		!strings.Contains(run["command"].(string), "https://<host>/x") || run["ref"] != ran["ref"] {
		t.Errorf("the exec row: %v", run)
	}
	if ran["exit_code"] != 3.0 || ran["duration_ms"] != 42.0 || ran["timed_out"] != false {
		t.Errorf("its result row: %v", ran)
	}
	if details[2]["command"] != "uptime" || failed["error"] != "unknown method machine.exec" || failed["exit_code"] != nil {
		t.Errorf("the failed one's rows: %v, %v", details[2], failed)
	}
	for _, never := range []string{"0123456789abcdef0123", "example.com", "out-a1b2c3"} {
		if text := strings.Join(all, "\n"); strings.Contains(text, never) {
			t.Errorf("audit holds %q:\n%s", never, text)
		}
	}
	if len(node.rows) != 2 || node.rows[0] != len(earlier)+1 {
		t.Errorf("the node was asked with %v audit rows written, want the exec row first (%d before)", node.rows, len(earlier))
	}
}
