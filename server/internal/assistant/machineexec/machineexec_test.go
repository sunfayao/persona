package machineexec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// root is the root's side as a test plays it: one computer "mac" named "Mac mini", a node answering answer or err.
type root struct {
	mu       sync.Mutex
	setting  Setting
	online   bool
	card     proto.Resolution
	answer   proto.MachineExecResult
	err      error
	cards    []Confirmation
	calls    []proto.MachineExecParams
	timeouts []time.Duration
}

func newRoot() *root { return &root{setting: DefaultSetting(), online: true} }

func (r *root) domain() *Domain {
	return New(Dependencies{
		Setting: func(context.Context) (Setting, error) { return r.setting, nil },
		Resolve: func(_ context.Context, name string) (Machine, bool) {
			m := Machine{ID: "mac", Name: "Mac mini", SSHHost: "mini", Online: r.online}
			return m, name == m.ID || strings.EqualFold(name, m.Name)
		},
		Known: func(context.Context) string { return "server, mac" },
		Confirm: func(_ context.Context, c Confirmation) (proto.Resolution, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.cards = append(r.cards, c)
			return r.card, nil
		},
		Call: func(ctx context.Context, machine string, p proto.MachineExecParams, out *proto.MachineExecResult) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.calls = append(r.calls, p)
			if machine != "mac" {
				return errors.New("not the computer asked for")
			}
			*out = r.answer
			return r.err
		},
		Clock: r,
	})
}

func (r *root) WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	r.mu.Lock()
	r.timeouts = append(r.timeouts, d)
	r.mu.Unlock()
	return context.WithCancel(ctx)
}

var mainCaller = Caller{Machine: "server", Session: "s1", Class: "main"}

func (r *root) run(t *testing.T, c Caller, args string) (string, error) {
	t.Helper()
	return r.domain().Run(context.Background(), c, json.RawMessage(args))
}

