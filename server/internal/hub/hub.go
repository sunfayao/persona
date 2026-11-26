// Package hub serves the iPhone / Watch apps and coordinates the nodes on every machine.
package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/hubedge"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/update"
)

type Hub struct {
	passwordLogin passwordLoginState
	browserLogin  browserLoginState
	// errs: the hub's own error reports on their way to the managers (error_reports.go).
	errs hubErrors

	deployCtx     context.Context // canceled with the hub; node updates finish before the database closes
	nodeWorkers   sync.WaitGroup
	shutdown      *shutdownBudget
	wsStopping    bool // guarded by mu; prevents new WaitGroup additions during shutdown
	wsHandlers    sync.WaitGroup
	wsConnections map[*client]struct{} // includes connections still registering their device session
	relay         relayState           // the connections relayed to the manager: Persona Link (devices_relay.go)
	linkPing      [2]time.Duration     // tests: heartbeat interval and timeout of new links
	listedWait    time.Duration        // tests: how long a started session is awaited in the list (0: listedWaitMax)
	bus           busState             // the services' connections and the roster (bus.go)
	// extProofs are the managers' proofs' nonces seen lately, extRoster the roster's answers lately about managers off
	// the bus that proofs name (extensions_proof.go).
	extProofs proofNonces
	extRoster rosterSeen
	// extLost are when this hub started and when managers' bus connections ended here lately (extensions_proof.go).
	extLost busLosses
	// clarificationProofs are short-lived, one-time full-device web question proofs.
	clarificationProofs hubClarificationProofBook
	// tests: replaces os.Hostname
	hostname func() (string, error)

	cfg     *Config
	version string
	build   update.Build // channel, build number, where updates come from
	upd     *updater     // updating itself to the publisher's release (selfupdate.go)
	// quiesce and assistantProc pause the hub's own writers for a Web
	// development snapshot (quiesce.go).
	quiesce       writeQuiescer
	assistantProc assistantGate
	// deviceFinishes: device sessions that ended while a snapshot paused
	// writes, written on resume (devicesessions.go).
	deviceFinishesMu      sync.Mutex
	deviceFinishesFlushMu sync.Mutex
	deviceFinishes        []deviceSessionFinish
	deviceFinishesRunning bool
	testAfterFinishAppend func()
	testAfterFinishRefuse func()
	clientOps             clientOpsWake
	codexPendingMu        sync.Mutex
	codexPending          map[string]*codexSaveEntry
	looksGoneMu           sync.Mutex
	looksGonePending      []string
	assistantProfileSave  assistantProfileSaveState
	logger                *log.Logger
	traffic               trafficLog // privacy-safe minute counters, flushed to the journal
	machines              []*Machine
	byID                  map[string]*Machine
	// fs.watch: generation fences stale node writes, and forced replays survive machine-ID recreation.
	fsWatchGenMu         sync.Mutex
	fsWatchGen           map[string]uint64
	fsWatchEpoch         uint64
	fsWatchEpochDeferred atomic.Bool
	fsWatchForceMu       sync.Mutex
	fsWatchForceByID     map[string]bool
	push                 *Pusher
	agentWork            agentWork // whether the manager is there to ask (agents_link.go)
	store                Store     // nil in unit tests: nothing is persisted

	terms terminals  // the terminal tab's live streams (terminal.go)
	fulls fullRelays // the web's full herdr views (herdrclient.go)

	devices devicesState // the manager's machines as it reports them (devices_link.go)
	trust   machineTrust // the computers waiting to be confirmed (machine_trust.go)
	// machineAdd: one computer is added for a pairing code at a time (machines_add.go), so two get different ids.
	machineAdd sync.Mutex
	// linkSwitch: the switches of ssh computers to Persona Link under way (link_switch.go).
	linkSwitch linkSwitches

	// handedOver: the managers' pushes and outbox items delivered lately, so one sent again is delivered once
	// (outbox_relay.go).
	handedOver seenItems

	mutesMu   sync.Mutex
	mutesSend sync.Mutex // one send of the mutes to the managers at a time (fleet_link.go)
	mutes     *Pins      // muted spaces / agents (no pushes); loaded from settings on first use

	todoOwnersMu sync.RWMutex
	todoOwners   map[string]string // todo id -> manager id

	inboxForms   inboxForms   // the inbox lists' items as localized lately (inbox_forms.go)
	inboxAnswers inboxAnswers // the merged inbox answer, reused while no manager's inbox moved (inbox_answers.go)
	// handoffRecorded: the refused approvals of hand-off cards recorded lately (secret_handoff.go).
	handoffRecorded handoffRecorded

	// machineEventsMu keeps machine-list snapshots in the order they are queued.
	machineEventsMu sync.Mutex
	// sessionsSent: each machine's last session list sent to the apps, as encoded (broadcastSessions).
	sessionsMu   sync.Mutex
	sessionsSent map[string][]sentSession
	listCache    runtimeList // opt-in indexed transport rows; initialized by the first paginated reader
	// origins: what repository each listed directory holds, as its computer said when a catalog asked
	// (sessions_catalog.go); has its own lock.
	origins placeOrigins

	mu      sync.Mutex
	clients map[*client]struct{}
	events  *eventLog // what was broadcast lately, numbered (eventlog.go); guarded by mu
	// rings: the notify events kept for the devices without a push route while they have no ws_notify socket
	// (notifyring.go); guarded by mu.
	rings notifyRings

	// linkDesktops: the computers whose Persona Link says the user is at them (desktops.go); guarded by mu.
	linkDesktops linkDesktops

	// stopping: a planned stop has begun (beginStop): what the stop itself causes (machines going
	// offline, their questions ending) is not news for the apps, which reconnect to the next hub in a moment.
	stopping atomic.Bool
	// slot: the hub's edge started this process (hub_edge.go); it listens on the edge's
	// sockets, and an update or a rollback hands over to the next slot instead of exec'ing. nil: the hub listens itself.
	slot *hubedge.Slot
	// deltas: the live text of each session's message being generated, as last relayed (machine, session -> text): a
	// client subscribing in the middle of it gets it after the backlog instead of waiting for the next token.
	deltas map[[2]string]string
	ttft   hubTTFTTracker

	// helloWrites persists client metadata in ordered, coalescing consumers (hello_writes.go).
	helloWrites helloWriteWorkers

	// askVia replaces fleet.ask in tests that play the manager (waitInteraction).
	askVia func(context.Context, proto.Interaction) (proto.Resolution, error)
	// prepared owns hub-side retained settings handles (prepared_settings.go).
	prepared preparedSettings
	// reported is what the manager told of the nodes' interactions and the fleet (fleet_link.go).
	reported fleetCache

	ports portOpens // ports the user opened in the app (ports.go)
	// assistant: what the assistant (internal/assistant, its own process on the bus) last reported (assistant_link.go).
	assistant assistantState
	owners    ownerCache    // the default manager, kept a moment (ownership.go)
	others    managerStates // the other managers' (manager_state.go)
	moving    moves         // who carries what changing (ownership_moves.go)
	// userLocale: the user's language, an i18n.Lang; langs: each device's language that decides it (locale.go)
	userLocale atomic.Value
	langs      userLangs
	deleted    deletedBook // conversations taken out of the apps' lists (deleted_sessions.go)

	deletedConversationContent map[[2]string]bool // Manager/conversation transport tombstones; guarded by mu.

	secret       secretState  // secretkey.go
	devtok       devTokens    // device token lookups (devicetokens.go)
	shareIngress shareIngress // anonymous admission limits; no sharing business state
	public       publicState  // TLS identity, listener and advertised addresses (public.go)
	sends        sendLog      // messages being / just sent, by client_msg_id (send.go)
	fresh        freshStarts  // agents started for a todo without a prompt, until their first message (assistant_todos.go)
	// ext is what the hub keeps of the extensions, connectors and bots, the manager's (extensions_link.go).
	ext   extLink
	admin adminState // the admin UI (admin.go)

	apiMux *http.ServeMux // the API's routes: the one a request goes to decides what an extension's token may do

	mesh  meshState // the other hubs of the mesh group (mesh.go)
	fleet fleet     // guards machines and byID (machines.go)
	// historyReads are the history rows an extension's or scoped token was given lately, which it may read though
	// the live list does not have them (sessions_history.go).
	historyReads historyReads

	todoLinks todoLinks // what the assistant reports of its todos' agents (assistant_todos.go)

	// The looks built into personad (persona.go; nil in unit tests) and the tier each tiered look shows (persona_tiers.go).
	builtinLooks fs.FS
	tiers        personaTierState
}

