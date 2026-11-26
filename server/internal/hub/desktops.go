package hub

import (
	"cmp"
	"encoding/json"
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/devicewire"
)

// featureDesktopUnregister says this hub withdraws a connection's desktop presence without closing its app socket.
const featureDesktopUnregister = "desktop_unregister"

// linkDesktop is a computer whose Persona Link says the user is at it until until, as its manager (owner) last
// reported.
type linkDesktop struct {
	owner, machine, name string
	since, until         time.Time
}

// linkDesktops are the Persona Link desktops last reported by their managers (h.mu).
type linkDesktops struct {
	list []linkDesktop
}

func init() {
	handleBusEvent(devicewire.EventDesktops, func(h *Hub, c *busConn, params json.RawMessage) {
		var d devicewire.Desktops
		if err := json.Unmarshal(params, &d); err != nil {
			h.logf("bus: [%s] %s: %v", c.ident.ID, devicewire.EventDesktops, err)
			return
		}
		h.takeLinkDesktops(c.managerID(), d.Desktops)
	})
}

// takeLinkDesktops is manager owner's devices.desktops: its computers whose Persona Link says the user is at them. Only
// its own computers count, as the hub has them (named as the hub calls them), with notifications showing; a mesh
// member's computer does not count yet. Each counts for the TTL it says from now, on the hub's own clock and at most
// DesktopTTLMax: a manager's clock, ahead or behind, never stretches it.
func (h *Hub) takeLinkDesktops(owner string, ds []devicewire.Desktop) {
	now := time.Now()
	var mine []linkDesktop
	for _, d := range ds {
		m := h.machineByID(d.MachineID) // a hub.json entry another one absorbed names that one's computer
		if m == nil || !d.Notifies || d.TTLMS <= 0 {
			continue
		}
		m.mu.Lock()
		ours := m.managed && m.owner == owner && !m.derived
		m.mu.Unlock()
		if ours {
			ttl := time.Duration(min(d.TTLMS, devicewire.DesktopTTLMax.Milliseconds())) * time.Millisecond
			mine = append(mine, linkDesktop{owner: owner, machine: m.cfg.ID, name: m.name(), since: d.Since, until: now.Add(ttl)})
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	// The other managers' as they said them, but those that lapsed: those the timer has not taken out yet are no change.
	all := slices.DeleteFunc(slices.Clone(h.linkDesktops.list), func(d linkDesktop) bool {
		return d.owner == owner || !d.until.After(now)
	})
	all = append(all, mine...)
	slices.SortStableFunc(all, func(a, b linkDesktop) int { return cmp.Compare(a.owner, b.owner) })
	h.linkDesktops.list = all
}

// linkDesktopOnline: some computer of manager owner's has its Persona Link say the user is at it ("Phone Muted" holds
// back that manager's "Needs you" cards, the ones that Persona Link shows). Read when asked, so one whose manager went
// quiet stops counting when it lapses.
func (h *Hub) linkDesktopOnline(owner string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	return slices.ContainsFunc(h.linkDesktops.list, func(d linkDesktop) bool { return d.owner == owner && d.until.After(now) })
}

// desktopOnline: some desktop app is connected ("Phone Muted" holds the phone's pushes back).
func (h *Hub) desktopOnline() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.desktop {
			return true
		}
	}
	return false
}

// desktopOpen: the desktop app with this push id is connected. It posts its own notifications for agents that turn
// needs-you or finish (the Mac's MacAttentionWatcher, on the same Settings › Notifications), so those pushes would show twice.
func (h *Hub) desktopOpen(device string) bool {
	if device == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if c.desktop && c.pushDevice == device {
			return true
		}
	}
	return false
}

// registerDesktop marks c as a desktop app for notification routing. Not for an answer-only device (a watch): it would
// mute the phone. pushDevice is its id for pushes (PushRegistrar.deviceID; empty from an older app): while it is
// connected the hub leaves the agents' alerts to it (desktopOpen).
func (h *Hub) registerDesktop(c *client, name, pushDevice string) {
	if c.role != roleFull {
		return
	}
	h.mu.Lock()
	c.desktop, c.name, c.pushDevice = true, name, pushDevice
	h.mu.Unlock()
	go h.fleetClientsChanged() // Phone Muted holds back a phone's MCP form pushes now (fleet_link.go)
	h.logf("desktop %q connected (device %s)", name, c.device)
}

// unregisterDesktop withdraws only this connection's desktop presence. The web app keeps its socket and other work.
// Older hubs ignore the unknown message; the existing socket-close path remains the compatibility fallback.
func (h *Hub) unregisterDesktop(c *client) {
	if c.role != roleFull {
		return
	}
	h.mu.Lock()
	if !c.desktop && c.pushDevice == "" {
		h.mu.Unlock()
		return
	}
	name := c.name
	c.desktop, c.pushDevice = false, ""
	h.mu.Unlock()
	go h.fleetClientsChanged()
	h.logf("desktop %q unregistered (device %s)", name, c.device)
}

// dropClient forgets a client whose connection ended (also as one the shutdown has to close).
func (h *Hub) dropClient(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	delete(h.wsConnections, c)
	desktop := c.desktop
	h.mu.Unlock()
	h.fsWatchRelease(c) // the folders its panels had open are no longer watched (fswatch.go)
	go h.fleetClientsChanged()
	if desktop {
		h.logf("desktop %q disconnected (device %s)", c.name, c.device)
	}
}

// alertWanted is which devices an alert push goes to: what their settings pick (wants; nil: every device, for what
// was asked for explicitly), less those muted while a desktop is online.
func alertWanted(wants func(NotifyPrefs) bool, desktopOnline bool) func(NotifyPrefs) bool {
	return func(p NotifyPrefs) bool {
		if wants == nil {
			return true
		}
		return wants(p) && !(desktopOnline && p.MuteWhileDesktop)
	}
}
