package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// pushWants is which devices' settings take a push.
type pushWants struct {
	Kind string        // "" every device | input | done
	Took time.Duration // done: how long the turn took
	Long time.Duration // done: what counts as a long one
}

func (w pushWants) fn() func(NotifyPrefs) bool {
	switch w.Kind {
	case "input":
		return func(p NotifyPrefs) bool { return p.Input }
	case "done":
		return func(p NotifyPrefs) bool { return p.wantsDone(w.Took, w.Long) }
	}
	return nil
}

// pushOutcome is where one push went: the devices it was handed to now (a ws_notify connection or a
// browser's Web Push), the devices that could take this kind before their settings decided, the offline devices that
// keep it for their next connection (notifyring.go), and the ln-mode reason the push was held ("" when it was not).
// None of these is a delivery receipt.
type pushOutcome struct {
	devices, reachable, kept int
	held                     string
}

// reason is why nothing took the push now ("" when a device did): "no_device" when no device could take it,
// "filtered" when every one that could turned it off, as POST /api/v1/notify answers.
func (o pushOutcome) reason() string {
	switch {
	case o.devices > 0:
		return ""
	case o.reachable > 0:
		return "filtered"
	}
	return "no_device"
}

// notifyDoneUnder is notifyDone under a push policy. It returns
// where the push went, with the ln-mode reason it was held, so the assistant can tell the model whether a notify_user
// notice reached a device, the briefing or nothing.
func (h *Hub) notifyDoneUnder(owner, kind, machineID, machine, sessionID, title, text string, wants pushWants, policy pushPolicy) pushOutcome {
	build := func(l i18n.Lang) map[string]any {
		return donePayloadIn(l, machineID, machine, sessionID, title, text)
	}
	if !h.sessionHiddenFromExtensions(machineID, sessionID) { // only of a session the extensions are shown (extension_private.go)
		h.emitToExtensions(owner, "agent.finished", map[string]any{"machine_id": machineID, "machine": machine, "session_id": sessionID, "title": title})
	}
	policy.manager, policy.title, policy.body = owner, title, text
	heldReason := h.push.grade(owner, policy, build)
	var ws wsReach
	if heldReason == "" {
		ws = h.notifyWSFrom(owner, kind, build, wants.fn(), wants.quiet())
	}
	return pushOutcome{devices: ws.selected, reachable: ws.reachable, kept: ws.kept, held: heldReason}
}

// notifyReplyKeyed is notifyReply with the manager's related key for the question it waits on (low-distraction mode keeps one key
// quiet for 12 hours after it rang): reminders of the same question merge, a new question of the same agent rings.
// "" (an older manager) keys by the session and what it asks.
func (h *Hub) notifyReplyKeyed(owner, machineID, machine, sessionID, title, text, key string) {
	build := func(l i18n.Lang) map[string]any { return donePayloadIn(l, machineID, machine, sessionID, title, text) }
	wants := pushWants{Kind: "input"}
	policy := pushPolicy{level: assistantwire.LevelP0, relatedKey: replyKey(sessionID, text, key), title: title, body: text}
	if heldReason := h.push.grade(owner, policy, build); heldReason == "" {
		h.notifyWSFrom(owner, "reply", build, wants.fn(), wants.quiet())
	}
}

// replyKey is the related key of a reply reminder: the manager's when it named one, else the session and a digest of
// the question, so one agent's second question is not taken for a repeat of its first.
func replyKey(sessionID, text, given string) string {
	if given = strings.TrimSpace(given); given != "" {
		return given
	}
	sum := sha256.Sum256([]byte(sessionID + "\x00" + strings.TrimSpace(text)))
	return "reply:" + sessionID + ":" + hex.EncodeToString(sum[:8])
}

