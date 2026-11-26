package devices

import (
	"errors"
	"testing"
	"time"
)

// Settings that cannot be read are off.
func TestFileTransferSwitchFailsClosed(t *testing.T) {
	d := newTestDevices(t)
	if err := d.opts.Store.PutSetting(t.Context(), fileTransferKey, map[string]bool{"box": true}); err != nil {
		t.Fatal(err)
	}
	store := d.opts.Store.(*memStore)
	store.mu.Lock()
	store.fail = errors.New("database down")
	store.mu.Unlock()
	d.fileTransfer.mu.Lock()
	d.fileTransfer.loadedAt = time.Time{} // as when the cache is stale
	d.fileTransfer.mu.Unlock()
	if d.fileTransferOn("box") {
		t.Fatal("on while the settings cannot be read")
	}
}
