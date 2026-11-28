// Package shellexec runs machine.exec (proto.MethodMachineExec): one non-interactive command in the login shell of
// the user the node runs as, with an empty stdin, in a session and process group of its own. At the timeout, when the
// caller goes away, and when the shell exits leaving something running in its group, the whole group is stopped:
// SIGTERM, a short grace, then SIGKILL until it is empty, so nothing the command started outlives the call. Each
// stream keeps its head and its end. Nothing here logs the command or its output: at most the command's length and how
// it ended.
package shellexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// Clock is the time an Executor waits on: the timeout, and the grace between SIGTERM and SIGKILL.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

// Process is a started shell, the leader of a process group of its own.
type Process interface {
	// Wait waits for the shell to exit and its output to be read, and says its exit code (-1: a signal ended it).
	Wait() (int, error)
	// Terminate sends the group SIGTERM.
	Terminate() error
	// Kill sends the group SIGKILL until it is empty or within has passed.
	Kill(within time.Duration)
	// Alive says whether anything is left in the group.
	Alive() bool
}

// Starter starts what a Spec says.
type Starter interface {
	Start(Spec) (Process, error)
}

// Spec is one start: Path with Args, in Dir, with Env, stdin empty, the output into Stdout and Stderr.
type Spec struct {
	Path           string
	Args           []string
	Dir            string
	Env            []string
	Stdout, Stderr io.Writer
}

// Dependencies of an Executor. Every one but Home and DataDir has a default: the real one, or none.
type Dependencies struct {
	Home    string                 // the user's home: the default directory, and ~ and $HOME in a command
	DataDir string                 // the node's data directory: $PERSONA_HOME in a command
	GOOS    string                 // runtime.GOOS
	Env     []string               // the node's environment (os.Environ), which the command inherits
	Refused func(path string) bool // a path a command may not name (command.go); none
	Start   Starter                // real processes
	Clock   Clock                  // the wall clock
	Logf    func(string, ...any)
}

// Executor runs machine.exec.
type Executor struct{ d Dependencies }

const (
	// grace is how long the group has between SIGTERM and SIGKILL, poll how often it is looked at meanwhile, sweep how
	// long the SIGKILLs after it may go on (osproc.SweepGroup: a child forked while a signal goes out survives one).
	grace = 2 * time.Second
	poll  = 50 * time.Millisecond
	sweep = 2 * time.Second
)

// New is an Executor with d, reading the environment once, here.
func New(d Dependencies) *Executor {
	if d.GOOS == "" {
		d.GOOS = runtime.GOOS
	}
	if d.Env == nil {
		d.Env = os.Environ()
	}
	if d.Refused == nil {
		d.Refused = func(string) bool { return false }
	}
	if d.Start == nil {
		d.Start = processStarter{}
	}
	if d.Clock == nil {
		d.Clock = wallClock{}
	}
	if d.Logf == nil {
		d.Logf = func(string, ...any) {}
	}
	return &Executor{d: d}
}

// Run runs p's command and says how it ended. An error means it did not run (refused, or it could not start), or that
// the caller went away and it was stopped.
func (e *Executor) Run(ctx context.Context, p proto.MachineExecParams) (proto.MachineExecResult, error) {
	if e.d.GOOS == "windows" {
		return proto.MachineExecResult{}, errors.New("machine.exec: this computer runs Windows, which has no login shell " +
			"for it; hand the command to an Agent there")
	}
	dir, timeout, err := e.check(p)
	if err != nil {
		return proto.MachineExecResult{}, err
	}
	return e.run(ctx, p.Command, dir, timeout)
}

func (e *Executor) run(ctx context.Context, command, dir string, timeout time.Duration) (proto.MachineExecResult, error) {
	var stdout, stderr capture
	start := e.d.Clock.Now()
	proc, err := e.d.Start.Start(Spec{Path: e.shell(), Args: []string{"-lc", command}, Dir: dir, Env: e.env(),
		Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		return proto.MachineExecResult{}, fmt.Errorf("machine.exec: could not start the command in %s: %w", dir, err)
	}
	exited := make(chan int, 1)
	go func() {
		code, _ := proc.Wait() // ErrWaitDelay (a child that left the group kept the output open) changes no exit code
		exited <- code
	}()
	code, ended, timedOut, gone := -1, false, false, false
	select {
	case code = <-exited:
		ended = true
	case <-e.d.Clock.After(timeout):
		timedOut = true
	case <-ctx.Done():
		gone = true
	}
	if !ended || proc.Alive() { // the timeout, the caller gone, or what the shell left running in its group
		code, ended = e.stop(proc, exited, code, ended)
	}
	if !ended {
		code = <-exited // the group is gone: Wait returns once the output is read
	}
	res := proto.MachineExecResult{ExitCode: code, TimedOut: timedOut, DurationMS: e.d.Clock.Now().Sub(start).Milliseconds()}
	if timedOut {
		res.ExitCode = -1
	}
	res.Stdout, res.StdoutOmitted = stdout.result()
	res.Stderr, res.StderrOmitted = stderr.result()
	e.d.Logf("node: machine.exec: a %d-byte command: exit %d after %d ms (timed out: %v, caller gone: %v)",
		len(command), res.ExitCode, res.DurationMS, timedOut, gone)
	if gone {
		return proto.MachineExecResult{}, errors.New("machine.exec: the caller went away; the command was stopped")
	}
	return res, nil
}

// stop ends proc's group: SIGTERM, then until grace has passed the group may still end on it (looked at every poll),
// then SIGKILL until it is empty. It returns the shell's exit code and whether the shell has exited, as they now are.
func (e *Executor) stop(proc Process, exited <-chan int, code int, ended bool) (int, bool) {
	_ = proc.Terminate()
	until := e.d.Clock.Now().Add(grace)
	for e.d.Clock.Now().Before(until) && (!ended || proc.Alive()) {
		wait := exited
		if ended {
			wait = nil
		}
		select {
		case code = <-wait:
			ended = true
		case <-e.d.Clock.After(poll):
		}
	}
	proc.Kill(sweep)
	return code, ended
}

type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
