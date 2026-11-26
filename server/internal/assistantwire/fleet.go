package assistantwire

import (
	"github.com/nwverse/persona/server/internal/proto"
)

// What comes from the nodes is the manager's: it creates and answers the
// interactions, works out each Agent's state, decides the pushes and the Persona Link summary's content, runs the nodes' MCP
// tools and keeps the machines' monitoring. The hub shows and delivers what it reports: the session lists, the
// pending interactions and the fleet (kept while the manager is away), and the pushes (APNs, the relay, the apps'
// notify events).

// Manager → hub, one way.
const (
	// EventFleetSessions is a machine's session list as the apps get it (FleetSessions): after every change.
	EventFleetSessions = "fleet.sessions"
	// EventFleetArchived is a machine's archived rows (FleetArchived), to a hub whose hello says Hello.ArchivedApart:
	// the rows of the machine's list that are archived (proto.Session.Archived, not the assistant's), which the apps
	// never see. EventFleetSessions then leaves them out. Told when they change, and once per machine after each
	// handshake; the hub reads them wherever it read the manager's list, except what the apps get (#6836).
	EventFleetArchived = "fleet.archived"
	// EventFleetInteraction is a pending interaction that is new or shown again (FleetInteraction).
	EventFleetInteraction = "fleet.interaction"
	// EventFleetResolved is an interaction that is over (FleetResolved).
	EventFleetResolved = "fleet.interaction_resolved"
	// EventFleetInteractions is every pending interaction (FleetInteractions): after each handshake, the hub's list
	// replaced by it.
	EventFleetInteractions = "fleet.interactions"
)

// Hub → manager, one way.
const (
	// EventClients is what the hub's connected apps can take now (Clients), after every change.
	EventClients = "fleet.clients"
	// EventMutes is the muted spaces / agents (FleetMutes), the hub's: once after each handshake (on the manager's
	// first word: they are in the hello's answer too, Hello.Mutes) and whenever the user changes them.
	EventMutes = "fleet.mutes"
	// EventSeen: an app looked at a session (its WebSocket's seen) (FleetSeen).
	EventSeen = "fleet.seen"
)

// InteractionsOwnerAction has the manager's channels take the owner action they handed over with a channel's press or
// message, for the card the assistant is about to answer with it in channel ({interaction_id, action, channel, kind}
// → {}, kind the card's in the queue; an error: refused).
// Answered in the manager's process (internal/extmgr channels_bus.go).
const InteractionsOwnerAction = "interactions.owner_action"

// MethodFleetAsk is a card one of the hub's own features asks the user (a mail send, Cua's apps, persona_settings):
// {interaction} → proto.Resolution, when it is answered, expires or the call ends.
const MethodFleetAsk = "fleet.ask"

// MethodFleetAskPrepared is the hub's retained settings operation:
// {interaction, replacement} → approval.Outcome. The executable handle stays in
// the hub; only its random interaction ID crosses the bus.
const MethodFleetAskPrepared = "fleet.ask_prepared"

// MethodFleetApprovalApply consumes that hub handle once after a fresh owner
// Allow. Resolution.Values travels only on this trusted manager/hub call.
const MethodFleetApprovalApply = "fleet.approval_apply"

// MethodFleetApprovalDiscard releases a handle without applying it.
const MethodFleetApprovalDiscard = "fleet.approval_discard"

type FleetApprovalApply struct {
	InteractionID string           `json:"interaction_id"`
	Resolution    proto.Resolution `json:"resolution"`
}

type FleetApprovalApplyResult struct {
	Text  string `json:"text"`
	Stale bool   `json:"stale,omitempty"`
}

type FleetApprovalDiscard struct {
	InteractionID string `json:"interaction_id"`
	Status        string `json:"status"`
}

// ElicitationHeader marks an app's request the hub relays to the manager as coming from an app that takes MCP
// elicitations ("1"): the interaction history keeps them out otherwise.
const ElicitationHeader = "X-Persona-Elicitation"

// FleetSessions is EventFleetSessions' params. Muted are the sessions whose pushes are off.
type FleetSessions struct {
	MachineID string          `json:"machine_id"`
	Sessions  []proto.Session `json:"sessions"`
	Muted     []string        `json:"muted,omitempty"`
}

