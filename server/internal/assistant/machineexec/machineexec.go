// Package machineexec is the machine_exec tool: one quick non-interactive shell command on one of the user's
// computers, run by its node (proto.MethodMachineExec, node/shellexec), within the owner's setting (SettingKey): on or
// off, which computers, and whether each command first waits for the user's approval on a card. The hub writes every
// call into the computer's audit as it relays it.
package machineexec

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// Tool is the tool's name, and its approval card's.
const Tool = "machine_exec"

// SettingKey is where the owner's Setting is kept.
const SettingKey = "assistant.machine_exec"

const (
	defaultTimeout = 20 * time.Second
	maxTimeout     = int(proto.MachineExecMaxTimeout / time.Second)
	// margin is how much longer than the command's timeout its node's answer is waited for: the node then stops the
	// command's group (a grace, a sweep), and the answer comes back through the hub.
	margin      = 15 * time.Second
	maxMachines = 32
)

// Setting is the owner's say over the tool. Machines are ids or names; none means every connected computer.
type Setting struct {
	Enabled  bool     `json:"enabled"`
	Confirm  bool     `json:"confirm"`
	Machines []string `json:"machines"`
}

// DefaultSetting is the setting the owner has not changed: on, no card, every computer.
func DefaultSetting() Setting { return Setting{Enabled: true, Machines: []string{}} }

// DecodeSetting reads a new value of the whole setting: each of its three keys, nothing else, no computer twice.
func DecodeSetting(raw json.RawMessage) (Setting, error) {
	bad := fmt.Errorf(`value must be the whole object {"enabled": true|false, "confirm": true|false, "machines": [ids `+
		`or names of computers, at most %d; [] for every one]}`, maxMachines)
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || len(keys) != 3 || keys["enabled"] == nil || keys["confirm"] == nil || keys["machines"] == nil {
		return Setting{}, bad
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var v Setting
	if dec.Decode(&v) != nil || len(v.Machines) > maxMachines {
		return Setting{}, bad
	}
	seen := map[string]bool{}
	for i, m := range v.Machines {
		m = strings.TrimSpace(m)
		if m == "" || seen[strings.ToLower(m)] {
			return Setting{}, bad
		}
		seen[strings.ToLower(m)], v.Machines[i] = true, m
	}
	if v.Machines == nil {
		v.Machines = []string{}
	}
	return v, nil
}

// allows says whether the setting lets the tool use m.
func (s Setting) allows(m Machine) bool {
	for _, n := range s.Machines {
		if strings.EqualFold(n, m.ID) || strings.EqualFold(n, m.Name) || m.SSHHost != "" && strings.EqualFold(n, m.SSHHost) {
			return true
		}
	}
	return len(s.Machines) == 0
}

// Caller is the conversation that called. Class is how the root classed it (the dispatch policy's classes: main,
// scheduled, background_supervisor, other), "" when the call is outside the bounds every one of the root's own tools
// has: an Agent, a guest conversation, an Inbox conversation, a work computer, a caller it cannot tell.
type Caller struct {
	Machine, Session, Class string
}

// refusal is why c may not run a command, "" when it may: the main conversation and the user's other conversations
// may, as they may call secret_run; a supervision round, which reads what Agents wrote without the user, may not.
func (c Caller) refusal() string {
	switch c.Class {
	case "main", "scheduled", "other":
		return ""
	case "background_supervisor":
		return "machine_exec does not run in a supervision round: hand the check to an Agent, or leave it to the main conversation."
	}
	return "machine_exec runs only in the main conversation and Persona's other conversations: an Agent runs commands in its own terminal."
}

// label names c in the hub's audit.
func (c Caller) label() string {
	return cmp.Or(c.Class, "unknown") + " conversation " + c.Session + " on " + c.Machine
}

// Machine is a computer as the root resolved it.
type Machine struct {
	ID, Name, SSHHost string
	Online            bool // its node is connected
}

// Confirmation is what the approval card shows: the computer, the command verbatim, its directory and timeout.
type Confirmation struct {
	Caller       Caller
	Machine      Machine
	Command, Cwd string
	Timeout      time.Duration
}

// Approved says whether r, the card's answer, lets the command run: the user's own Allow on it, not an answer given for
// them (interaction_answer, a default at a deadline) nor an allowance kept from before.
func Approved(r proto.Resolution) bool {
	return r.Allow != nil && *r.Allow && !r.Dismiss && r.AnsweredBy == "" && !r.Remembered
}

// Clock bounds the wait for the node.
type Clock interface {
	WithTimeout(context.Context, time.Duration) (context.Context, context.CancelFunc)
}

// Dependencies are what the root gives the tool.
type Dependencies struct {
	Setting func(context.Context) (Setting, error)
	// Resolve finds a computer by id or name (machineByName); Known lists their ids for an error.
	Resolve func(ctx context.Context, name string) (Machine, bool)
	Known   func(context.Context) string
	// Confirm shows the approval card and waits for its answer.
	Confirm func(context.Context, Confirmation) (proto.Resolution, error)
	// Call is machine.exec on the computer's node.
	Call  func(ctx context.Context, machine string, p proto.MachineExecParams, out *proto.MachineExecResult) error
	Clock Clock // the wall clock
}

// Domain is the tool.
type Domain struct{ d Dependencies }

func New(d Dependencies) *Domain {
	if d.Clock == nil {
		d.Clock = wallClock{}
	}
	return &Domain{d: d}
}

type args struct {
	Machine        string `json:"machine"`
	Command        string `json:"command"`
	Cwd            string `json:"cwd"`
	TimeoutSeconds *int   `json:"timeout_seconds"`
}

// Run is one call from c with args raw: the command's outcome as compact JSON, or why nothing ran (or what is known of
// it when the node did not answer).
func (x *Domain) Run(ctx context.Context, c Caller, raw json.RawMessage) (string, error) {
	if why := c.refusal(); why != "" {
		return "", errors.New(why)
	}
	a, timeout, err := decodeArgs(raw)
	if err != nil {
		return "", err
	}
	set, err := x.d.Setting(ctx)
	if err != nil {
		return "", fmt.Errorf("machine_exec's setting could not be read (%v); nothing ran", err)
	}
	if !set.Enabled {
		return "", errors.New("machine_exec is off, so nothing ran; the user turns it on with persona_settings (" + SettingKey + ").")
	}
	m, ok := x.d.Resolve(ctx, a.Machine)
	if !ok {
		return "", fmt.Errorf("no computer %q (computers: %s); nothing ran", a.Machine, x.d.Known(ctx))
	}
	name := cmp.Or(m.Name, m.ID)
	switch {
	case !set.allows(m):
		return "", fmt.Errorf("machine_exec may not use %s (%s allows %s); nothing ran. The user can add it with "+
			"persona_settings.", name, SettingKey, strings.Join(set.Machines, ", "))
	case !m.Online:
		return "", fmt.Errorf("%s is offline (its Persona node is not connected); nothing ran. machines_list shows which are up.", name)
	}
	if set.Confirm {
		r, err := x.d.Confirm(ctx, Confirmation{Caller: c, Machine: m, Command: a.Command, Cwd: a.Cwd, Timeout: timeout})
		if err != nil {
			return "", err
		}
		if !Approved(r) {
			return "", fmt.Errorf("the user did not approve the command on its card; nothing ran on %s", name)
		}
	}
	cctx, cancel := x.d.Clock.WithTimeout(ctx, timeout+margin)
	defer cancel()
	var res proto.MachineExecResult
	p := proto.MachineExecParams{Command: a.Command, Cwd: a.Cwd, TimeoutMS: timeout.Milliseconds(), Caller: c.label()}
	if err := x.d.Call(cctx, m.ID, p, &res); err != nil {
		return "", callError(name, err)
	}
	return outcome(name, res)
}

// decodeArgs checks a call before anything is asked, and gives its timeout.
func decodeArgs(raw json.RawMessage) (args, time.Duration, error) {
	var a args
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return a, 0, fmt.Errorf("invalid arguments: %v", err)
	}
	switch {
	case strings.TrimSpace(a.Machine) == "": // machineByName would take "" for a computer without an ssh alias
		return a, 0, errors.New("machine must name the computer: its id or name")
	case strings.TrimSpace(a.Command) == "":
		return a, 0, errors.New("command is empty")
	case len(a.Command) > proto.MachineExecMaxCommand || len(a.Cwd) > proto.MachineExecMaxCwd:
		return a, 0, fmt.Errorf("command is at most %d bytes and cwd %d: longer work goes to an Agent",
			proto.MachineExecMaxCommand, proto.MachineExecMaxCwd)
	case !shows(a.Command) || !shows(a.Cwd):
		return a, 0, errors.New("command and cwd must read as they run: no control characters but newlines and tabs, " +
			"no invisible or bidirectional ones")
	case a.TimeoutSeconds == nil:
		return a, defaultTimeout, nil
	case *a.TimeoutSeconds < 1 || *a.TimeoutSeconds > maxTimeout:
		return a, 0, fmt.Errorf("timeout_seconds must be 1 to %d", maxTimeout)
	}
	return a, time.Duration(*a.TimeoutSeconds) * time.Second, nil
}

