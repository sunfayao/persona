package node

import (
	"encoding/json"
	"io"
	"time"

	"github.com/sunfayao/persona/server/internal/node/harness/codex"
	"github.com/sunfayao/persona/server/internal/node/harness/codex/appserver"
	"github.com/sunfayao/persona/server/internal/proto"
)

// codexLineMax is the longest app-server line read; a var so tests can lower it.
var codexLineMax = 64 * 1024 * 1024

func (c *codexConn) notification(method string, params json.RawMessage) {
	n, p := c.p.ownerOr(c.n), c.p
	ev, ok := appserver.DecodeEvent(method, params)
	if !ok {
		return
	}
	c.mu.Lock()
	thread := c.threadID
	c.mu.Unlock()
	if ev.ThreadID != "" && thread != "" && ev.ThreadID != thread {
		return // a subagent's thread: its items are in the main rollout's events anyway
	}
	if n.lifecycle != nil && (ev.Kind == appserver.TurnStarted || ev.Kind == appserver.TurnCompleted) {
		if err := n.lifecycle.CodexTurn(p.id, ev.Turn.ID, method, ev.Turn.Status); err != nil {
			n.logger.Printf("lifecycle Codex turn: %v", err)
		}
	}
	switch ev.Kind {
	case appserver.ThreadSettings:
		c.mu.Lock()
		c.speedModel = firstNonEmpty(ev.Settings.Model, c.speedModel)
		if tier := codex.ServiceTier(ev.Settings.ServiceTier); tier != "" {
			c.serviceTier = tier
			c.serviceTierRevision++
		}
		c.mu.Unlock()
		n.notifyCodex()
	case appserver.TurnStarted:
		n.ttft.armIfUnarmed(p.id, time.Now(), ev.Turn.ID)
		c.mu.Lock()
		c.turnID = ev.Turn.ID
		c.loop = codexCompactionLoop{}
		c.mu.Unlock()
		p.mu.Lock()
		had := p.partial.Len() > 0
		p.partial.Reset()
		p.turnError = ""
		p.turnErrorKind = ""
		resumed := !p.busy
		p.busy = true
		p.lastActive = time.Now()
		p.mu.Unlock()
		if resumed {
			n.notifySessions()
		}
		if had {
			n.emitDelta(p.id, "")
		}
	case appserver.ItemStarted:
		if ev.Item.Type == "fileChange" {
			c.mu.Lock()
			c.files[ev.Item.ID] = ev.Item.Paths()
			c.mu.Unlock()
		}
		if ev.Item.Type != "agentMessage" {
			return
		}
		// A new message: the last one is in the rollout by now; clear the live copy so it is not shown twice.
		p.mu.Lock()
		had := p.partial.Len() > 0
		p.partial.Reset()
		p.lastActive = time.Now()
		p.mu.Unlock()
		if had {
			n.emitDelta(p.id, "")
		}
	case appserver.AgentMessageDelta:
		if ev.Delta != "" {
			n.ttft.firstToken(p.id, time.Now(), ev.TurnID)
		}
		p.mu.Lock()
		p.partial.WriteString(ev.Delta)
		text := n.partialGrewLocked(p)
		p.lastActive = time.Now()
		p.mu.Unlock()
		if text != "" {
			n.emitDelta(p.id, text)
		}
	case appserver.ReasoningDelta:
		if ev.Delta != "" {
			n.ttft.firstToken(p.id, time.Now(), ev.TurnID)
		}
	case appserver.TokenUsageUpdated:
		c.noteRequest(ev.TokenUsage.Last.InputTokens, ev.TokenUsage.ModelContextWindow, c.turnModel())
	case appserver.ThreadCompacted:
		c.compacted()
	case appserver.ItemCompleted:
		c.mu.Lock()
		delete(c.files, ev.Item.ID)
		c.mu.Unlock()
		if ev.Item.Type == "contextCompaction" {
			c.compacted()
		}
		if ev.Item.Type == "agentMessage" {
			n.emitDelta(p.id, ev.CompletedMessage(time.Now())) // until history contains the item
		}
		if f, err := n.file(p.id); err == nil {
			n.refresh(p.id, f)
		}
	case appserver.TurnCompleted:
		n.ttft.clearPending(p.id)
		c.mu.Lock()
		if c.turnID == ev.Turn.ID {
			c.turnID = ""
		}
		c.mu.Unlock()
		failure, failureKind, modelServiceFailure := ev.Failure, ev.FailureKind, ev.ModelServiceFailure
		if stop, model := c.endTurnLoop(); stop != "" {
			// The loop's interrupt (assistant_codex_compaction_loop.go): the user's to act on, never a "Continue".
			if failure == "" {
				failure, failureKind, modelServiceFailure = stop, proto.TurnErrorUser, false
			}
			n.leaveLoopingFallback(p.id, model)
		}
		if failure != "" {
			n.logger.Printf("assistant codex %s: turn %s: %s", short(p.id), ev.Turn.Status, failure)
		}
		failureKinds := []string{failureKind}
		if modelServiceFailure {
			failureKinds = append(failureKinds, assistantModelServiceFailure)
			if failureKind != proto.TurnErrorUser && failureKind != proto.TurnErrorUsageLimit {
				failureKind = proto.TurnErrorTransient
			}
		}
		recovering := n.recoverAssistantResult(p, failure != "", failure, failureKinds...)
		if !recovering {
			n.clearAssistantRecovery(p, failure == "")
		}
		p.mu.Lock()
		if !recovering {
			p.busy = false
		}
		p.turnError = failure
		p.turnErrorKind = failureKind
		p.lastActive = time.Now()
		p.partial.Reset()
		manual := p.manual
		p.manual, p.compacting = false, false
		p.mu.Unlock()
		if manual { // the turn the compaction ran in ended before its item came: done, unless it failed
			n.compactionEnded(p.id, snippet(failure))
		}
		if f, err := n.file(p.id); err == nil {
			n.refresh(p.id, f)
		}
		n.emitDelta(p.id, "")
		if !recovering {
			n.notifySessions()
		}
		go c.syncTasks()
	case appserver.ThreadReverted:
		p.mu.Lock()
		p.partial.Reset()
		p.turnError, p.turnErrorKind = "", ""
		p.mu.Unlock()
		if f, err := n.file(p.id); err == nil {
			n.refresh(p.id, f)
		}
		n.emitDelta(p.id, "")
		n.notifyCodex()
	case appserver.UnretriedError:
		n.logger.Printf("assistant codex %s: %s", short(p.id), ev.Error.Message)
	case appserver.RequestResolved:
		// Answered some other way (the turn was interrupted): drop the phone's question.
		c.mu.Lock()
		cancel := c.asks[string(ev.RequestID)]
		c.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

func (c *codexConn) read(r io.Reader) {
	err := appserver.ReadMessages(r, codexLineMax, c.receive)
	stopUnreadAssistant(c.p.ownerOr(c.n).logger, c.p, "codex "+c.key, err)
}

func (c *codexConn) receive(m codexMessage) { c.rpcWire().Receive(m) }