// notifyDecide pushes a to-do whose conclusion waits for the owner's decision and recommends an option (#2056) to the
// devices that take "needs you" pushes: category TODO_DECIDE, whose OK takes the recommendation, or TODO_DECIDE_OPEN,
// with no action, when the manager says open_only (decidePayloadIn). On the WebSocket it is an "assistant" notify
// carrying the same `todo`: a client that does not know it shows a plain notification. Nothing finished: the
// extensions hear no agent.finished. One collapse id a to-do: a later round's push takes the place of the earlier one,
// whose OK could only answer "already handled".
func (h *Hub) notifyDecide(owner, machineID, machine, sessionID, title, text string, todo assistantwire.NotifyTodo) {
	build := func(i18n.Lang) map[string]any {
		return decidePayloadIn(owner, machineID, machine, sessionID, title, text, todo)
	}
	wants := pushWants{Kind: "input"}
	// One key a round: the same decision does not ring twice in 12 hours, but a new round of it (asked again) does.
	policy := pushPolicy{level: assistantwire.LevelP0, relatedKey: "todo:" + todo.ID + "@" + todo.AskedAt.UTC().Format(time.RFC3339Nano),
		title: title, body: text}
	if heldReason := h.push.grade(owner, policy, build); heldReason == "" {
		h.notifyWSFrom(owner, "assistant", build, wants.fn(), wants.quiet())
	}
}

// notifyInteractionAt is notifyInteraction with the manager's push level and related key for low-distraction mode (push_quiet.go):
// "" is a card's own, P0 keyed by the card. A supervisor's escalation names P1 when what it asks is not the owner's
// alone, and low-distraction mode then holds it for the "Needs you" list instead of alerting.
func (h *Hub) notifyInteractionAt(owner string, ix proto.Interaction, machine string, linkCovered bool, level, key string) {
	switch level {
	case assistantwire.LevelP0, assistantwire.LevelP1, assistantwire.LevelP2:
	default:
		level = assistantwire.LevelP0 // none, or one this hub does not know: a card interrupts
	}
	if key = strings.TrimSpace(key); key == "" {
		key = "ix:" + ix.ID
	}
	build := func(l i18n.Lang) map[string]any { return interactionPayloadIn(l, ix, machine) }
	summary := ix.Summary
	if ix.Private { // a Just me turn's card: what it asks stays in the app
		summary = ""
	}
	if !h.hiddenFromExtensions(ix) { // only of a card the extensions are shown (extension_private.go)
		h.emitToExtensions(owner, "agent.needs_you", map[string]any{"machine_id": ix.MachineID, "machine": machine, "session_id": ix.SessionID,
			"interaction_id": ix.ID, "kind": ix.Kind, "summary": summary})
	}
	wants := pushWants{Kind: "input"}
	if ix.Kind == proto.IxElicitation {
		// Only what can show an MCP form or link: the connections that declared it.
		policy := pushPolicy{level: level, relatedKey: key, title: summary}
		if heldReason := h.push.grade(owner, policy, build); heldReason == "" {
			h.notifyWSFrom(owner, "interaction", build, wants.fn(), wants.quiet(), "elicitation")
		}
	} else {
		// A card owner's Persona Link summary rings for: "Phone Muted" holds it back while one of owner's Persona Link
		// desktops is there too (desktops.go); which cards those are is owner's to say. The desktop app's online check
		// follows in notifyWSFrom.
		fn := alertWanted(wants.fn(), linkCovered && h.linkDesktopOnline(owner))
		policy := pushPolicy{level: level, relatedKey: key, title: summary}
		if heldReason := h.push.grade(owner, policy, build); heldReason == "" {
			h.notifyWSFrom(owner, "interaction", build, fn, wants.quiet())
		}
	}
}