// Store is the hub's persistence (internal/db on Postgres).
type Store interface {
	InsertDeviceSession(ctx context.Context, s db.DeviceSession) (int64, error)
	UpdateDeviceSessionHeartbeat(ctx context.Context, id int64, interval int) error
	FinishDeviceSession(ctx context.Context, id int64, at time.Time, reason string, interval int) error
	StopDeviceSessions(ctx context.Context, at time.Time) error
	PruneDeviceSessions(ctx context.Context, before time.Time) error
	DeviceSessions(ctx context.Context, deviceID string, since, until time.Time) (db.DeviceSessionHistory, error)
	BeginClientOp(ctx context.Context, op db.ClientOp) (*db.ClientOp, bool, error)
	ClientOp(ctx context.Context, id string) (*db.ClientOp, error)
	FinishClientOp(ctx context.Context, id, state string, result json.RawMessage) error
	RecoverClientOps(ctx context.Context) error
	CleanupClientOps(ctx context.Context, before time.Time) error
	ClaimSend(ctx context.Context, id, machine, session string) (string, error)
	SettleSend(ctx context.Context, id, state string) error
	Devices(ctx context.Context) ([]db.Device, error)
	DeleteDevice(ctx context.Context, id string) error
	SetDevicePrefs(ctx context.Context, id, credential string, prefs json.RawMessage) error
	Setting(ctx context.Context, key string, out any) (bool, error)
	PutSetting(ctx context.Context, key string, v any) error
	TakeSetting(ctx context.Context, key string, out any) (bool, error)
	CreateDeviceToken(ctx context.Context, t db.DeviceToken, hash string) error
	DeviceTokenByHash(ctx context.Context, hash string) (*db.DeviceToken, error)
	DeviceTokens(ctx context.Context) ([]db.DeviceToken, error)
	RevokeDeviceToken(ctx context.Context, id string) (bool, error)
	TouchDeviceToken(ctx context.Context, id string) error
	CreatePairToken(ctx context.Context, hash string, expiresAt time.Time, machine string) error
	PairTokenByHash(ctx context.Context, hash string) (*db.PairToken, error)
	RedeemPairToken(ctx context.Context, pairHash string, device db.DeviceToken, tokenHash string) (bool, error)
	// A switch of an ssh computer to Persona Link (link_switch.go): whether it has a link already, and undoing exactly
	// the code a switch made and the credential it became.
	MachineLinkInUse(ctx context.Context, machine string) (bool, error)
	RevokeLinkPair(ctx context.Context, hash, machine string) (credentialID string, found bool, err error)
}

