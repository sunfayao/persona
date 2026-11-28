package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/node/harness/agy"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
	"github.com/sunfayao/persona/server/internal/transcript"
)

type agyPaneBinding struct {
	conversation string
	verified     bool
}

type agyCandidateDecision uint8

const (
	agyCandidateSubagent agyCandidateDecision = iota
	agyCandidatePending
	agyCandidateNormal
	agyCandidateUnverified
)

func reportedAgyConversation(a host.Agent) string {
	s := a.AgentSession
	if a.Agent == agyAgent && s != nil && s.Source == "herdr:antigravity_cli" &&
		s.Agent == agyAgent && s.Kind == "id" && uuidRe.MatchString(s.Value) {
		return s.Value
	}
	return ""
}

// rawAgyConversationLocked checks the latest raw herdr snapshot as well as the pane row.
// The row ID is pane-derived. Cwd and previously bound session IDs cannot identify
// a native conversation, and ambiguous reports do not authorize a read or resume.
// While herdr reports none, a conversation Persona proved for its own start counts (agyproof.go); agyConversation
// then checks it like a reported one (a subagent's conversation is never bound).
func (n *Node) rawAgyConversationLocked(id, pane string) string {
	a := n.agents[id]
	conversation := reportedAgyConversation(a)
	if conversation == "" {
		return n.agyProvenLocked(id, pane)
	}
	if a.PaneID != pane || id != otherSession(a) || host.ParsePane(pane).Kind != host.Herdr {
		return ""
	}
	matches := 0
	for _, raw := range n.hostAgents[host.Herdr] {
		reported := reportedAgyConversation(raw)
		if raw.PaneID == pane {
			if reported != conversation {
				return ""
			}
			matches++
		} else if reported == conversation {
			return ""
		}
	}
	if matches != 1 {
		return ""
	}
	return conversation
}

func (n *Node) agyConversationClaimedElsewhereLocked(pane, conversation string) bool {
	for _, raw := range n.hostAgents[host.Herdr] {
		if raw.PaneID != pane && reportedAgyConversation(raw) == conversation {
			return true
		}
	}
	return false
}

// agyContent is the reader surface shared by pane and runtime routes.
type agyContent interface {
	Read(agy.Ref) (*agy.Projection, error)
	Placeholder(id, text string) *agy.Projection
}

type agyClock struct{}

func (agyClock) Now() time.Time { return time.Now() }

func (n *Node) agyReader() agyContent {
	n.agyOnce.Do(func() { n.agy = agy.New(agy.Dependencies{Clock: agyClock{}}) })
	return n.agy
}

func (n *Node) agyRef(conversation string) agy.Ref {
	return agy.Ref{Root: n.agyDataRoot(), Conversation: conversation}
}

func (n *Node) agyTranscript(conversation string) (*agy.Projection, bool) {
	if conversation == "" {
		return nil, false
	}
	h, err := n.agyReader().Read(n.agyRef(conversation))
	return h, err == nil
}

func (n *Node) probeAgyConversation(conversation string) agy.Kind {
	if h, ok := n.agyTranscript(conversation); ok {
		return h.Kind()
	}
	return agy.Unreadable
}

func (n *Node) classifyAgyCandidate(parent, candidate string) agyCandidateDecision {
	kind := n.probeAgyConversation(candidate)
	if kind == agy.Subagent {
		return agyCandidateSubagent
	}
	p, ok := n.agyTranscript(parent)
	if ok && p.HasChild(candidate) {
		return agyCandidateSubagent
	}
	switch kind {
	case agy.Normal:
		return agyCandidateNormal
	case agy.Empty, agy.Unreadable:
		return agyCandidatePending
	default:
		if ok && p.InvokesChild() {
			return agyCandidatePending
		}
		return agyCandidateUnverified
	}
}

