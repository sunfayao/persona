package extmgr

import (
	"slices"

	"github.com/sunfayao/persona/server/internal/proto"
)

// MachineControl is what the assistant can do on one Mac through the built-in plugins cua and ego, for its
// instructions and machines_list (assistant/machine_controls.go). The switches are the plugins' settings, which hold
// only Macs and stay put while a Mac is away; the rest is what the Mac's node said when last asked, empty until then.
type MachineControl struct {
	// ComputerUse is the driver cua is set to use (cua: Cua Driver, lcu, or auto: LCU where it is set up, else Cua
	// Driver); "" while computer use is off for the Mac.
	ComputerUse string
	// Driver is what a computer_* call drives there now (callBackend): auto until a status of the Mac says lcu.
	Driver string
	// Browser: ego is on for it (browser_look, browser_run).
	Browser bool
	// Reported: its node said what it has of the plugin's live screen since the manager started; ScreenSharing:
	// macOS's Screen Sharing answered then, so its live screen opens (web lib/computer.ts showsScreen).
	Reported, ScreenSharing bool
	// ScreenPermission is Persona Screen's Screen Recording (granted | missing | unknown), "" without the helper;
	// ScreenAsked: macOS was asked for this helper version (proto.ScreenVideo).
	ScreenPermission string
	ScreenAsked      bool
}

// MachineControls are the Macs cua or ego is on for, by machine id; nil while neither is on for any.
func (h *Manager) MachineControls() map[string]MachineControl {
	cua, ego := h.cua(), h.machineExtension(egoExtensionID)
	if len(cua.Machines) == 0 && len(ego.Machines) == 0 {
		return nil
	}
	out := map[string]MachineControl{}
	for _, m := range h.machinesNow() {
		id := m.cfg.ID
		var mc MachineControl
		if slices.Contains(cua.Machines, id) {
			mc.ComputerUse, mc.Driver = cua.backend(), h.callBackend(m)
		}
		mc.Browser = slices.Contains(ego.Machines, id)
		if mc.ComputerUse == "" && !mc.Browser {
			continue
		}
		m.mu.Lock()
		computer, browser := m.computer, m.browser
		m.mu.Unlock()
		// The live screen is the one of the plugin that is on for the Mac, cua's first (showsScreen): both read the
		// same Screen Sharing and the same helper.
		var video *proto.ScreenVideo
		switch {
		case mc.ComputerUse != "" && computer != nil:
			mc.Reported, mc.ScreenSharing, video = true, computer.ScreenSharing, computer.ScreenVideo
		case mc.ComputerUse == "" && browser != nil:
			mc.Reported, mc.ScreenSharing, video = true, browser.ScreenSharing, browser.ScreenVideo
		}
		if video != nil {
			mc.ScreenPermission, mc.ScreenAsked = video.Permission, video.Asked
		}
		out[id] = mc
	}
	return out
}
