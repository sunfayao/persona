package devices

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/rpc"
)

// logLines collects what d logs.
type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

// with is the lines that contain every one of parts.
func (l *logLines) with(parts ...string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
next:
	for _, line := range l.lines {
		for _, p := range parts {
			if !strings.Contains(line, p) {
				continue next
			}
		}
		out = append(out, line)
	}
	return out
}

func TestCallResult(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{nil, callOK},
		{context.DeadlineExceeded, callDeadline},
		{fmt.Errorf("start: %w", context.DeadlineExceeded), callDeadline},
		{&rpc.Error{Message: "context deadline exceeded"}, callDeadline},
		{context.Canceled, callCanceled},
		{fmt.Errorf("m1: %w", errOffline), callOffline},
		{rpc.ErrClosed, callClosed},
		{rpc.ErrDropped, callClosed},
		{rpc.ErrSuspended, callClosed},
		{&rpc.Error{Message: "rpc: connection closed"}, callClosed},
		{&rpc.Error{Code: "terminal_not_ready", Message: "no"}, callError},
		{errors.New("boom"), callError},
	} {
		if got := callResult(c.err); got != c.want {
			t.Errorf("callResult(%v) = %s, want %s", c.err, got, c.want)
		}
	}
}

// A failed call always leaves a line with its machine, method, time and kind of failure; a quick one that worked
// does not (unless sampled), a session start always does (#7509).
func TestMachineCallLogsFailures(t *testing.T) {
	d := newTestDevices(t)
	var log logLines
	d.logf = log.logf
	m := newMachine(d, MachineConfig{ID: "m1", Kind: "ssh", SSHHost: "m1"})
	m.firstTried()
	err := m.call(context.Background(), "session.start", map[string]any{}, nil)
	if !errors.Is(err, errOffline) {
		t.Fatalf("call to an unconnected machine: %v", err)
	}
	lines := log.with("[m1] rpc call method=session.start result=offline took=", `err="`)
	if len(lines) != 1 {
		t.Fatalf("log: %q", log.lines)
	}

	// Offline calls: the first in a minute is logged, the rest not (a start always is); the next line after the minute
	// counts them.
	m.logCall("node.configure", errOffline, time.Millisecond, 1)
	m.logCall("usage.read", errOffline, time.Millisecond, 1)
	m.logCall("session.start", errOffline, time.Millisecond, 1)
	if len(log.with("result=offline")) != 3 || len(log.with("method=session.start result=offline")) != 2 {
		t.Fatalf("offline calls within the minute: %q", log.lines)
	}
	m.callLogMu.Lock()
	m.offlineLogged = time.Now().Add(-callOfflineEvery)
	m.callLogMu.Unlock()
	m.logCall("node.configure", errOffline, time.Millisecond, 1)
	if len(log.with("method=node.configure result=offline", "offline_not_logged=1")) != 1 {
		t.Fatalf("the offline line after the minute: %q", log.lines)
	}

	m.logCall("ping", nil, time.Millisecond, 1)
	if got := log.with("method=ping"); len(got) != 0 {
		t.Fatalf("a quick ping that worked was logged: %q", got)
	}
	m.logCall("session.start", nil, time.Second, 1)
	m.logCall("ping", nil, 3*time.Second, 1)
	if len(log.with("method=session.start result=ok took=1s")) != 1 || len(log.with("method=ping result=ok took=3s")) != 1 {
		t.Fatalf("a start or a slow call that worked was not logged: %q", log.lines)
	}
	m.logCall("session.start", rpc.ErrDropped, time.Minute, 2)
	if len(log.with("method=session.start result=closed took=1m0s sent=true tries=2", `err="rpc: connection closed"`)) != 1 {
		t.Fatalf("dropped start: %q", log.lines)
	}
	m.logCall("session.watch", context.Canceled, time.Millisecond, 1)
	if len(log.with("method=session.watch")) != 0 {
		t.Fatalf("a quick cancel was logged: %q", log.lines)
	}
	m.logCall("session.send", &rpc.Error{Code: "busy", Message: "the agent is busy"}, time.Millisecond, 1)
	if len(log.with("method=session.send result=error", "code=busy")) != 1 {
		t.Fatalf("an answered error: %q", log.lines)
	}
}

// An error text over the limit is cut on a whole character: a cut inside a Chinese character would put invalid UTF-8
// in the log line.
func TestOneLineErrCutsOnWholeCharacter(t *testing.T) {
	// 100 characters of 3 bytes each: the 300-byte limit lands on a boundary, so start with one ASCII byte to move it
	// inside a character.
	err := errors.New("x" + strings.Repeat("好", 100))
	got := oneLineErr(err)
	if !utf8.ValidString(got) {
		t.Fatalf("cut inside a character: %q", got)
	}
	if !strings.HasSuffix(got, "好…") || len(got) > 300+len("…") {
		t.Fatalf("got %d bytes: %q", len(got), got)
	}
}
