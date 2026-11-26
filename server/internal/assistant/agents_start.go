package assistant

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/ai"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/lazyre"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
)

// What a new or resumed Agent runs on and what its tab is called: the
// manager's. The hub asks before it starts one the app asked for (agents.start_params), before a message resumes one
// nothing says the launcher of (agents.resume_params), and for a tab's name (agents.tab_name: an Agent started in the
// background, the Feishu card's); the assistant's own starts (agents_start, a task's agents) come here directly.

// The Agents business's names on the bus (assistantwire/agents.go).
type (
	WorkReport        = assistantwire.WorkReport
	SessionWork       = assistantwire.SessionWork
	AgentStartParams  = assistantwire.AgentStartParams
	AgentResumeParams = assistantwire.AgentResumeParams
	SessionSeen       = assistantwire.SessionSeen
)

const (
	MethodAgentStartParams  = assistantwire.MethodAgentStartParams
	MethodAgentResumeParams = assistantwire.MethodAgentResumeParams
	MethodAgentTabName      = assistantwire.MethodAgentTabName
	EventSessionSeen        = assistantwire.EventSessionSeen
	CodeGone                = assistantwire.CodeGone
)

// AgentStartParams is agents.start_params: the start's params with what the Agent runs on (Model Settings › Agents, over it
// the launcher, model and effort the app picked for this start) and, when asked, its tab's name. A resumed session
// keeps what it runs on (the node knows). A model configuration that cannot run is the app's to hear (bad_request).
func (s *Service) AgentStartParams(ctx context.Context, p AgentStartParams) (map[string]any, error) {
	params := p.Params
	if params == nil {
		params = map[string]any{}
	}
	delete(params, "external")
	if launcher, _ := params["launcher"].(string); proto.IsExternalLauncher(launcher) {
		return nil, badRequest(i18n.Err("external-adapter.purpose_unsupported"), s.appLang(ctx, p.Lang))
	}
	if resume, _ := params["resume_session_id"].(string); resume != "" {
		if err := checkStartSpeed(params, startAgent{}, resume); err != nil {
			return nil, badRequest(err, s.appLang(ctx, p.Lang))
		}
		clearStandardSpeed(params)
	} else {
		own, err := s.resolveStart(ctx, params, false)
		if err != nil {
			return nil, badRequest(err, s.appLang(ctx, p.Lang)) // a typed model that is no id among them
		}
		if params["launch"] != "engine" {
			return nil, badRequest(fmt.Errorf("purpose %q requires engine launch for session.start", "agent.new"), s.appLang(ctx, p.Lang))
		}
		// Held to what the computer has (#2640): a launcher picked that it lacks is refused, a default it lacks gives
		// way to one it has.
		if m, ok := s.device(ctx, p.MachineID); ok {
			note, agent, err := s.fitNewLauncher(ctx, m, own, params)
			if err != nil {
				return nil, badRequest(err, s.appLang(ctx, p.Lang))
			}
			if err := checkStartSpeed(params, agent, ""); err != nil {
				return nil, badRequest(err, s.appLang(ctx, p.Lang))
			}
			if mode, _ := params["permission_mode"].(string); mode != "" && own.permission == "" && !agent.supportsPermissionMode(mode) {
				delete(params, "permission_mode")
			}
			leaveLauncherNote(ctx, note) // the app says so (agents_bus.go launcher_note, #4661)
			// AGY returns a session only after startup, including for a launcher resolved from Model settings.
			if declared, _ := params["agent_type"].(string); declared == "" && agent.agent == "agy" {
				params["agent_type"] = agent.agent
			}
		}
		if name, _ := params["launcher"].(string); name != "" {
			if err := s.launcherScopeErr(ctx, p.MachineID, name); err != nil {
				return nil, err // a provider out of this computer's scope (models_launchers_scope.go)
			}
		}
	}
	delete(params, "launcher_default_effort")
	clearStandardSpeed(params)
	s.setPlanThen(ctx, p.MachineID, params)
	if name, _ := params["tab_name"].(string); strings.TrimSpace(name) == "" && p.NameFrom != "" {
		if name := s.tabName(ctx, p.NameFrom); name != "" {
			params["tab_name"] = name
		}
	}
	return params, nil
}