func Run(ctx context.Context, b update.Build) error {
	claimInherited() // before anything can start a process: runHub starts the manager before it listens (listen.go)
	logger := log.New(os.Stderr, "", log.LstdFlags)
	// An update that keeps failing goes back before anything else can fail, the config included (selfupdate.go).
	updating, err := newUpdater(&Hub{build: b, version: b.Version, logger: logger}).boot()
	if err != nil {
		return fmt.Errorf("update recovery: %w", err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	return runConfigured(ctx, b, cfg, updating, nil)
}

// runConfigured runs the hub until ctx ends; updating: an update is under way, this process confirms it. slot: the
// edge started it (nil: it listens itself).
func runConfigured(ctx context.Context, b update.Build, cfg *Config, updating bool, slot *hubedge.Slot) error {
	budget := newShutdownBudget(cfg)
	defer budget.close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return budget.run(ctx, func() error { return runHub(ctx, b, cfg, budget, updating, slot) })
}

func runHub(ctx context.Context, b update.Build, cfg *Config, budget *shutdownBudget, updating bool, slot *hubedge.Slot) error {
	// The stop asked for (stop) begins with beginStop, before anything sees its context end (restart.go).
	stop := ctx
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()

	h := &Hub{
		deployCtx: ctx,
		shutdown:  budget, cfg: cfg, version: b.Version, build: b, logger: slotLogger(slot),
		byID: map[string]*Machine{}, clients: map[*client]struct{}{},
		slot: slot,
	}
	defer func() { h.flushTraffic(time.Now(), true) }()
	h.extLost.started = time.Now() // managers' extensions may call before their managers are back (extensions_proof.go)
	h.teeErrors()                  // the log's error lines become reports while an extension takes them (error_reports.go)
	h.builtinLooks = builtinPersonaFS()
	h.upd = newUpdater(h)
	h.upd.confirmed = !updating // an update under way is confirmed by this process (its watchdog waits for that)
	h.upd.logSource()
	database, err := db.Open(ctx, cfg.DatabaseURL, db.HubSet)
	if err != nil {
		return err
	}
	defer h.closeDatabaseAfterWorkers(cancel, database.CloseContext)
	h.store = database
	h.startHelloWriters()
	go func() {
		select {
		case <-stop.Done():
			h.beginStop()
			cancel()
		case <-ctx.Done():
		}
	}()
	if err := h.startClientOps(ctx); err != nil {
		return err
	}
	if err := h.initHubID(ctx); err != nil {
		return err
	}
	h.loadEventLog(ctx)
	// The interactions left pending are the manager's to take back (assistant/fleet_ix.go restoreInteractions).
	h.loadUserLang(ctx)
	h.push = newPusher(h.store, h.logf)
	h.push.quiesce = &h.quiesce
	h.push.web = newWebPusher(h.store, h.logf) // browsers' subscriptions and the VAPID key, in the hub's settings (#7459)
	h.push.web.quiesce = &h.quiesce
	h.push.desktopOnline = h.desktopOnline
	h.push.desktopOpen = h.desktopOpen
	h.push.userLoc = h.userLocation
	go h.push.maintenanceLoop(ctx)
	// The machines are the manager's to connect (devices_link.go); until it reports them they show as before a
	// restart (what it handed over), before the assistant's process starts and takes that hand-over.
	h.loadMachineTrust(ctx) // before the manager connects them and the assistant asks (machine_trust.go)
	h.startDevices(ctx)
	go h.machineTrustLoop(ctx)
	h.initMesh(ctx) // before the manager connects: a member leaves its computers to the center (mesh_group.go)
	go h.terminalKeepalive(ctx)
	go h.fullKeepalive(ctx)
	h.sayMovedSections()
	h.sayRetiredKeys()
	// The assistant, its own process on the bus (assistant_process.go); it carries the machines, so the hub's stop
	// waits for it as it waited for the node connections.
	h.nodeWorkers.Add(1)
	go func() {
		defer h.nodeWorkers.Done()
		h.superviseAssistant(ctx)
	}()
	h.loadAssistantProfile(ctx) // the user's zone and the main conversation before the assistant reports (assistant_profile.go)
	h.loadExtReport(ctx)        // what the apps saw of the extensions, until the manager reports (extensions_link.go)
	go h.personaTiersLoop(ctx)
	h.upd.start(ctx)
	go h.adminStartup(ctx) // the admin UI's first-run setup code, while it has no password (admin_setupcode.go)
	if err := h.startPublic(ctx); err != nil {
		return err
	}
	h.startMesh(ctx)
	return h.serveHTTP(ctx)
}

// closeDatabaseAfterWorkers is runHub's real cleanup sequence. serveHTTP has already closed and joined app socket
// handlers when this starts; accepted hello reports drain before the finite node workers and the database close.
func (h *Hub) closeDatabaseAfterWorkers(cancel context.CancelFunc, closeDatabase func(context.Context) error) {
	cancel()
	h.shutdown.step(h, "hello writes", func(context.Context) error {
		h.stopHelloWriters()
		return nil
	})
	h.shutdown.step(h, "node connections", func(context.Context) error {
		h.nodeWorkers.Wait()
		return nil
	})
	if h.stopping.Load() {
		h.shutdown.step(h, "hand over the event log", func(ctx context.Context) error { h.saveEventLog(ctx); return nil })
	}
	// Every stop, planned or not: the next process may be a build from before hub_push_sent (push_grading.go mirrorSent).
	h.shutdown.step(h, "delivered pushes for an older build", h.push.mirrorSent)
	h.shutdown.step(h, "close database", closeDatabase)
}

func (h *Hub) logf(format string, args ...any) { h.logger.Printf(format, args...) }

func (h *Hub) broadcastMachines() {
	if h.pruneSessionListMachines() {
		h.broadcastSessionListNotice()
	}
	h.machineEventsMu.Lock()
	h.broadcast(map[string]any{"type": "machines", "machines": h.machineList()})
	h.machineEventsMu.Unlock()
	h.meshTouch() // a member's record says which of its computers the center can reach through it
	h.assistantUpdated()
}

// sessionsChanged sends the apps the machine's list as the hub has it (the manager's last, fleet_link.go).
func (h *Hub) sessionsChanged(m *Machine) {
	h.broadcastSessions(m, m.keptSessions())
}

func (h *Hub) machineName(id string) string {
	if m := h.localMachine(id); m != nil {
		return m.name()
	}
	return id
}