// shows says whether s reads as it is, on a card and in the audit: valid UTF-8 with no control character but a newline
// or a tab, and no format character (zero width, bidirectional), line or paragraph separator, private-use or
// unassigned code point.
func shows(s string) bool {
	for _, r := range s {
		if r != '\n' && r != '\t' && (r == utf8.RuneError || unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp, unicode.Co, unicode.Cn)) {
			return false
		}
	}
	return true
}

// callError is what the model is told of a failed node call to computer name.
func callError(name string, err error) error {
	var re *rpc.Error
	switch {
	case strings.Contains(err.Error(), "unknown method"):
		return fmt.Errorf("%s's persona-node is older than machine_exec; run machine_redeploy first", name)
	case errors.As(err, &re) && re.Code == assistantwire.CodeMachineOffline:
		return fmt.Errorf("%s is offline (its Persona node is not connected); nothing ran", name)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%s's node did not answer in time: the command may have run, and its node stops it at its "+
			"timeout", name)
	}
	return fmt.Errorf("%s: %v", name, err)
}

// outcome is the tool's result: the node's answer as compact JSON, without what is empty.
func outcome(name string, r proto.MachineExecResult) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // output with < > & reads as written
	err := enc.Encode(struct {
		Machine       string `json:"machine"`
		ExitCode      int    `json:"exit_code"`
		TimedOut      bool   `json:"timed_out,omitempty"`
		DurationMS    int64  `json:"duration_ms"`
		Stdout        string `json:"stdout"`
		Stderr        string `json:"stderr,omitempty"`
		StdoutOmitted int64  `json:"stdout_omitted_bytes,omitempty"`
		StderrOmitted int64  `json:"stderr_omitted_bytes,omitempty"`
	}{name, r.ExitCode, r.TimedOut, r.DurationMS, r.Stdout, r.Stderr, r.StdoutOmitted, r.StderrOmitted})
	return strings.TrimSuffix(b.String(), "\n"), err
}

type wallClock struct{}

func (wallClock) WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}