// agyConversation resolves the pane's active parent agy conversation while ignoring
// built-in subagent conversation IDs reported via inherited HERDR_PANE_ID hooks (#13336).
func (n *Node) agyConversation(id, pane string) string {
	for range 3 {
		n.mu.Lock()
		raw := n.rawAgyConversationLocked(id, pane)
		if raw == "" {
			delete(n.agyBound, pane)
			n.mu.Unlock()
			return ""
		}
		bound := n.agyBound[pane]
		if bound.conversation != "" && n.agyConversationClaimedElsewhereLocked(pane, bound.conversation) {
			bound = agyPaneBinding{}
			delete(n.agyBound, pane)
		}
		if bound.conversation == raw && bound.verified {
			n.mu.Unlock()
			return raw
		}
		n.mu.Unlock()

		var next agyPaneBinding
		var result string
		var keepBound bool

		if bound.conversation == "" || bound.conversation == raw {
			switch n.probeAgyConversation(raw) {
			case agy.Subagent:
				next = agyPaneBinding{}
				result = ""
			case agy.Normal:
				next = agyPaneBinding{conversation: raw, verified: true}
				result = raw
			default:
				next = agyPaneBinding{conversation: raw, verified: false}
				result = raw
			}
		} else {
			switch n.classifyAgyCandidate(bound.conversation, raw) {
			case agyCandidateSubagent, agyCandidatePending:
				keepBound = true
				next = bound
				result = bound.conversation
			case agyCandidateNormal:
				next = agyPaneBinding{conversation: raw, verified: true}
				result = raw
			case agyCandidateUnverified:
				next = agyPaneBinding{conversation: raw, verified: false}
				result = raw
			}
		}

		n.mu.Lock()
		if n.rawAgyConversationLocked(id, pane) != raw {
			n.mu.Unlock()
			continue
		}
		if keepBound && n.agyConversationClaimedElsewhereLocked(pane, bound.conversation) {
			delete(n.agyBound, pane)
			n.mu.Unlock()
			continue
		}
		if next.conversation == "" {
			delete(n.agyBound, pane)
		} else {
			if n.agyBound == nil {
				n.agyBound = make(map[string]agyPaneBinding)
			}
			n.agyBound[pane] = next
		}
		n.mu.Unlock()
		return result
	}
	return ""
}

// agyNotice is shown only until the pane has a verified conversation to read.
func (n *Node) agyNotice(id string) *agy.Projection {
	return n.agyReader().Placeholder(id, i18n.T(i18n.Default, "node-engine.agy_pane_integration"))
}

type agyWatch struct {
	pane, history, conversation string
	since                       int
	last                        *agy.Projection
}

func (n *Node) agyPaneCall(ctx context.Context, method, id, pane string, raw json.RawMessage) (any, error, bool) {
	switch method {
	case "session.unwatch":
		n.unwatch(id)
		return okResult(nil), nil, true
	case "session.info":
		return n.agyPaneSessionInfo(id, pane), nil, true
	case "session.items", "session.item", "session.image", "session.watch":
	default:
		return nil, nil, false
	}
	var p agy.PageParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err, true
	}
	if method == "session.watch" {
		n.watchAgy(rpc.ConnContext(ctx), id, pane, p.SinceSeq, p.History)
		return okResult(nil), nil, true
	}
	return n.agyContentCall(method, id, pane, p)
}

func (n *Node) agyContentRef(id, pane string) agy.Ref {
	if pane != "" {
		return n.agyRef(n.agyConversation(id, pane))
	}
	return n.runtimes.agyOf(id)
}

func (n *Node) agyContentCall(method, id, pane string, p agy.PageParams) (any, error, bool) {
	ref := n.agyContentRef(id, pane)
	projection := n.agyNotice(id)
	if ref.Conversation != "" {
		h, err := n.agyReader().Read(ref)
		if err != nil {
			return nil, fmt.Errorf("read conversation transcript: %w", err), true
		}
		if n.agyContentRef(id, pane) == ref {
			projection = n.agyEchoed(id, ref.Conversation, h)
		}
	}
	switch method {
	case "session.item", "session.image":
		var out any
		var err error
		if method == "session.item" {
			out, err = projection.Item(p.ItemID)
		} else {
			out, err = projection.Image(p.ItemID)
		}
		if n.agyContentRef(id, pane) != ref || errors.Is(err, agy.ErrNotFound) {
			return nil, &rpc.Error{Code: codeNotFound, Message: "no item"}, true
		}
		return out, err, true
	}
	return projection.Page(p), nil, true
}

func (n *Node) agyPaneSessionInfo(id, pane string) map[string]any {
	info := map[string]any{
		"session_id": id, "agent": agyAgent,
		"context_tokens": 0, "output_tokens": 0, "compactions": 0, "turns": 0,
		"usage_unknown": true,
	}
	if a, ok := n.agent(id); ok {
		info["cwd"] = a.Cwd
	}
	if st, ok := n.state.get(id); ok {
		if st.Launcher != "" {
			info["launcher"] = st.Launcher
		}
		if st.Model != "" {
			info["model"] = st.Model
		}
	}
	conversation := n.agyConversation(id, pane)
	if conversation != "" {
		if runtimeRow, _, ok := n.runtimes.rowOf(agyAgent + "-" + conversation); ok {
			if runtimeRow.Model != "" {
				info["model"] = runtimeRow.Model
			}
			if runtimeRow.PermissionMode != "" {
				info["permission_mode"] = runtimeRow.PermissionMode
			}
			if runtimeRow.Effort != "" {
				info["effort"] = runtimeRow.Effort
			}
		}
		if h, ok := n.agyTranscript(conversation); ok {
			h.FillInfo(info)
			if model, _ := h.Model(); info["model"] == nil && model != "" {
				info["model"] = model
			}
		}
	}
	n.applyAgyPaneInfo(id, conversation, info)
	return info
}

