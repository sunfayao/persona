package hub

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
)

// Device is a browser or app that keeps notification settings on the hub.
type Device = db.Device

// Pusher decides what is notified, to whom and how loudly: each device's settings, quiet hours, the low-distraction
// grading and the held-push list, and the browsers' Web Push. The alerts themselves go over the clients' WebSockets
// (notifyws.go) and Web Push.
type Pusher struct {
	store   Store
	quiesce *writeQuiescer
	logf    func(string, ...any)
	now     func() time.Time
	// desktopOnline: a desktop app is connected (Hub.desktopOnline), for "Phone Muted".
	desktopOnline func() bool
	// userLoc is the user's time zone (Hub.userLocation): quiet hours without a zone of their own read in it (quiethours.go).
	userLoc func() *time.Location
	// desktopOpen: the desktop app with this device id is connected (Hub.desktopOpen).
	desktopOpen func(device string) bool
	// web pushes to the browsers that turned Web Push on (webpush.go, #7459).
	web *webPusher

	mu      sync.Mutex
	devices map[string]Device
	saveMu  sync.Mutex // one device row write at a time, always the latest state (setPrefsBy, forget)
	// storeTimeout bounds each device row write, which never runs under mu; tests shorten it.
	storeTimeout time.Duration
	// prefsEdit holds one change of a device's settings from its read to its write (editPrefs); taken before saveMu
	// and mu, never by a push.
	prefsEdit sync.Mutex

	// gradingMu guards the low-distraction mode state (push_grading.go): the cached switch, the per-key push times for the
	// 12-hour rule, the held-push list and the delivered-push counts.
	gradingMu         sync.Mutex
	gradingCached     *bool
	gradingCachedAt   time.Time
	gradingRefreshing bool
	gradingVersion    uint64 // bumped by setPushGrading, so a refresh that read before it does not undo it
	gradingLastAlert  map[string]time.Time
	intercepts        []heldPushRecord
	sent              []pushSent
	heldDirty         bool // the held-push list changed since the last save
	// sentPending are the delivered pushes not yet appended to the store's record (pushSentStore); sentPrunedAt is
	// when its last sweep ran.
	sentPending  []pushSent
	sentPrunedAt time.Time
	// The hub.push_sent setting a build from before the table reads (mirrorSent): sentMirrorable once this hub read
	// both at start, sentMirrorDue while it holds pushes the setting lacks, sentMirroredAt its last write.
	sentMirrorable bool
	sentMirrorDue  bool
	sentMirroredAt time.Time
	sentMirrorMu   sync.Mutex
	gradingWriting bool // the one saver goroutine is running
}

// deviceStoreTimeout: how long a device row write may take before it gives up (the pool has no statement_timeout).
const deviceStoreTimeout = 5 * time.Second

func newPusher(store Store, logf func(string, ...any)) *Pusher {
	p := &Pusher{store: store, logf: logf, devices: map[string]Device{}, now: time.Now, storeTimeout: deviceStoreTimeout,
		// Web Push in memory here; the hub gives it the store when it starts (hub.go): a test's store may have no settings.
		web: newWebPusher(nil, logf)}
	if store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), deviceStoreTimeout)
		if list, err := store.Devices(ctx); err == nil {
			for _, d := range list {
				p.devices[d.DeviceID] = d
			}
		} else {
			logf("load devices: %v", err)
		}
		cancel()
	}
	p.loadPushGradingState() // the grading switch and held pushes survive restarts (hub_settings)
	return p
}

// signedOut forgets the devices a revoked credential wrote settings for (Settings › Devices): they get no more
// notifications, and pairing again starts from the defaults.
func (p *Pusher) signedOut(credential string) {
	if p == nil || credential == "" {
		return
	}
	p.mu.Lock()
	var gone []Device
	for _, d := range p.devices {
		if d.Credential == credential {
			gone = append(gone, d)
		}
	}
	p.mu.Unlock()
	for _, d := range gone {
		p.forget(d)
		p.logf("device %s signed out: no more notifications", d.Name)
	}
	p.web.signedOut(credential)
}

// webPusher is p's Web Push (nil for a nil Pusher).
func (p *Pusher) webPusher() *webPusher {
	if p == nil {
		return nil
	}
	return p.web
}

// forget drops a device (signedOut).
func (p *Pusher) forget(d Device) {
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	p.mu.Lock()
	delete(p.devices, d.DeviceID)
	p.mu.Unlock()
	p.deleteRow(d)
}

// deleteRow deletes a forgotten device's row, bounded by storeTimeout. A delete that fails is logged: the row stays,
// so the device is loaded again on the next start until it is forgotten again.
func (p *Pusher) deleteRow(d Device) {
	if p.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.storeTimeout)
	defer cancel()
	if err := p.store.DeleteDevice(ctx, d.DeviceID); err != nil {
		p.logf("forget device %s: %v", d.Name, err)
	}
}

// prefs is a device's notification settings (defaults when it never set any).
func (p *Pusher) prefs(id string) NotifyPrefs {
	p.mu.Lock()
	defer p.mu.Unlock()
	return devicePrefs(p.devices[id].Prefs)
}

// setPrefsBy stores a device's notification settings (also for a device that has not registered a token yet),
// written with a device token (`credential`): a device that has none recorded takes it, so revoking the token
// forgets a device only its prefs made (a browser's Settings › Notifications, which never registers a push token).
// The row is written under saveMu, not mu: a slow database holds this one save (up to storeTimeout), not every
// other device's settings and every push. The settings take effect once the row is written.
func (p *Pusher) setPrefsBy(id, credential string, prefs NotifyPrefs) error {
	raw, _ := json.Marshal(prefs)
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	if p.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), p.storeTimeout)
		err := p.store.SetDevicePrefs(ctx, id, credential, raw)
		cancel()
		if err != nil {
			return err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.devices[id]
	if !ok {
		d = Device{DeviceID: id}
	}
	if d.Credential == "" {
		d.Credential = credential
	}
	d.Prefs = raw
	p.devices[id] = d
	return nil
}

// editPrefs changes a device's settings with edit and stores them as setPrefsBy does, holding prefsEdit from the read
// to the write: two partial changes at once (a client sends one switch per PUT) both take effect, the later one is not
// written over the settings as they were before the earlier was stored. The result is the settings as stored,
// normalized as devicePrefs reads them. An error from edit stores nothing.
func (p *Pusher) editPrefs(id, credential string, edit func(*NotifyPrefs) error) (NotifyPrefs, error) {
	p.prefsEdit.Lock()
	defer p.prefsEdit.Unlock()
	after := p.prefs(id)
	if err := edit(&after); err != nil {
		return after, err
	}
	raw, _ := json.Marshal(after)
	after = devicePrefs(raw) // normalizes an unknown "done"
	return after, p.setPrefsBy(id, credential, after)
}

// mailTargets are the languages some device wants its mail translated into (Settings › Translation on, mail in scope).
func (p *Pusher) mailTargets() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, d := range p.devices {
		t := devicePrefs(d.Prefs).Translate
		if t.Enabled && t.Mail && assistantwire.TranslateLanguages[t.Target] != "" && !seen[t.Target] {
			seen[t.Target] = true
			out = append(out, t.Target)
		}
	}
	sort.Strings(out)
	return out
}
