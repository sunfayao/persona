package node

import (
	"context"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/shellcmd"
	"github.com/sunfayao/persona/server/internal/textfmt"
)

// What a runtime that starts sessions needs beyond listing and driving them (the agy and OpenCode companions): its
// launchers, the form's choices passed on when a session starts, the phone's permission mode and effort (caps.modes,
// runtimes.go).

// runtimeLauncher is one of a runtime's entries for the new-session form (runtime.hello's launchers).
type runtimeLauncher struct {
	Name string `json:"name"`
	// Model is shown next to it (the provider's default model).
	Model string `json:"model,omitempty"`
	// Agent is what it starts: "claude" for Claude Code (the form's Claude choices, the apps' Claude controls), else
	// the runtime's own (or another the apps know).
	Agent string `json:"agent,omitempty"`
	// Models are its models for the form (launchers.models), the default marked.
	Models []proto.AgentModel `json:"models,omitempty"`
	// PromptOptional: a Codex launcher the runtime starts with no first message too (Paseo's, #8140): the form need not
	// ask for one (proto.Launcher.PromptOptional).
	PromptOptional bool `json:"prompt_optional,omitempty"`
	// AutoAccept: its agents' approval is a switch a start in bypassPermissions turns on (Paseo's Auto Accept), passed on
	// in launchers.list (proto.Launcher.AutoAccept, #8141).
	AutoAccept bool `json:"auto_accept,omitempty"`
	// DisplayName and PermissionModes: what the apps call it (Paseo's label for the provider) and, when its agent is
	// not Claude Code, the permission modes a start takes (proto.Launcher's, #8261).
	DisplayName     string                  `json:"display_name,omitempty"`
	PermissionModes proto.PermissionModeSet `json:"permission_modes,omitempty"`
	// AssistantFor: on the runtime's assistant launcher, the Agent launcher it runs the assistant for
	// (proto.Launcher.AssistantFor).
	AssistantFor string `json:"assistant_for,omitempty"`
}

// Bounds on a runtime's launchers. A display name is as long as a provider's may be (the manager's maxDisplayName).
const (
	maxRuntimeLaunchers    = 64
	maxLauncherModels      = 200
	maxLauncherText        = 200
	maxLauncherDisplayName = 64
)

// cleanLaunchers keeps the launchers a runtime may offer: names a launcher may have (shellcmd.NameRe, the shell's
// rule), each once, agents that are ids, text clipped, the lists bounded. Its permission modes are Persona's once read
// (proto.PermissionModeSet).
func cleanLaunchers(list []runtimeLauncher) []runtimeLauncher {
	clipTo := func(s string, n int) string {
		if r := []rune(strings.TrimSpace(s)); len(r) > n {
			return string(r[:n])
		}
		return strings.TrimSpace(s)
	}
	clip := func(s string) string { return clipTo(s, maxLauncherText) }
	out := make([]runtimeLauncher, 0, len(list))
	seen := map[string]bool{}
	for _, l := range list {
		if len(out) == maxRuntimeLaunchers {
			break
		}
		if !shellcmd.NameRe.MatchString(l.Name) || seen[l.Name] || l.Agent != "" && !runtimeIDRe.MatchString(l.Agent) {
			continue
		}
		seen[l.Name] = true
		l.Model = clip(l.Model)
		l.DisplayName = clipTo(l.DisplayName, maxLauncherDisplayName)
		if !shellcmd.NameRe.MatchString(l.AssistantFor) { // a launcher's name, or none
			l.AssistantFor = ""
		}
		models := make([]proto.AgentModel, 0, min(len(l.Models), maxLauncherModels))
		for _, m := range l.Models {
			if len(models) == maxLauncherModels {
				break
			}
			if m.ID = clip(m.ID); m.ID == "" {
				continue
			}
			m.Name, m.Description, m.Resolved, m.DefaultEffort = clip(m.Name), clip(m.Description), clip(m.Resolved), clip(m.DefaultEffort)
			efforts := m.Efforts[:0:0]
			for _, e := range m.Efforts {
				if validModes("", e) == nil {
					efforts = append(efforts, e)
				}
			}
			m.Efforts = efforts
			models = append(models, m)
		}
		l.Models = models
		out = append(out, l)
	}
	return out
}

// launcherList is every launcher h offers: hello's launcher, then its launchers.
func (h *runtimeConn) launcherList() []runtimeLauncher {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []runtimeLauncher
	if h.hello.Launcher != "" {
		entry := runtimeLauncher{Name: h.hello.Launcher, Model: h.hello.LauncherModel}
		for _, l := range h.hello.Launchers {
			if l.Name == h.hello.Launcher {
				if l.Model != "" && entry.Model == "" {
					entry.Model = l.Model
				}
				entry.Agent = l.Agent
				entry.Models = l.Models
				entry.AutoAccept = l.AutoAccept
				entry.DisplayName, entry.PermissionModes, entry.AssistantFor = l.DisplayName, l.PermissionModes, l.AssistantFor
				break
			}
		}
		out = append(out, entry)
	}
	for _, l := range h.hello.Launchers {
		if l.Name != h.hello.Launcher {
			out = append(out, l)
		}
	}
	return out
}

// launcherNamed is h's launcher of that name.
func (h *runtimeConn) launcherNamed(name string) (runtimeLauncher, bool) {
	for _, l := range h.launcherList() {
		if l.Name == name {
			return l, true
		}
	}
	return runtimeLauncher{}, false
}