func (n *Node) watchAgy(parent context.Context, id, pane string, since int, history string) {
	n.mu.Lock()
	if cur := n.watches[id]; cur != nil {
		n.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w := &watch{cancel: cancel, ctx: ctx, parent: parent, id: id, agy: &agyWatch{pane: pane, since: since, history: history}}
	n.watches[id] = w
	if !n.pumping {
		n.pumping = true
		go n.watchPump()
	}
	n.mu.Unlock()
	context.AfterFunc(ctx, func() { n.watchEnded(w) })
	n.pollAgyPane(w)
	w.armed.Store(true)
}

func (n *Node) pollAgyPane(w *watch) {
	mu := n.pushLock(w.id)
	mu.Lock()
	defer mu.Unlock()
	if w.ctx.Err() != nil {
		return
	}
	a := w.agy
	ref := n.agyContentRef(w.id, a.pane)
	var h *agy.Projection
	if ref.Conversation != "" {
		var err error
		h, err = n.agyReader().Read(ref)
		if err != nil || n.agyContentRef(w.id, a.pane) != ref || h == a.last {
			return
		}
	} else if a.pane != "" {
		h = n.agyNotice(w.id)
	} else {
		return
	}
	since := a.since
	reset := a.history != "" && a.history != h.History() || since > h.Seq()
	initial := a.last == nil
	if initial && since == 0 && !reset {
		since = h.Seq() // the first page is the subscriber's backlog
	}
	echoed := n.agyEchoed(w.id, ref.Conversation, h)
	if reset {
		since = 0
	} else if updated := echoed.Updates(a.last, since); len(updated) > 0 {
		span := transcript.Span{History: h.History(), From: since, To: since}
		if !n.pushAgy(w, ref, agy.LightItems(updated), span) {
			return
		}
	}
	for reset || since < h.Seq() {
		items, span, more := echoed.After(since)
		if !n.pushAgy(w, ref, items, span) {
			return
		}
		a.since, since, reset = span.To, span.To, false
		a.history = h.History()
		if !more {
			break
		}
	}
	a.history, a.last, a.conversation, a.since = h.History(), h, ref.Conversation, since
}

func (n *Node) pushAgy(w *watch, ref agy.Ref, items []proto.Item, span transcript.Span) bool {
	if n.agyContentRef(w.id, w.agy.pane) != ref || w.ctx.Err() != nil {
		return false
	}
	if n.peer != nil {
		if err := n.peer.NotifyContext(w.ctx, "session.items", withSpan(map[string]any{"session_id": w.id, "items": items}, span, true)); err != nil {
			return false
		}
	}
	if w.agy.pane == "" {
		n.runtimes.pushed(w.id, span.To, span.History, span.From == 0)
	}
	return true
}

func (n *Node) agyEchoed(id, conversation string, h *agy.Projection) *agy.Projection {
	if !n.echoes.known(id) || len(h.Items()) == 0 {
		return h
	}
	if conversation != "" {
		n.echoes.syncAgyConversation(id, conversation, h.History())
	}
	if from, ok := n.echoes.waitingFrom(id, echoInTranscript); ok {
		items := h.Items()
		for _, r := range n.echoes.match(id, echoInTranscript, items[agy.AfterIndex(items, from):]) {
			n.notifyEcho(r)
		}
	}
	return h.WithItems(n.echoes.tag(id, echoInTranscript, h.Items()))
}

func (n *Node) pollWatchedAgyPane(id string) {
	n.mu.Lock()
	w := n.watches[id]
	n.mu.Unlock()
	if w != nil && w.agy != nil {
		n.pollAgyPane(w)
	}
}

func (n *Node) agyEchoBase(id, pane string) echoAfter {
	n.pollWatchedAgyPane(id)
	mu := n.pushLock(id)
	mu.Lock()
	defer mu.Unlock()
	ref := n.agyContentRef(id, pane)
	if ref.Conversation == "" {
		return echoAfter{in: echoInTranscript}
	}
	h, err := n.agyReader().Read(ref)
	if err != nil || n.agyContentRef(id, pane) != ref {
		return echoAfter{in: echoInTranscript}
	}
	n.agyEchoed(id, ref.Conversation, h)
	return echoAfter{seq: h.Seq(), in: echoInTranscript, history: h.History()}
}
