package agents

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Nodes is the manager's view of running Agent sessions and their RPCs.
type Nodes interface {
	Call(context.Context, string, string, any, any) error
	Session(context.Context, string, string) (proto.Session, bool, error)
	Sessions(context.Context, string) []proto.Session
	Listed(Ref) (proto.Session, bool)
	ExecutionMachine(context.Context, Ref) string
	Executable(context.Context, string) string
	Delivered(error) string
}

// Machines resolves computer addresses and reports trust and connectivity.
type Machines interface {
	List(context.Context) []assistantwire.Device
	Resolve(context.Context, string) (assistantwire.Device, bool)
	Lookup(context.Context, string) (assistantwire.Device, bool)
	Trusted(context.Context, string) bool
	Offline(string) bool
}

// Visibility supplies the manager's conversation and directory policy.
type Visibility interface {
	Config(context.Context) assistantwire.A2AConfig
	Available(context.Context, Ref) bool
	Private(context.Context, Ref) bool
	PrivateLaunchPath(context.Context, string) bool
	Deleted(context.Context, Ref) bool
	Archived(context.Context, Ref) bool
	InTrack(proto.Session) bool
	HiddenByDirectory(string, proto.Session) bool
}

// Outbound supplies the manager's policy and confirmation for hand-offs.
type Outbound interface {
	AllowLaunch(context.Context, Ref) error
	AllowSend(context.Context, Ref, Ref, string) error
	NeedsConfirmation(context.Context, Ref) bool
	Confirm(context.Context, Ref, string, string, json.RawMessage) (bool, error)
	CredentialRevoked(context.Context, string) (bool, error)
}

// Launches resolves and validates launchers through the manager's model settings.
type Launches interface {
	ResolveTier(context.Context, assistantwire.Device, string, string) (Run, error)
	DescribeTier(context.Context, *Launch) string
	Presets(context.Context, []assistantwire.Device) map[string]PresetList
	ApplyModel(context.Context, map[string]any) error
	SelectHost(context.Context, assistantwire.Device, map[string]any) (bool, error)
	UsesClaude(context.Context, string) bool
	CompanionAway(error, bool) bool
	StartUnknown(error) bool
}

// Admission shares the launch limit and lock with dispatch_start.
type Admission interface {
	Enabled(context.Context) bool
	Lock() (unlock func())
	CheckCapacity(context.Context, Ref, time.Time) error
}

// Clock supplies both timestamps and the waits driven by them.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
	Ticker(time.Duration) (<-chan time.Time, func())
	WithTimeout(context.Context, time.Duration) (context.Context, context.CancelFunc)
}

type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (wallClock) Ticker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}
func (wallClock) WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

// Dependencies are supplied by the assistant's composition root.
type Dependencies struct {
	Store        func() Store
	Nodes        Nodes
	Machines     Machines
	Visibility   Visibility
	Outbound     Outbound
	Launches     Launches
	Admission    Admission
	Clock        Clock
	RunContext   func() context.Context
	NewSessionID func() string
	NewID        func() string
	StartTimeout func() time.Duration
	Language     func(context.Context) i18n.Lang
	Logf         func(string, ...any)
}

// Domain owns A2A task state and the delivery worker's wake signal.
type Domain struct {
	getStore     func() Store
	nodes        Nodes
	machines     Machines
	visibility   Visibility
	outbound     Outbound
	launches     Launches
	admission    Admission
	clock        Clock
	runContext   func() context.Context
	newSessionID func() string
	newID        func() string
	startTimeout func() time.Duration
	language     func(context.Context) i18n.Lang
	logf         func(string, ...any)
	wake         chan struct{}
}

func New(deps Dependencies) *Domain {
	if deps.Clock == nil {
		deps.Clock = wallClock{}
	}
	return &Domain{
		getStore: deps.Store, nodes: deps.Nodes, machines: deps.Machines, visibility: deps.Visibility,
		outbound: deps.Outbound, launches: deps.Launches, admission: deps.Admission,
		clock: deps.Clock, runContext: deps.RunContext, newSessionID: deps.NewSessionID, newID: deps.NewID, startTimeout: deps.StartTimeout,
		language: deps.Language, logf: deps.Logf, wake: make(chan struct{}, 1),
	}
}

func (s *Domain) config(ctx context.Context) assistantwire.A2AConfig {
	return s.visibility.Config(ctx)
}

func (s *Domain) store() (Store, error) {
	st := s.getStore()
	if st == nil {
		return nil, errA2AUnavailable
	}
	return st, nil
}

// HasStore reports whether durable A2A is available, including when a manager swaps databases.
func (s *Domain) HasStore() bool { return s.getStore() != nil }

// MachineAllowed checks the currently configured A2A scope.
func (s *Domain) MachineAllowed(ctx context.Context, machine string) bool {
	return s.a2aMachineAllowed(ctx, s.config(ctx), machine)
}

// Discoverable reports whether to list the A2A tools for a machine or a bound session.
func (s *Domain) Discoverable(ctx context.Context, ref Ref) bool {
	cfg := s.config(ctx)
	return s.HasStore() && (ref.Session == "" && s.a2aMachineAllowed(ctx, cfg, ref.Machine) || s.a2aAllowed(ctx, cfg, ref))
}

type a2aError string

func (e a2aError) Error() string { return string(e) }

// MachineScope reads the configured scope once for a list of computers.
func (s *Domain) MachineScope(ctx context.Context) func(string) bool {
	cfg := s.config(ctx)
	return func(machine string) bool { return s.a2aMachineAllowed(ctx, cfg, machine) }
}