// AgentResumeParams is agents.resume_params: a message that resumes an Agent started outside Persona (nothing on its
// machine says its launcher) goes on Model Settings › Agents' launcher and model rather than plain claude, which may not be
// signed in there (node/sendsafe.go resumeLauncher). When that cannot run as a launcher the message goes as it was.
func (s *Service) AgentResumeParams(ctx context.Context, p AgentResumeParams) (map[string]any, error) {
	params := p.Params
	if params == nil {
		params = map[string]any{}
	}
	delete(params, "external")
	if launcher, _ := params["launcher"].(string); proto.IsExternalLauncher(launcher) {
		return nil, badRequest(i18n.Err("external-adapter.purpose_unsupported"), s.requestCtxLang(ctx))
	}
	if err := checkStartSpeed(params, startAgent{}, p.SessionID); err != nil {
		return nil, badRequest(err, s.requestCtxLang(ctx))
	}
	clearStandardSpeed(params)
	q := map[string]any{}
	if err := s.applyModel(ctx, "agent.new", q); err != nil {
		s.logf("resume %s: 新建 Agent's launcher not applied: %v", p.SessionID, err)
		return params, nil
	}
	// One the computer lacks gives way to one it has (#2640; a default is never refused).
	if m, ok := s.device(ctx, p.MachineID); ok {
		if _, _, err := s.fitLauncher(ctx, m, startPick{}, q); err != nil {
			s.logf("resume %s: the New Agent launcher not applied: %v", p.SessionID, err)
			return params, nil
		}
	}
	if name, _ := q["launcher"].(string); s.launcherScopeErr(ctx, p.MachineID, name) != nil {
		return params, nil // out of this computer's scope: the message goes as it was
	}
	for _, k := range []string{"launcher", "model", "effort", "model_fingerprint"} {
		if v, ok := q[k]; ok {
			params[k] = v
		}
	}
	return params, nil
}

// tabName is a short name for the herdr tab of a new agent, from its first message (purpose agent.title, a few
// seconds at most). "" when there is no message or the model fails: the node names it agent-<n>.
func (s *Service) tabName(ctx context.Context, prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if r := []rune(prompt); len(r) > 1500 {
		prompt = string(r[:1500])
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// Reasoning models think first: 20 tokens came back empty.
	text, err := s.purposeModel("agent.title").Complete(cctx, prompts.RenderTabName(), prompt, 200)
	if err != nil {
		if !ai.IsPaused(err) {
			s.logf("tab name: %v", err)
		}
		return ""
	}
	name := tabSlug(text)
	if name == "" {
		s.logf("tab name: nothing usable in %q", clip(oneLine(text), 80))
	}
	return name
}

// nameStartedAgent records the dispatched Agent's initial Persona title. Use the actual tab label when the host
// made the requested name unique. A later CLI or Persona rename can replace it.
func (s *Service) nameStartedAgent(ctx context.Context, machine string, c proto.Session, requested string) string {
	if requested == "" || c.SessionID == "" {
		return requested
	}
	title := cmp.Or(c.HerdrTabName(), requested)
	if err := s.putInitialAgentTitle(ctx, machine, c.SessionID, title); err != nil {
		s.logf("dispatch: save session title for %q/%q: %v", machine, c.SessionID, err)
	} else {
		s.publishSessions(machine)
	}
	return title
}

// tabSlug keeps what a tab name may be: lowercase ASCII letters and digits, single hyphens, at most 24 characters.
func tabSlug(s string) string {
	if i := strings.IndexByte(strings.TrimSpace(s), '\n'); i > 0 {
		s = strings.TrimSpace(s)[:i]
	}
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ' || r == '/':
			dash = true
		}
		if b.Len() >= 24 {
			break
		}
	}
	return strings.Trim(b.String()[:min(b.Len(), 24)], "-")
}

// defaultTabName: a name nobody chose — the old fixed "persona", herdr's numbers, the agent-<n> fallback.
var defaultTabName = lazyre.New(`^(persona(-\d+)?|\d+|agent-\d+)?$`)