// notifyWSFrom projects a push onto the opt-in WebSocket notification event, written in each connection's language
// (client.lang), by the authenticated device's preferences, even without an APNs token or key, and with what its quiet
// hours do to it (quiethours.go): skipped, or marked "quiet" for the client to post without a sound. manager is the
// one that asked for it: a device that turned that manager's pushes off (NotifyPrefs.MutedManagers) gets none, as with
// its push (Pusher.sendFrom); "" is the hub's own, for every device wants takes. Returns the selected count and the
// count of capable connections before preferences are applied, each with the browsers its Web Push went to (or could
// go to); neither is a client delivery receipt. A device without a push route keeps what its settings take for its
// next connection when no ws_notify socket accepts it (notifyring.go). kept counts those devices; a rejected socket
// still counts as selected.
func (h *Hub) notifyWSFrom(manager, kind string, build localized, wants func(NotifyPrefs) bool, rule quietRule, required ...string) wsReach {
	msgs := map[i18n.Lang]map[string]any{}
	msgIn := func(l i18n.Lang) map[string]any {
		if m, ok := msgs[l]; ok {
			return m
		}
		m := notifyMessage(kind, build(l))
		msgs[l] = m
		return m
	}
	type form struct {
		lang  i18n.Lang
		quiet bool
	}
	encoded := map[form][]byte{}
	msgFor := func(l i18n.Lang, quiet bool) []byte {
		if b, ok := encoded[form{l, quiet}]; ok {
			return b
		}
		msg := msgIn(l)
		if quiet {
			msg = maps.Clone(msg)
			msg["quiet"] = true
		}
		b, err := json.Marshal(msg)
		if err != nil {
			b = nil // sent nowhere, as sendJSON did
		}
		encoded[form{l, quiet}] = b
		return b
	}
	// Mute phone holds these back while a desktop is online, with the same rule as the APNs pushes (alertWanted): an
	// Android phone alerts from them. desktopOnline takes h.mu, so it is read before.
	wants = alertWanted(wants, h.desktopOnline())
	// Read before h.mu as well: the user's time zone asks the assistant's profile.
	now, loc := time.Now(), (*time.Location)(nil)
	if rule != quietNone && h.push != nil {
		now, loc = h.push.now(), h.userLocation()
	}
	// wantsOn is whether device's settings take it, and whether inside its quiet hours (posted without a sound).
	wantsOn := func(device string) (ok, quiet bool) {
		prefs := defaultPrefs()
		if h.push != nil {
			prefs = h.push.prefs(device)
		}
		if prefs.mutes(manager) || wants != nil && !wants(prefs) {
			return false, false
		}
		quiet = rule != quietNone && prefs.Quiet.activeAt(now, loc)
		return !(quiet && rule == quietSkip), quiet
	}
	var reach wsReach
	h.mu.Lock()
	defer h.mu.Unlock()
	live := map[string]bool{} // devices whose ws_notify socket accepted this event
	for c := range h.clients {
		if !c.has("ws_notify") {
			continue
		}
		if len(required) > 0 && !c.has(required[0]) {
			continue
		}
		reach.reachable++
		ok, quiet := wantsOn(c.device)
		if !ok {
			continue
		}
		if b := msgFor(c.lang(), quiet); b != nil && c.queue(b) {
			live[c.device] = true
		}
		reach.selected++
	}
	reach.kept = h.ringNotify(live, required, wantsOn, msgIn, msgFor)
	sent, capable := h.webPushNotify(kind, required, wantsOn, msgIn) // the browsers with no page open (webpush.go)
	reach.selected += sent
	reach.reachable += capable
	return reach
}

// wsReach is where notifyWSFrom put one notify event: selected and reachable as it says, and kept, the devices
// that keep it for their next connection (offline or rejected by every eligible socket).
type wsReach struct {
	selected, reachable, kept int
}

func notifyMessage(kind string, payload map[string]any) map[string]any {
	payload = cleanAlert(payload)
	aps := payload["aps"].(map[string]any)
	alert := aps["alert"].(map[string]any)
	msg := map[string]any{
		"type": "notify", "kind": kind,
		"machine_id": payload["machine_id"], "session_id": payload["session_id"],
		"title": alert["title"], "subtitle": alert["subtitle"], "body": alert["body"],
	}
	if id, ok := payload["interaction_id"]; ok {
		msg["interaction_id"] = id
	}
	if id, ok := payload["id"]; ok { // alert: the caller's id (POST /api/v1/notify)
		msg["id"] = id
	}
	if en, ok := payload["extension_notification"]; ok {
		msg["extension_notification"] = en
	}
	if td, ok := payload["todo"]; ok { // a decision's push (decidePayloadIn)
		msg["todo"] = td
	}
	return msg
}
