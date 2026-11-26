package devices

// Machine lists report node clipboard support and the persisted sharing choice.

import (
	"context"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/nodeproto"
	"github.com/sunfayao/persona/server/internal/proto"
)

// clipboardKey is the settings key holding map[machineID]bool: the machines the user turned clipboard sharing on for.
const clipboardKey = "machine_clipboard"

// clipboardSwitches caches clipboardKey, loaded on first use (machine lists read it on every report).
type clipboardSwitches struct {
	mu       sync.Mutex
	loadedAt time.Time // zero: never loaded
	failedAt time.Time
	on       map[string]bool
}

// clipboardFresh is how long a load of the switches is trusted: another manager process on the same settings (a
// blue-green switch) may have turned one off, and an off switch must not stay on here for long.
const clipboardFresh = 10 * time.Second

// clipboardOn is whether the user turned clipboard sharing on for machine; off when never set, and off while the
// settings cannot be read (it fails closed).
func (d *Devices) clipboardOn(machine string) bool {
	c := &d.clipboard
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.loadedAt) >= clipboardFresh && (c.failedAt.IsZero() || time.Since(c.failedAt) >= engineRetry) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.loadClipboardLocked(ctx); err != nil {
			d.logf("clipboard switches: %v (retried in %s)", err, engineRetry)
			c.failedAt, c.on = time.Now(), nil // fails closed
		}
	}
	return c.on[machine]
}

func (d *Devices) loadClipboardLocked(ctx context.Context) error {
	c := &d.clipboard
	on := map[string]bool{}
	if d.opts.Store != nil {
		if _, err := d.opts.Store.Setting(ctx, clipboardKey, &on); err != nil {
			return err
		}
	}
	c.on, c.loadedAt, c.failedAt = on, time.Now(), time.Time{}
	return nil
}

// clipboardSupportLocked is what the node said it can do (m.mu held): an older node cannot share at all; a machine
// never reached has said nothing yet.
func (m *Machine) clipboardSupportLocked() (supported bool, reason string) {
	switch {
	case m.info.Clipboard != nil:
		return m.info.Clipboard.Supported, m.info.Clipboard.Reason
	case m.info.Version == "" && m.info.OS == "":
		return false, ""
	case m.info.Protocol > 0 && nodeHas(m.info, nodeproto.Clipboard):
		return true, "" // announced the methods without the field: trust them
	}
	return false, proto.ClipboardReasonNodeOutdated
}

func (m *Machine) clipboardStateLocked(enabled bool) *proto.MachineClipboard {
	supported, reason := m.clipboardSupportLocked()
	return &proto.MachineClipboard{Supported: supported, Enabled: enabled, ReasonCode: reason}
}
