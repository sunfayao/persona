package assistant

import (
	"context"
	"net/url"

	"github.com/sunfayao/persona/server/internal/extmgr"
	"github.com/sunfayao/persona/server/internal/prompts"
)

// machineControlSource is the part of the extensions that says what can be done on each Mac besides handing work to
// its Agents: computer use (cua), its browser (ego) and its live screen. Optional: extensions without it (older ones,
// tests) leave the machine lines and machines_list as they were.
type machineControlSource interface {
	MachineControls() map[string]extmgr.MachineControl
}

// The extensions in this process say it.
var _ machineControlSource = (*extmgr.Manager)(nil)

// machineControls are the Macs computer use or the browser is on for, by machine id; nil when none is, or the
// extensions do not say.
func (s *Service) machineControls() map[string]extmgr.MachineControl {
	if c, ok := s.ext.(machineControlSource); ok {
		return c.MachineControls()
	}
	return nil
}

// withControls adds to the instructions' machine lines what can be done on each Mac. Only what the user switches is
// shown, never what a Mac said last (online, Screen Sharing): a changed line rewrites the instructions, and a Claude
// conversation then resumes in a fresh process. The one exception, a Persona Screen macOS keeps refusing, changes only
// when the user acts on it.
func (s *Service) withControls(ctx context.Context, machines []prompts.AssistantMachine) {
	controls := s.machineControls()
	if len(controls) == 0 {
		return
	}
	web := s.webBase(ctx)
	for i := range machines {
		c, ok := controls[machines[i].ID]
		if !ok {
			continue
		}
		m := &machines[i]
		m.ComputerUse, m.Browser = c.ComputerUse, c.Browser
		if web != "" {
			m.LiveScreen = web + "/screen/" + url.PathEscape(m.ID)
		}
		m.ScreenNotAllowed = screenNotAllowed(c)
	}
}

// screenNotAllowed: macOS was asked whether Persona Screen may record the Mac's screen, and it still may not.
func screenNotAllowed(c extmgr.MachineControl) bool {
	return c.ScreenPermission == "missing" && c.ScreenAsked
}
