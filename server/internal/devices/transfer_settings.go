package devices

// The file transfer switch (#3065): each machine has one, off until the user turns it
// on. While it is off nothing is sent to the machine to keep (dest=inbox, which the hub checks against this switch as
// the manager reports it on the machine). The manager keeps the switches in settings (machine_file_transfer) and answers
// GET / PUT /api/v1/machines/{mid}/file-transfer/settings.

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// fileTransferKey is the settings key holding map[machineID]bool: the machines the user turned file transfer on for.
const fileTransferKey = "machine_file_transfer"

// fileTransferSwitches caches fileTransferKey, loaded on first use (machine lists read it on every report).
type fileTransferSwitches struct {
	mu       sync.Mutex
	loadedAt time.Time // zero: never loaded
	failedAt time.Time
	on       map[string]bool
}

// fileTransferFresh is how long a load of the switches is trusted: another manager process on the same settings (a
// blue-green switch) may have turned one off, and an off switch must not stay on here for long.
const fileTransferFresh = 10 * time.Second

// fileTransferOn is whether the user turned file transfer on for machine; off when never set, and off while the
// settings cannot be read (it fails closed).
func (d *Devices) fileTransferOn(machine string) bool {
	c := &d.fileTransfer
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.loadedAt) >= fileTransferFresh && (c.failedAt.IsZero() || time.Since(c.failedAt) >= engineRetry) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := d.loadFileTransferLocked(ctx); err != nil {
			d.logf("file transfer switches: %v (retried in %s)", err, engineRetry)
			c.failedAt, c.on = time.Now(), nil // fails closed
		}
	}
	return c.on[machine]
}

func (d *Devices) loadFileTransferLocked(ctx context.Context) error {
	c := &d.fileTransfer
	on := map[string]bool{}
	if d.opts.Store != nil {
		if _, err := d.opts.Store.Setting(ctx, fileTransferKey, &on); err != nil {
			return err
		}
	}
	c.on, c.loadedAt, c.failedAt = on, time.Now(), time.Time{}
	return nil
}

// fileTransferState is proto.Machine.FileTransfer: the switch, and the receive folder while it is on.
func (m *Machine) fileTransferState(enabled bool) *proto.MachineFileTransfer {
	st := &proto.MachineFileTransfer{Enabled: enabled}
	if enabled {
		st.Inbox = strings.TrimSpace(m.cfg.InboxDir)
		if st.Inbox == "" {
			st.Inbox = proto.DefaultInboxDir
		}
	}
	return st
}
