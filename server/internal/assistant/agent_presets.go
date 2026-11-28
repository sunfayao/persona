package assistant

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/agents"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Agent presets for Agents (#14672): an Agent that starts
// another one (a2a_send's new Agent, dispatch_start) names a tier of the profile in use as the assistant does with
// agent_start, and reads beforehand which presets each computer can start (a2a_sessions with presets,
// dispatch_read's overview).

// runs is r in a line, as a tier's is said (ModelTier.runs).
func agentRunText(r agents.Run) string {
	return ModelTier{Launcher: r.Launcher, Model: r.Model, Effort: r.Effort, Speed: r.Speed}.runs()
}

// agentStartTier resolves the tier an Agent names when it starts another (a2a_send's new Agent, dispatch_start) as
// agent_start resolves the assistant's, before anything is recorded or started: the tier of the profile in use first
// (an unknown one is refused, tierModel); a launcher the caller names that is not the tier's takes nothing from it
// (layTier); then the start is held to what computer m runs, with Model configuration › Agents laid over it as the
// start lays it (applyModel): a tier's launcher m lacks is refused (launcher_not_on_machine, fitLauncher), as is a
// faster speed its agent cannot start with (checkStartSpeed), and an agent that takes no effort drops the tier's. The
// tier's permission mode is left out on purpose: an Agent another Agent starts keeps the mode its start path gives
// it (a2a_send's plan, dispatch_start's own rule). Zero without a tier, or for one that gave nothing.
func (s *Service) agentStartTier(ctx context.Context, m Device, tier, launcher string) (agents.Run, error) {
	tier, launcher = strings.TrimSpace(tier), strings.TrimSpace(launcher)
	if tier == "" {
		return agents.Run{}, nil
	}
	t, err := s.tierModel(ctx, tier)
	if err != nil {
		return agents.Run{}, err
	}
	run := agents.Run{Launcher: launcher}
	if !layTier(t, &run.Launcher, &run.Model, &run.Effort) {
		return agents.Run{}, nil // a launcher of the caller's own: the tier gives it nothing
	}
	if proto.FasterSpeed(t.Speed) {
		run.Speed = t.Speed // these starts name no model of their own, so the tier's speed goes with the tier's model
	}
	probe := map[string]any{}
	run.Apply(probe)
	if err := s.applyModel(ctx, "agent.new", probe); err != nil {
		return agents.Run{}, err
	}
	_, fit, err := s.fitLauncher(ctx, m, startPick{launcher: launcher, tier: tier}, probe)
	if err != nil {
		return agents.Run{}, err
	}
	if err := checkStartSpeed(probe, fit, ""); err != nil {
		return agents.Run{}, err
	}
	if !startTakesModes(fit.agent) {
		run.Effort = ""
	}
	return run, nil
}

// presetsWait bounds how long a list of several computers' presets waits for their launcher lists.
const presetsWait = 8 * time.Second

// computerPresets is one computer's presets as presetsOn read them, or why they could not be read.
type computerPresets = agents.PresetList

// presetsOn reads at once, by device id, the Agent presets each of machines can start where its new Agents start
// (machinePresets in its default host, as New Agent lists them), within presetsWait. No computer is asked when the
// profile in use has no tiers.
func (s *Service) presetsOn(ctx context.Context, machines []Device) map[string]computerPresets {
	out := map[string]computerPresets{}
	if p, _, err := s.activeModelProfile(ctx); err != nil || p == nil || len(p.Tiers) == 0 || len(machines) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, presetsWait)
	defer cancel()
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, m := range machines {
		wg.Go(func() {
			presets, err := s.machinePresets(ctx, m.ID)
			mu.Lock()
			out[m.ID] = computerPresets{Presets: presets, Err: err}
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}

// runs is what preset p runs on, in a line (ModelTier.runs without a permission mode, which Agent-started Agents do
// not take from a tier).
func presetRunText(p workerPreset) string {
	return ModelTier{Launcher: p.Launcher, Model: p.Model, Effort: p.Effort, Speed: p.Speed}.runs()
}

// writePresets puts the presets read for machine under its line in dispatch_read's overview: one line each, with
// what it runs and when to use it.
func writePresets(b *strings.Builder, read map[string]computerPresets, machine string) {
	r, ok := read[machine]
	switch {
	case !ok:
		return
	case r.Err != nil:
		b.WriteString("  - its Agent presets could not be read this time\n")
		return
	}
	for _, p := range r.Presets {
		fmt.Fprintf(b, "  - tier %s: %s", p.Name, presetRunText(p))
		if p.Description != "" {
			fmt.Fprintf(b, " (%s)", p.Description)
		}
		if p.Unavailable {
			b.WriteString("; unavailable now: its launcher's latest check failed")
		}
		b.WriteString("\n")
	}
}

// a2aLaunchWhere is where the phone's card for a new a2a_send launch from an assistant conversation says it goes: the
// folder, and the tier with what it runs on, as agent_start's card says it.
func (s *Service) a2aLaunchWhere(ctx context.Context, launch *agents.Launch) string {
	if launch.Run.Launcher == "" {
		return launch.CWD
	}
	return launch.CWD + i18n.T(s.lang(ctx), "assistant-tools.with_tier", i18n.Args{"tier": launch.Tier, "runs": agentRunText(launch.Run)})
}