// setLaunchers replaces h's launchers (runtime.launchers: a runtime whose providers changed, or came in after hello).
func (h *runtimeConn) setLaunchers(list []runtimeLauncher) {
	h.mu.Lock()
	h.hello.Launchers = list
	h.mu.Unlock()
}

// runtimeLauncherModels is launchers.models for a runtime's launcher: the models it said (none: an empty list, the
// form offers no choice); false for a launcher no runtime runs (runtimeOf).
func (n *Node) runtimeLauncherModels(name string) (proto.LauncherModels, bool) {
	return launcherModelsOf(n.runtimeOf(name), name)
}

// agyRuntimeModels is the Antigravity companion's advertised catalog for settings.
func (n *Node) agyRuntimeModels() (proto.LauncherModels, bool) {
	return launcherModelsOf(n.runtimes.launcherRuntime(agyAgent), agyAgent)
}

// launcherModelsOf is runtime h's advertisement for its launcher name; false without a runtime.
func launcherModelsOf(h *runtimeConn, name string) (proto.LauncherModels, bool) {
	if h == nil {
		return proto.LauncherModels{}, false
	}
	l, _ := h.launcherNamed(name)
	agent := l.Agent
	if agent == "" {
		agent = h.agent()
	}
	models := l.Models
	if models == nil {
		models = []proto.AgentModel{}
	}
	return proto.LauncherModels{Agent: agent, Models: models}, true
}

// runtimeStartParams are what session.start passes to a runtime.
type runtimeStartParams struct {
	Cwd, Prompt, Launcher         string
	Model, PermissionMode, Effort string
	ClientMsgID                   string
	Images                        []proto.Image
	WorkspaceID                   string // a space of the runtime's to start in (one of its sessions' space_id)
	DesktopProjectID              string // the exact native desktop project selected in the project board
	Resume                        string // the session to continue (Claude's desktop app)
	// SessionID: the id the hub named for a new session (session.start's session_id, #8130): the manager finds a
	// start whose answer it lost by it. A runtime that can give its session that id does; one that cannot picks its
	// own, and the manager looks among the sessions Persona started instead (started_by_persona).
	SessionID string
	// AgentType: the kind of agent Launcher starts (claude, codex, agy…): an agy start may run in a herdr pane instead
	// (usesAgyPane).
	AgentType       string
	RequireLauncher bool // the start carries context to a fresh session on its old launcher
}

// liveRuntimeModels asks runtime h, the one offering launcher name to the start in question (runtimeOfIn), for its
// current native catalog when it opts in. Older runtimes keep their advertised list.
func (n *Node) liveRuntimeModels(ctx context.Context, h *runtimeConn, name string, refresh bool, fallback proto.LauncherModels) (proto.LauncherModels, error) {
	if h == nil {
		return fallback, nil
	}
	h.mu.Lock()
	supported := h.hello.Caps.Models
	h.mu.Unlock()
	if !supported {
		return fallback, nil
	}
	ctx, cancel := context.WithTimeout(ctx, runtimeCallTimeout)
	defer cancel()
	var out proto.LauncherModels
	if err := h.peer.Call(ctx, "launchers.models", map[string]any{"launcher": name, "refresh": refresh}, &out); err != nil {
		return proto.LauncherModels{}, err
	}
	// Apply the same bounds as runtime.launchers before exposing a native response.
	clean := cleanLaunchers([]runtimeLauncher{{Name: name, Agent: fallback.Agent, Models: out.Models}})
	out.Agent, out.Models = fallback.Agent, clean[0].Models
	out.SupportsStartModel = false // only the node's native omp start can advertise this.
	if out.Models == nil {
		out.Models = []proto.AgentModel{}
	}
	return out, nil
}

// runtimeLauncherCheck opts into the authenticated availability probe of runtime h, the one offering launcher name to
// the start in question (runtimeOfIn). A refused probe never falls back to the login shell, whose credentials may be
// different. Older runtimes keep the existing shell check.
func (n *Node) runtimeLauncherCheck(ctx context.Context, h *runtimeConn, name, cwd, prompt, model string) (proto.LauncherCheck, bool) {
	if h == nil {
		return proto.LauncherCheck{}, false
	}
	h.mu.Lock()
	supported := h.hello.Caps.Check
	h.mu.Unlock()
	if !supported {
		return proto.LauncherCheck{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, launcherCheckTimeout)
	defer cancel()
	start := time.Now()
	var result proto.LauncherCheck
	params := map[string]any{"launcher": name, "cwd": cwd, "prompt": prompt}
	if model != "" {
		params["model"] = model
	}
	if err := h.peer.Call(ctx, "launchers.check", params, &result); err != nil {
		result = proto.LauncherCheck{Error: err.Error()}
	}
	// The runtime's CLI may print colour and shell integration like any other: the record and the apps read it clean.
	if result.Error != "" {
		result.Error = textfmt.LauncherErrorSummary(result.Error, 400)
	}
	result.Answer = textfmt.StripTerminalEscapes(result.Answer)
	launcher, _ := h.launcherNamed(name)
	result.Agent = launcher.Agent
	if result.Agent == "" {
		result.Agent = h.agent()
	}
	result.Millis = int(time.Since(start).Milliseconds())
	return result, true
}
