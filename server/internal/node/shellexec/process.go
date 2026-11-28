package shellexec

import (
	"fmt"
	"os/exec"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/osproc"
)

// waitDelay is how long Wait reads a command's output after its shell exited: a child that left the group (setsid,
// a daemon) and kept the output open does not hold the call beyond it.
const waitDelay = 2 * time.Second

// processStarter starts real processes, each in a session and process group of its own: osproc.StopGroup reaches all
// of it, and with no controlling terminal a login shell cannot be stopped reaching for one (#3265). Stdin is
// /dev/null.
type processStarter struct{}

func (processStarter) Start(s Spec) (Process, error) {
	cmd := exec.Command(s.Path, s.Args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = s.Dir, s.Env, s.Stdout, s.Stderr
	cmd.SysProcAttr = osproc.SessionGroupAttr()
	cmd.WaitDelay = waitDelay
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return process{cmd}, nil
}

type process struct{ cmd *exec.Cmd }

func (p process) Wait() (int, error) {
	err := p.cmd.Wait()
	if p.cmd.ProcessState == nil {
		return -1, err
	}
	return p.cmd.ProcessState.ExitCode(), err
}

func (p process) Terminate() error { return osproc.StopGroup(p.cmd.Process.Pid, false) }

func (p process) Kill(within time.Duration) {
	_ = osproc.StopGroup(p.cmd.Process.Pid, true)
	osproc.SweepGroup(p.cmd.Process.Pid, within)
}

func (p process) Alive() bool { return osproc.Alive(-p.cmd.Process.Pid) } // -pid: anyone in the group

// StreamCap is how much of a stream comes back: its first streamHead bytes and its last StreamCap-streamHead.
const (
	StreamCap  = 32 << 10
	streamHead = 8 << 10
	streamTail = StreamCap - streamHead
)

// capture is one stream of a command: its head, its end and how many bytes it was. exec writes to it from one
// goroutine; the lock is for the read after.
type capture struct {
	mu         sync.Mutex
	head, tail []byte
	n          int64
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n += int64(len(p))
	k := min(streamHead-len(c.head), len(p))
	c.head = append(c.head, p[:k]...)
	if c.tail = append(c.tail, p[k:]...); len(c.tail) > 2*streamTail { // moved down once per streamTail bytes at most
		c.tail = append(c.tail[:0], c.tail[len(c.tail)-streamTail:]...)
	}
	return len(p), nil
}

// result is the stream as it comes back: whole when it fits, else its head, a marker and its end, each cut on a
// character's boundary, and how many bytes were left out.
func (c *capture) result() (string, int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	head, tail := c.head, c.tail[max(0, len(c.tail)-streamTail):]
	if int64(len(head)+len(tail)) == c.n {
		return string(head) + string(tail), 0
	}
	for i := 1; i <= min(utf8.UTFMax, len(head)); i++ { // a character cut at the head's end
		if utf8.RuneStart(head[len(head)-i]) {
			if !utf8.FullRune(head[len(head)-i:]) {
				head = head[:len(head)-i]
			}
			break
		}
	}
	for i := 1; i < utf8.UTFMax && len(tail) > 0 && !utf8.RuneStart(tail[0]); i++ { // and at the tail's start
		tail = tail[1:]
	}
	omitted := c.n - int64(len(head)+len(tail))
	return fmt.Sprintf("%s\n[… %d bytes omitted …]\n%s", head, omitted, tail), omitted
}
