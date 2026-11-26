package assistant

import (
	"context"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
)

// Requested names use the background naming model and write to the same ledger. The owner
// confirms the tool request, so automatic naming preferences and placeholder checks do not apply.

// agentNameRefusal carries the tool response code, localized error and whether the caller can retry.
type agentNameRefusal struct {
	code  string
	retry bool
	err   error
}

func (e *agentNameRefusal) Error() string { return e.err.Error() }
func (e *agentNameRefusal) Unwrap() error { return e.err }

// agentNameRefusals are the naming call's static reasons (agentNameFromModel) as the tool reports them. A truncated
// answer is an invalid name to the user; a canceled call can only be the manager stopping. A call held back because
// the Sorting model's launcher answers slower than naming waits was never sent, and asking again soon is held back
// the same way, so it is not retryable and says what to change.
var agentNameRefusals = map[string]agentNameRefusal{
	"model_not_configured":   {code: "model_not_configured", err: i18n.Err("assistant-agent_naming.model_not_configured")},
	agentNameHeldBack:        {code: "launcher_too_slow", err: i18n.Err("assistant-agent_naming.launcher_too_slow")},
	"no_dialogue":            {code: "no_dialogue", err: i18n.Err("assistant-agent_naming.no_dialogue")},
	"invalid_name":           {code: "invalid_name", retry: true, err: i18n.Err("assistant-agent_naming.invalid_name")},
	"model_output_truncated": {code: "invalid_name", retry: true, err: i18n.Err("assistant-agent_naming.invalid_name")},
	"model_call_failed":      {code: "model_failed", retry: true, err: i18n.Err("assistant-agent_naming.model_failed")},
	"dialogue_unavailable":   {code: "dialogue_unavailable", retry: true, err: i18n.Err("assistant-agent_naming.dialogue_unavailable")},
	"naming_timeout":         {code: "timeout", retry: true, err: i18n.Err("assistant-agent_naming.timeout")},
	"naming_canceled":        {code: "canceled", retry: true, err: i18n.Err("assistant-agent_naming.timeout")},
}

var (
	errAgentNameNoMachine = &agentNameRefusal{err: i18n.Err("devices-api.unknown_machine")}
	errAgentNameNoSession = &agentNameRefusal{err: i18n.Err("hub-deleted_sessions.unknown")}
	errAgentNameAssistant = &agentNameRefusal{code: "assistant_session", err: i18n.Err("assistant-agent_naming.assistant_session")}
	errAgentNameLedger    = &agentNameRefusal{code: "names_unavailable", retry: true, err: i18n.Err("assistant-profile.agent_names_unavailable")}
	errAgentNameSave      = &agentNameRefusal{code: "save_failed", retry: true, err: i18n.Err("assistant-agent_naming.save_failed")}
	errAgentNameRenamed   = &agentNameRefusal{code: "renamed_meanwhile", err: i18n.Err("assistant-agent_naming.renamed_meanwhile")}
)

// agentNameResult is a requested name as given: the title the lists show now, and whether it differs from the one
// they showed before.
type agentNameResult struct {
	Title   string
	Changed bool
}

// requestAgentName names machine's session sid with the model now, as the user asked. Requests for one session that
// arrive while its call is out share that call and its answer: one model call, one ledger write, one list update.
func (s *Service) requestAgentName(ctx context.Context, machine, sid string) (agentNameResult, error) {
	if m, ok := s.fleetMachine(machine); ok {
		machine = m.ID // an id another one absorbed names that one
	} else if s.fleetOn() {
		return agentNameResult{}, errAgentNameNoMachine
	}
	if hook := s.agentNames.beforeRequest; hook != nil {
		hook()
	}
	v, err, _ := s.agentNames.requests.Do(agentNameKey(machine, sid), func() (any, error) {
		// The request that started the call may go away; the ones that joined it still get its answer.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), agentNamingTimeout+15*time.Second)
		defer cancel()
		return s.nameAgentNow(ctx, machine, sid)
	})
	if err != nil {
		return agentNameResult{}, err
	}
	return v.(agentNameResult), nil
}

func (s *Service) nameAgentNow(ctx context.Context, machine, sid string) (agentNameResult, error) {
	// The raw node row, as the background pass reads it: the name is kept against the native title.
	c, found := s.rawAgentForNaming(ctx, machine, sid)
	if !found {
		return agentNameResult{}, errAgentNameNoSession
	}
	if c.Assistant || s.isMain(ctx, sid) {
		return agentNameResult{}, errAgentNameAssistant
	}
	key := agentNameKey(machine, sid)
	b := &s.agentNames
	b.mu.Lock()
	loaded := s.agentNamesLocked(ctx)
	before := b.records[key]
	b.mu.Unlock()
	if !loaded {
		return agentNameResult{}, errAgentNameLedger // a write over an unread ledger would drop every other name
	}
	shown := s.agentDisplayName(machine, c)
	if shown == "" {
		shown = c.Title
	}
	callCtx, cancel := context.WithTimeout(ctx, agentNamingTimeout)
	title, reason := s.agentNameFromModel(callCtx, machine, sid)
	cancel()
	if reason != "" {
		s.logf("agent naming: requested session=%q/%q reason=%s", machine, sid, reason)
		refusal, known := agentNameRefusals[reason]
		if !known {
			refusal = agentNameRefusals["model_call_failed"]
		}
		return agentNameResult{}, &refusal
	}
	// The name stands over the native title as it is now: one that changed during the call is the one it covers.
	current, found := s.rawAgentForNaming(ctx, machine, sid)
	if !found {
		return agentNameResult{}, errAgentNameNoSession
	}
	if current.Assistant {
		return agentNameResult{}, errAgentNameAssistant
	}
	// A name written by hand while the model was asked is the later wish: in the app (the ledger's manual record
	// is new or changed) or in the Agent itself (a custom title that was not there when the call started).
	nativeRename := (current.TitleManual || current.TitleSource.String() == "manual") && !sameAgentRawTitle(c.Title, current.Title)
	b.mu.Lock()
	r := b.records[key]
	if nativeRename || r.Manual && (!before.Manual || r.Title != before.Title) {
		b.mu.Unlock()
		s.logf("agent naming: requested session=%q/%q reason=renamed_meanwhile", machine, sid)
		return agentNameResult{}, errAgentNameRenamed
	}
	r.Machine, r.Session = machine, sid
	r.Title, r.RawTitle = title, current.Title
	// An automatic name from now on, whatever it was. The background's change count is its own: this neither spends
	// nor resets it. Its failures and their cooldown are over, since the model just named this session.
	r.Manual, r.Requested = false, true
	r.NamedAt, r.NamedOutput = time.Now(), current.OutputTokens
	r.Failures, r.LastFailure, r.NextAttempt = 0, "", time.Time{}
	err := s.putAgentNameLocked(ctx, key, r)
	b.mu.Unlock()
	if err != nil {
		s.logf("agent naming: requested session=%q/%q reason=result_write_failed", machine, sid)
		return agentNameResult{}, errAgentNameSave
	}
	s.publishSessions(machine) // the apps' lists follow before the request is answered
	s.logf("agent naming: requested session=%q/%q result=named", machine, sid)
	return agentNameResult{Title: title, Changed: title != shown}, nil
}
