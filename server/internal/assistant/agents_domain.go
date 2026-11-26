package assistant

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/agents"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/identity"
	"github.com/sunfayao/persona/server/internal/proto"
)

func (s *Service) newAgentsDomain() *agents.Domain {
	return agents.New(agents.Dependencies{
		Store: func() agents.Store {
			st, _ := s.store().(agents.Store)
			return st
		},
		Nodes: agentNodes{s}, Machines: agentMachines{s}, Visibility: agentVisibility{s},
		Outbound: agentOutbound{s}, Launches: agentLaunches{s}, Admission: agentAdmission{s},
		RunContext: s.runCtx, NewSessionID: newStartSessionID, NewID: func() string { return randomHex(16) },
		StartTimeout: func() time.Duration { return agentStartTimeout }, Language: s.lang, Logf: s.logf,
	})
}

type agentNodes struct{ s *Service }

func (a agentNodes) Call(ctx context.Context, machine, method string, params, result any) error {
	return a.s.call(ctx, machine, method, params, result)
}
func (a agentNodes) Session(ctx context.Context, machine, session string) (proto.Session, bool, error) {
	return a.s.sessionNow(ctx, machine, session)
}
func (a agentNodes) Sessions(ctx context.Context, machine string) []proto.Session {
	list, _ := a.s.sessions(ctx, machine)
	return list
}
func (a agentNodes) Listed(ref agents.Ref) (proto.Session, bool) {
	return a.s.nodeSession(ref.Machine, ref.Session)
}
func (a agentNodes) ExecutionMachine(ctx context.Context, ref agents.Ref) string {
	return a.s.logicalExecutionMachine(ctx, ref.Machine, ref.Session)
}
func (a agentNodes) Executable(ctx context.Context, machine string) string {
	return a.s.nodeExecutable(ctx, machine)
}
func (agentNodes) Delivered(err error) string { return nodeDelivered(err) }

type agentMachines struct{ s *Service }

func (a agentMachines) List(ctx context.Context) []Device { return a.s.devices(ctx) }
func (a agentMachines) Resolve(ctx context.Context, id string) (Device, bool) {
	return a.s.machineByName(ctx, id)
}
func (a agentMachines) Lookup(ctx context.Context, id string) (Device, bool) {
	return a.s.device(ctx, id)
}
func (a agentMachines) Trusted(ctx context.Context, machine string) bool {
	if a.s.isWorkMachine(machine) || a.s.machinePending(ctx, machine) {
		return false
	}
	if a.s.devs != nil {
		return a.s.devs.Has(machine)
	}
	dev, ok := a.s.machineByName(ctx, machine)
	return ok && dev.ID == machine
}
func (a agentMachines) Offline(machine string) bool {
	return a.s.computerState(machine).state == standingComputerOffline
}

type agentVisibility struct{ s *Service }

func (a agentVisibility) Config(ctx context.Context) assistantwire.A2AConfig {
	cfg := a.s.hello(ctx)
	if cfg.Assistant.Standby {
		return assistantwire.A2AConfig{Machines: []string{}}
	}
	return cfg.Agents.A2A
}
func (a agentVisibility) Available(ctx context.Context, ref agents.Ref) bool {
	return a.s.conversationAvailable(ctx, ref.Machine, ref.Session) == nil
}
func (a agentVisibility) Private(ctx context.Context, ref agents.Ref) bool {
	if a.Archived(ctx, ref) {
		return true
	}
	_, guest, err := a.s.guestOf(ctx, ref.Machine, ref.Session)
	return err != nil || guest || a.s.selfPolicy(ctx, ref.Session).HideFromExtensions
}
func (a agentVisibility) PrivateLaunchPath(ctx context.Context, cwd string) bool {
	return a.s.trackPolicy(ctx, identity.SpaceAt(cwd)).HideFromExtensions
}
func (a agentVisibility) Deleted(ctx context.Context, ref agents.Ref) bool {
	_, deleted, _ := a.s.deletedConversation(ctx, ref.Machine, ref.Session)
	return deleted
}
func (a agentVisibility) Archived(ctx context.Context, ref agents.Ref) bool {
	row, ok, _ := a.s.archivedRow(ctx, ref.Machine, ref.Session)
	return ok && row.Archived
}
func (agentVisibility) InTrack(session proto.Session) bool { return runSpace(session) != "" }
func (a agentVisibility) HiddenByDirectory(machine string, row proto.Session) bool {
	return a.s.hiddenByDirectory(machine, row)
}

