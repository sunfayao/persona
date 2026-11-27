package devices

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// connect makes one connection through the machine's own hub.json entry and serves it until it ends (tests drive a
// machine this way, without the run loop).
func (m *Machine) connect(ctx context.Context) error {
	c, err := m.establish(ctx, cfgPath(m))
	if err != nil {
		return err
	}
	err = m.serve(ctx, c)
	c.close()
	m.detach(err, false)
	return err
}

// sshDown is ssh failing the way it does to a machine behind NAT.
func sshDown(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", "echo 'ssh: connect to host box port 22: Operation timed out' >&2; exit 255")
}

// tailcatMachine is a machine reachable both ways, with its timings shortened.
func tailcatMachine(h *Devices, retry, probe, check time.Duration) *Machine {
	m := newMachine(h, MachineConfig{ID: "box", Name: "Box", Kind: "ssh", SSHHost: "box", TailcatAddr: "tc-test"})
	m.timings = tailcatTimings{directRetry: retry, directProbeEvery: probe, checkEvery: check}
	return m
}

// A machine without a tailcat address is ssh only, as before; kind tailcat never tries ssh.
func TestTransports(t *testing.T) {
	h := newTestDevices(t)
	for _, c := range []struct {
		cfg  MachineConfig
		want string
	}{
		{MachineConfig{ID: "a", Kind: "local"}, "local"},
		{MachineConfig{ID: "a", Kind: "ssh", SSHHost: "a"}, "ssh"},
		{MachineConfig{ID: "a", Kind: "ssh", SSHHost: "a", TailcatAddr: "tc"}, "ssh,tailcat"},
		{MachineConfig{ID: "a", Kind: "tailcat", TailcatAddr: "tc"}, "tailcat"},
	} {
		if got := strings.Join(newMachine(h, c.cfg).transports(), ","); got != c.want {
			t.Errorf("%+v: %s, want %s", c.cfg, got, c.want)
		}
	}
}

// Both ways failing: the machine's error says why for each.
func TestBothWaysFailing(t *testing.T) {
	h := newTestDevices(t)
	m := tailcatMachine(h, time.Hour, time.Hour, time.Hour)
	m.nodeCmd = sshDown
	m.dialTailcat = func(context.Context) (net.Conn, error) { return nil, errors.New("no answer") }
	err := m.connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ssh：ssh: connect to host box") || !strings.Contains(err.Error(), "tailcat：no answer") {
		t.Fatalf("error %v, want both reasons", err)
	}
	if s := m.snapshot(); s.Online || s.Transport != "" {
		t.Fatalf("online=%v transport=%q after both failed", s.Online, s.Transport)
	}
}
