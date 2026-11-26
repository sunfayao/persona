package assistant

import (
	"context"

	"github.com/nwverse/persona/server/internal/assistant/agents"
)

// machinePresets are the Agent presets machine (an id) can start in host ("": its own launcher list): the profile in
// use's tiers whose launcher it has (pickerTiers), each marked unavailable while its launcher's latest check says it
// does not work. Agents read them through dispatch_read and a2a_sessions before starting one.
func (s *Service) machinePresets(ctx context.Context, machine, host string) ([]workerPreset, error) {
	launchers, err := s.pickerLaunchersOn(ctx, machine, host)
	if err != nil {
		return nil, err
	}
	_, tiers := s.pickerTiers(ctx, machine, launchers)
	out := make([]workerPreset, 0, len(tiers))
	for _, t := range tiers {
		_, unavailable := s.launcherUnusable(ctx, machine, t.Launcher)
		out = append(out, workerPreset{
			Name: t.Name, Launcher: t.Launcher, Model: t.Model, Effort: t.Effort, Speed: t.Speed,
			Description: t.Description, Unavailable: unavailable,
		})
	}
	return out, nil
}

// workerPreset is one preset offered to an agent.
type workerPreset = agents.Preset