// The node's answer comes back as compact JSON with the computer's name, the empty parts left out; the node is asked
// with the timeout (20 s unless given) and waited for a margin longer, and the hub is told who asked.
func TestARunReturnsTheNodesAnswerAsCompactJSON(t *testing.T) {
	t.Parallel()
	r := newRoot()
	r.answer = proto.MachineExecResult{ExitCode: 3, DurationMS: 12, Stdout: "a < b && c\n", StderrOmitted: 0}
	out, err := r.run(t, mainCaller, `{"machine":"Mac mini","command":"test -d x && ls","cwd":"~/src"}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"machine":"Mac mini","exit_code":3,"duration_ms":12,"stdout":"a < b && c\n"}`; out != want {
		t.Fatalf("result %s, want %s", out, want)
	}
	p := r.calls[0]
	if p.Command != "test -d x && ls" || p.Cwd != "~/src" || p.TimeoutMS != 20000 || p.Caller != "main conversation s1 on server" {
		t.Fatalf("asked the node %+v", p)
	}
	if r.timeouts[0] != 20*time.Second+margin {
		t.Fatalf("waited %s for the node", r.timeouts[0])
	}

	r.answer = proto.MachineExecResult{ExitCode: -1, TimedOut: true, DurationMS: 3000, Stderr: "e", StdoutOmitted: 5, StderrOmitted: 7}
	out, err = r.run(t, mainCaller, `{"machine":"mac","command":"sleep 9","timeout_seconds":1}`)
	want := `{"machine":"Mac mini","exit_code":-1,"timed_out":true,"duration_ms":3000,"stdout":"","stderr":"e","stdout_omitted_bytes":5,"stderr_omitted_bytes":7}`
	if err != nil || out != want || r.calls[1].TimeoutMS != 1000 {
		t.Fatalf("result %s, %v; asked %+v", out, err, r.calls[1])
	}
}

// A node that does not know machine.exec, or one gone offline, says so; any other refusal of the node is passed on.
func TestTheNodesFailuresAreToldPlainly(t *testing.T) {
	t.Parallel()
	for err, want := range map[error]string{
		&rpc.Error{Message: "unknown method machine.exec"}:                          "Mac mini's persona-node is older than machine_exec; run machine_redeploy first",
		&rpc.Error{Code: assistantwire.CodeMachineOffline, Message: "mac: offline"}: "Mac mini is offline (its Persona node is not connected); nothing ran",
		errors.New("machine.exec: refused, nothing ran: \"hub.json\" names …"):      "Mac mini: machine.exec: refused, nothing ran: \"hub.json\" names …",
		context.DeadlineExceeded: "Mac mini's node did not answer in time: the command may have run, and its node stops it at its timeout",
	} {
		r := newRoot()
		r.err = err
		if _, got := r.run(t, mainCaller, `{"machine":"mac","command":"uptime"}`); got == nil || got.Error() != want {
			t.Errorf("%v: told %v, want %s", err, got, want)
		}
	}
}

// An offline or unknown computer runs nothing, and the node is not asked.
func TestAnOfflineOrUnknownComputerRunsNothing(t *testing.T) {
	t.Parallel()
	r := newRoot()
	r.online = false
	if _, err := r.run(t, mainCaller, `{"machine":"mac","command":"uptime"}`); err == nil || !strings.Contains(err.Error(), "Mac mini is offline") {
		t.Fatalf("offline: %v", err)
	}
	if _, err := r.run(t, mainCaller, `{"machine":"box","command":"uptime"}`); err == nil || !strings.Contains(err.Error(), `no computer "box" (computers: server, mac)`) {
		t.Fatalf("unknown: %v", err)
	}
	if len(r.calls) != 0 {
		t.Fatalf("the node was asked: %+v", r.calls)
	}
}

// The main conversation and the user's other conversations may run a command, as they may call secret_run; a
// supervision round may not, nor anything outside the bounds of the conversations' own tools (an Agent, a guest, an
// Inbox conversation), and their refusal says so before anything else is looked at.
func TestOnlyTheMainConversationAndTheUsersOtherConversationsMayRun(t *testing.T) {
	t.Parallel()
	for class, refused := range map[string]string{"main": "", "other": "", "scheduled": "",
		"background_supervisor": "does not run in a supervision round", "": "an Agent runs commands in its own terminal"} {
		r := newRoot()
		_, err := r.run(t, Caller{Machine: "server", Session: "s2", Class: class}, `{"bad":1}`)
		if refused == "" {
			if err == nil || !strings.Contains(err.Error(), "invalid arguments") {
				t.Errorf("%q: %v, want its arguments looked at", class, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), refused) || strings.Contains(err.Error(), "the assistant") {
			t.Errorf("%q: %v, want refused", class, err)
		}
	}
}

// The setting: off refuses and says where it is turned on; a list of computers keeps the others out; with confirm, the
// command runs only once the user approved it on its card, which shows the computer and the command as written.
func TestTheSettingDecidesWhatRuns(t *testing.T) {
	t.Parallel()
	const args = `{"machine":"mac","command":"brew services list | grep -i redis","cwd":"/opt"}`
	r := newRoot()
	r.setting.Enabled = false
	if _, err := r.run(t, mainCaller, args); err == nil || !strings.Contains(err.Error(), "persona_settings ("+SettingKey+")") {
		t.Fatalf("off: %v", err)
	}
	r = newRoot()
	r.setting.Machines = []string{"server"}
	if _, err := r.run(t, mainCaller, args); err == nil || !strings.Contains(err.Error(), "may not use Mac mini") {
		t.Fatalf("not listed: %v", err)
	}
	r.setting.Machines = []string{"server", "MINI"} // its ssh alias, any case
	if _, err := r.run(t, mainCaller, args); err != nil {
		t.Fatalf("listed: %v", err)
	}

	yes, no := true, false
	for _, tc := range []struct {
		name string
		card proto.Resolution
		runs bool
	}{
		{"approved", proto.Resolution{Allow: &yes}, true},
		{"declined", proto.Resolution{Allow: &no}, false},
		{"dismissed or expired", proto.Resolution{Dismiss: true}, false},
		{"answered for the user", proto.Resolution{Allow: &yes, AnsweredBy: proto.AnsweredByAssistant}, false},
		{"kept from before", proto.Resolution{Allow: &yes, Remembered: true}, false},
	} {
		r := newRoot()
		r.setting.Confirm, r.card = true, tc.card
		_, err := r.run(t, mainCaller, args)
		if len(r.cards) != 1 || r.cards[0].Command != "brew services list | grep -i redis" || r.cards[0].Cwd != "/opt" ||
			r.cards[0].Machine.ID != "mac" || r.cards[0].Caller != mainCaller || r.cards[0].Timeout != defaultTimeout {
			t.Fatalf("%s: the card %+v", tc.name, r.cards)
		}
		if ran := len(r.calls) == 1; ran != tc.runs || ran != (err == nil) {
			t.Errorf("%s: ran %v, err %v", tc.name, ran, err)
		}
	}
}

// A new value of the setting is the whole object: each key, nothing else, no computer twice.
func TestDecodeSetting(t *testing.T) {
	t.Parallel()
	for raw, ok := range map[string]bool{
		`{"enabled":true,"confirm":false,"machines":[]}`:              true,
		`{"enabled":false,"confirm":true,"machines":[" mac ","box"]}`: true,
		`{"enabled":true,"confirm":false,"machines":null}`:            true,
		`{"enabled":true,"confirm":false}`:                            false,
		`{"enabled":true,"confirm":false,"machines":[],"x":1}`:        false,
		`{"enabled":"yes","confirm":false,"machines":[]}`:             false,
		`{"enabled":true,"confirm":false,"machines":["mac","MAC"]}`:   false,
		`{"enabled":true,"confirm":false,"machines":[""]}`:            false,
		`[]`: false,
	} {
		v, err := DecodeSetting(json.RawMessage(raw))
		if (err == nil) != ok || ok && v.Machines == nil {
			t.Errorf("%s: %+v, %v", raw, v, err)
		}
	}
	if v, _ := DecodeSetting(json.RawMessage(`{"enabled":false,"confirm":true,"machines":[" mac "]}`)); v.Enabled || !v.Confirm || v.Machines[0] != "mac" {
		t.Fatalf("decoded %+v", v)
	}
}

// What a call may not be is refused before anything is asked: an unknown argument, no computer or command, a command
// that would not read as it runs, a timeout outside 1 to 120 seconds.
func TestArgumentsAreCheckedFirst(t *testing.T) {
	t.Parallel()
	for _, args := range []string{
		`{"machine":"mac","command":"ls","sudo":true}`,
		`{"machine":" ","command":"ls"}`,
		`{"machine":"mac","command":" "}`,
		`{"machine":"mac","command":"echo ‮abc"}`,
		`{"machine":"mac","command":"echo \u0007"}`,
		`{"machine":"mac","command":"ls","cwd":"~/a​b"}`,
		`{"machine":"mac","command":"ls","timeout_seconds":0}`,
		`{"machine":"mac","command":"ls","timeout_seconds":121}`,
		`{"machine":"mac","command":"` + strings.Repeat("x", proto.MachineExecMaxCommand+1) + `"}`,
	} {
		r := newRoot()
		r.setting.Confirm = true
		if _, err := r.run(t, mainCaller, args); err == nil || len(r.cards)+len(r.calls) != 0 {
			t.Errorf("%.60s: err %v, cards %d, calls %d", args, err, len(r.cards), len(r.calls))
		}
	}
	r := newRoot()
	if _, err := r.run(t, mainCaller, "{\"machine\":\"mac\",\"command\":\"for f in *; do\\n\\techo $f\\ndone\",\"timeout_seconds\":120}"); err != nil {
		t.Fatalf("a command of several lines: %v", err)
	}
}
