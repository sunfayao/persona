package devices

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/proto"
)

// File transfer is off for every machine until the user turns it on; the switch is kept in settings under
// machine_file_transfer, shown on the machine (file_transfer) and read back by GET.
func TestFileTransferSwitchDefaultsOffAndIsStored(t *testing.T) {
	d := newTestDevices(t)
	m := connectedMachine(t, d, "box", &testNode{})
	mux := d.Handler(http.NotFoundHandler())
	get := func() proto.MachineFileTransfer {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/machines/box/file-transfer/settings", nil))
		var st proto.MachineFileTransfer
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &st) != nil {
			t.Fatalf("GET %d %s", rec.Code, rec.Body)
		}
		return st
	}
	if st := get(); st.Enabled || st.Inbox != "" {
		t.Fatalf("default %+v", st)
	}
	if ft := m.snapshot().FileTransfer; ft == nil || ft.Enabled {
		t.Fatalf("machine %+v", ft)
	}
	put := func(body string, role string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/api/v1/machines/box/file-transfer/settings", strings.NewReader(body))
		if role != "" {
			req.Header.Set(assistantwire.RoleHeader, role)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := put(`{}`, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("no enabled: %d", rec.Code)
	}
	if rec := put(`{"enabled":true}`, "answer"); rec.Code != http.StatusForbidden {
		t.Fatalf("answer-only device: %d", rec.Code)
	}
	if rec := put(`{"enabled":true}`, "full"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"inbox":"~/Downloads/Persona"`) {
		t.Fatalf("on: %d %s", rec.Code, rec.Body)
	}
	if st := get(); !st.Enabled || st.Inbox != proto.DefaultInboxDir {
		t.Fatalf("after on %+v", st)
	}
	if ft := m.snapshot().FileTransfer; ft == nil || !ft.Enabled {
		t.Fatalf("machine after on %+v", ft)
	}
	var stored map[string]bool
	if ok, err := d.opts.Store.Setting(t.Context(), fileTransferKey, &stored); !ok || err != nil || !stored["box"] {
		t.Fatalf("stored %v %v %v", ok, err, stored)
	}
	m.cfg.InboxDir = "~/Inbox"
	if st := get(); st.Inbox != "~/Inbox" {
		t.Fatalf("configured inbox %+v", st)
	}
	if rec := put(`{"enabled":false}`, ""); rec.Code != 200 {
		t.Fatalf("off: %d", rec.Code)
	}
	if st := get(); st.Enabled {
		t.Fatalf("after off %+v", st)
	}
	stored = nil
	if ok, _ := d.opts.Store.Setting(t.Context(), fileTransferKey, &stored); !ok || stored["box"] {
		t.Fatalf("stored after off %v", stored)
	}
}

// Settings that cannot be read are off.
func TestFileTransferSwitchFailsClosed(t *testing.T) {
	d := newTestDevices(t)
	if err := d.setFileTransferOn(t.Context(), "box", true); err != nil {
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

func TestFileTransferSettingsAreRelayed(t *testing.T) {
	for _, want := range []string{"GET /api/v1/machines/{mid}/file-transfer/settings", "PUT /api/v1/machines/{mid}/file-transfer/settings"} {
		if !slices.Contains(devicewire.Routes, want) {
			t.Errorf("%s is not relayed by the hub", want)
		}
	}
}