// FleetArchived is EventFleetArchived's params: all of the machine's archived rows, replacing the ones told before.
type FleetArchived struct {
	MachineID string          `json:"machine_id"`
	Sessions  []proto.Session `json:"sessions"`
}

// FleetInteraction is EventFleetInteraction's params.
type FleetInteraction struct {
	Interaction proto.Interaction `json:"interaction"`
}

// FleetResolved is EventFleetResolved's params; AnsweredBy says the assistant answered it, Reason why and Answers
// with what.
type FleetResolved struct {
	ID         string            `json:"id"`
	AnsweredBy string            `json:"answered_by,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Answers    map[string]string `json:"answers,omitempty"`
}

// FleetInteractions is EventFleetInteractions' params, oldest first.
type FleetInteractions struct {
	Interactions []proto.Interaction `json:"interactions"`
}

// Clients is EventClients' params.
type Clients struct {
	// Elicitation: an app that takes MCP elicitations is connected (without one, an elicitation is handed back to the
	// terminal at once).
	Elicitation bool `json:"elicitation"`
}

// FleetSeen is EventSeen's params.
type FleetSeen struct {
	MachineID string `json:"machine_id"`
	SessionID string `json:"session_id"`
}

// The kinds of push the manager asks the hub for (Notification.Kind besides NotifyPush).
const (
	// NotifyDone is an agent's finished turn: the devices whose settings take a turn of TookMS (LongMS counts as
	// long) get it.
	NotifyDone = "done"
	// NotifyInput is a pending interaction (Notification.Interaction): the devices that take "needs you" pushes.
	NotifyInput = "input"
	// NotifyReply reminds the user of an agent still waiting on their answer (Session.AwaitsReply, #5096): the devices
	// that take "needs you" pushes; answerable from the notification as a finished turn is.
	NotifyReply = "reply"
	// NotifyDecide is a to-do whose conclusion waits for the owner's decision with a recommended option
	// (Notification.Todo, #2056): the devices that take "needs you" pushes; the notification's OK takes the
	// recommendation through POST /api/v1/todos/{id}/accept with its asked_at.
	NotifyDecide = "decide"
)

// UsageSummary is the token usage across the machines (GET /api/v1/usage, the manager's).
type UsageSummary struct {
	TPM      int64 `json:"tpm"`      // output tokens per minute, over the last two to three minutes
	Tokens   int64 `json:"tokens"`   // today: all tokens, cache reads included
	Output   int64 `json:"output"`   // today
	Requests int   `json:"requests"` // today
	// Input (not from the cache), CacheWrite and CacheRead: today, for the cache hit rate
	// (CacheRead / (Input + CacheWrite + CacheRead)) on 机器's card of today's tokens.
	Input      int64          `json:"input"`
	CacheWrite int64          `json:"cache_write"`
	CacheRead  int64          `json:"cache_read"`
	Machines   []MachineUsage `json:"machines"`
	// Hourly: today's tokens (as Tokens, cache included) for each hour from midnight in the user's zone, 24 of them
	// (the hours to come 0): the iPhone's Usage widget draws them as a chart (#5664, #5721).
	Hourly []int64 `json:"hourly,omitempty"`
}

// MachineUsage is one machine's share and load; Load is the 1-minute load average over its cores, 0 when unknown.
type MachineUsage struct {
	ID     string  `json:"id"`
	TPM    int64   `json:"tpm"`
	Tokens int64   `json:"tokens"`
	Load   float64 `json:"load"`
}

// FleetMutes is fleet.mutes' params. The hub keeps the mutes (its settings; GET / PUT /api/v1/mutes); the manager
// decides the pushes by them.
type FleetMutes struct {
	// Mutes are the mutes now; nil from a hub before they came with the event (the manager keeps the ones it had).
	Mutes *Mutes `json:"mutes,omitempty"`
}

// Mutes: muted spaces ("machine/space name") and agents ("machine/session id"); Allowed are agents that keep their
// notifications although their space is muted.
type Mutes struct {
	Spaces   []string `json:"spaces"`
	Sessions []string `json:"sessions"`
	Allowed  []string `json:"allowed,omitempty"`
}
