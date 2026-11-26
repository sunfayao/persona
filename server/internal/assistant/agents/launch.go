package agents

import (
	"context"
	"fmt"

	"github.com/sunfayao/persona/server/internal/assistantwire"
)

// Run is what an Agent another Agent starts on a tier runs on: the tier's launcher, model, effort and faster speed,
// resolved when the start was accepted (agentStartTier); zero without a tier. It is kept on the stored launch beside
// the tier the caller named (a2aLaunch), so a start that goes out later runs what was checked, and a retry compares
// the caller's tier, not this.
type Run struct {
	Launcher string `json:"launcher"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
	Speed    string `json:"speed,omitempty"`
}

// Apply puts r on a session.start request in place of the caller's launcher; Model configuration › Agents then fills
// what r leaves out (applyModel), as for agent_start. Nothing without a tier.
func (r Run) Apply(params map[string]any) {
	if r.Launcher == "" {
		return
	}
	params["launcher"] = r.Launcher
	for k, v := range map[string]string{"model": r.Model, "effort": r.Effort, "speed": r.Speed} {
		if v != "" {
			params[k] = v
		}
	}
}

// a2aComputer is one computer a2a_sessions lists with presets: where a2a_send can start an Agent, and the Agent
// presets it can start there.
type a2aComputer struct {
	Machine string   `json:"machine_id"`
	Name    string   `json:"name,omitempty"`
	Online  bool     `json:"online"`
	Presets []Preset `json:"presets,omitempty"`
	// Unread: the computer is online but its launcher list could not be read this time.
	Unread bool `json:"presets_unread,omitempty"`
}

// a2aComputers are the computers an a2a_send launch can go to (machine: only that one), the caller's own included,
// each with its presets; none while a launch is not allowed (a2aLaunchOn).
func (s *Domain) a2aComputers(ctx context.Context, cfg assistantwire.A2AConfig, machine string) []a2aComputer {
	out := []a2aComputer{}
	if !s.a2aLaunchOn(ctx, cfg) {
		return out
	}
	var listed, online []assistantwire.Device
	for _, d := range s.machines.List(ctx) {
		if machine != "" && d.ID != machine || !s.a2aMachineAllowed(ctx, cfg, d.ID) {
			continue
		}
		listed = append(listed, d)
		if d.Online || d.Reachable {
			online = append(online, d)
		}
	}
	read := s.launches.Presets(ctx, online)
	for _, d := range listed {
		c := a2aComputer{Machine: d.ID, Name: d.Name, Online: d.Online || d.Reachable}
		if r, ok := read[d.ID]; ok {
			c.Presets, c.Unread = r.Presets, r.Err != nil
		}
		out = append(out, c)
	}
	return out
}

// a2aLaunchTier resolves the tier a new a2a_send launch names on its target computer (agentStartTier) before anything
// is recorded, and keeps what it runs on with the launch. A computer a2aStart would refuse is not asked, so a refusal
// never names its launchers. A retry by message_id is not resolved again: a2aStart matches it on the tier.
func (s *Domain) a2aLaunchTier(ctx context.Context, cfg assistantwire.A2AConfig, machine string, launch *Launch) error {
	if launch.Tier == "" {
		return nil
	}
	dev, ok := s.machines.Resolve(ctx, machine)
	if !ok || dev.ID != machine || !s.a2aMachineAllowed(ctx, cfg, machine) || !s.a2aLaunchOn(ctx, cfg) {
		return nil // a2aStart refuses it
	}
	run, err := s.launches.ResolveTier(ctx, dev, launch.Tier, launch.Launcher)
	if err != nil {
		return fmt.Errorf("%w; nothing was started", err)
	}
	launch.Run = run
	return nil
}

// a2aLaunchOn: a2aStart lets an a2a_send launch through, with an A2A scope named or Agent dispatch on.
func (s *Domain) a2aLaunchOn(ctx context.Context, cfg assistantwire.A2AConfig) bool {
	return cfg.Machines != nil || s.admission.Enabled(ctx)
}

type Preset struct {
	Name        string `json:"name"`
	Launcher    string `json:"launcher"`
	Model       string `json:"model,omitempty"`
	Effort      string `json:"effort,omitempty"`
	Speed       string `json:"speed,omitempty"`
	Description string `json:"description,omitempty"`
	// Unavailable: the launcher's latest check says it does not work right now (for example a usage limit with a
	// reset still ahead); absent check results leave it false, so an unchecked preset is offered.
	Unavailable bool `json:"unavailable,omitempty"`
}

// PresetList is a computer's available launch presets or its listing error.
type PresetList struct {
	Presets []Preset
	Err     error
}