type agentOutbound struct{ s *Service }

func (a agentOutbound) AllowLaunch(ctx context.Context, from agents.Ref) error {
	if err := a.s.guardRefusal(ctx, from.Machine, from.Session, "agent_start"); err != nil {
		return err
	}
	if err := a.s.dispatchToolGuard(ctx, from.Machine, from.Session, "agent_start"); err != nil {
		return err
	}
	return a.s.workerRefusal(ctx, from.Machine, from.Session)
}
func (a agentOutbound) AllowSend(ctx context.Context, from, to agents.Ref, text string) error {
	if err := a.s.guardRefusal(ctx, from.Machine, from.Session, "conversation_send"); err != nil {
		return err
	}
	return a.s.dispatchSendGuard(withToolCaller(ctx, from.Machine, from.Session), from.Session, to.Machine, to.Session, text)
}
func (a agentOutbound) NeedsConfirmation(ctx context.Context, ref agents.Ref) bool {
	return a.s.isAssistantConversation(ctx, ref.Machine, ref.Session)
}
func (a agentOutbound) Confirm(ctx context.Context, ref agents.Ref, text, body string, args json.RawMessage) (bool, error) {
	return a.s.confirmOutbound(ctx, ref.Machine, ref.Session, "a2a_send", text, body, args)
}
func (a agentOutbound) CredentialRevoked(ctx context.Context, device string) (bool, error) {
	return a.s.hub.CredentialRevoked(ctx, device)
}

type agentLaunches struct{ s *Service }

func (a agentLaunches) ResolveTier(ctx context.Context, m Device, tier, launcher string) (agents.Run, error) {
	return a.s.agentStartTier(ctx, m, tier, launcher)
}
func (a agentLaunches) DescribeTier(ctx context.Context, launch *agents.Launch) string {
	return a.s.a2aLaunchWhere(ctx, launch)
}
func (a agentLaunches) Presets(ctx context.Context, machines []Device) map[string]agents.PresetList {
	return a.s.presetsOn(ctx, machines)
}
func (a agentLaunches) ApplyModel(ctx context.Context, request map[string]any) error {
	return a.s.applyModel(ctx, "agent.new", request)
}
func (a agentLaunches) SelectHost(ctx context.Context, m Device, request map[string]any) (bool, error) {
	return a.s.agentStartedHost(ctx, m, request)
}
func (a agentLaunches) UsesClaude(ctx context.Context, launcher string) bool {
	if assistantKind(launcher) != "" {
		return launcher == "claude"
	}
	m, managed := a.s.managedNamed(ctx, launcher)
	return managed && m.AgentType() == "claude"
}
func (agentLaunches) CompanionAway(err error, chosen bool) bool { return companionAway(err, chosen) }
func (agentLaunches) StartUnknown(err error) bool               { return startResultUnknown(err) || sendUnknown(err) }

type agentAdmission struct{ s *Service }

func (a agentAdmission) Enabled(ctx context.Context) bool { return a.s.hello(ctx).Agents.Dispatch.On() }
func (a agentAdmission) Lock() func() {
	a.s.dispatch.admit.Lock()
	return a.s.dispatch.admit.Unlock
}
func (a agentAdmission) CheckCapacity(ctx context.Context, from agents.Ref, now time.Time) error {
	return a.s.checkDispatchCapacity(ctx, from, now)
}
