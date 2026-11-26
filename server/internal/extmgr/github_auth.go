package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sunfayao/persona/server/internal/connector"
	"github.com/sunfayao/persona/server/internal/connector/github"
	"github.com/sunfayao/persona/server/internal/connector/proc"
	"github.com/sunfayao/persona/server/internal/connector/serve"
	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// Where GitHub's token comes from, as the user picks it in Settings › Connectors › GitHub: a
// computer's gh login or the device flow. The choice is kept in settings ("github_auth"); hub.json's
// github.auth.from_machine is only the default until the user chooses.

const githubAuthKey = "github_auth"

// githubAuth is the user's choice.
type githubAuth struct {
	Source  string `json:"source"`            // github.SourceMachine | github.SourceDevice
	Machine string `json:"machine,omitempty"` // the machine's id, for SourceMachine
}

// githubConnector is the hub's own GitHub connector while it runs (not while an extension took its place).
func (h *Manager) githubConnector() *github.Connector {
	c, _ := h.connectors.Get("github").(*github.Connector)
	return c
}

// ownGitHub is the hub's own GitHub connector, running or set aside for an extension's (connectors_migrate.go): it
// still checks each computer's gh login for the page.
func (h *Manager) ownGitHub() *github.Connector {
	if c := h.githubConnector(); c != nil {
		return c
	}
	if v, ok := h.migrated.Load("github"); ok {
		c, _ := v.(*github.Connector)
		return c
	}
	return nil
}

// githubExtension is an extension's GitHub connector when one took the hub's place (#2830).
func (h *Manager) githubExtension() *proc.Conn {
	c, _ := h.connectors.Get("github").(*proc.Conn)
	return c
}

// githubChoice is the user's choice, kept from before; hub.json's github.auth.from_machine until they chose.
func (h *Manager) githubChoice(ctx context.Context) (githubAuth, bool) {
	var a githubAuth
	if h.store != nil {
		if ok, err := h.store.Setting(ctx, githubAuthKey, &a); err == nil && ok {
			return a, true
		}
	}
	if h.cfg.GitHub != nil && h.cfg.GitHub.Configured() {
		return githubAuth{Source: github.SourceMachine, Machine: h.cfg.GitHub.Auth.FromMachine}, false
	}
	return githubAuth{Source: github.SourceDevice}, false
}

// restoreGitHubSource applies the user's choice, kept from before, at start.
func (h *Manager) restoreGitHubSource(ctx context.Context) {
	c := h.githubConnector()
	if c == nil {
		return
	}
	if a, chosen := h.githubChoice(ctx); chosen {
		h.applyGitHubSource(ctx, c, a)
	}
}

func (h *Manager) applyGitHubSource(ctx context.Context, c *github.Connector, a githubAuth) {
	if a.Source == github.SourceMachine && a.Machine != "" {
		c.UseMachine(ctx, a.Machine, h.machineName(a.Machine), h.ghToken(a.Machine))
		return
	}
	c.UseDevice()
}

// applyGitHubExtensionSource gives an extension's GitHub connector the user's choice (or hub.json's default); the
// connector asks for a computer's token itself (credentials.gh_token).
func (h *Manager) applyGitHubExtensionSource(ctx context.Context, c *proc.Conn, a githubAuth) (serve.AuthView, error) {
	src := serve.AuthSource{Source: a.Source}
	if a.Source == github.SourceMachine && a.Machine != "" {
		src.Machine, src.MachineName = a.Machine, h.machineName(a.Machine)
	} else {
		src.Source = github.SourceDevice
	}
	return c.SetAuth(ctx, src)
}

// githubExtensionTaken sets an extension's GitHub connector up as the hub's own was: the user's sign-in choice, and
// the inbox settings the hub's own had (hub.json's github.inbox) when the user never changed them.
func (h *Manager) githubExtensionTaken(ctx context.Context, c *proc.Conn, cs connectorSettings) {
	a, _ := h.githubChoice(ctx)
	if _, err := h.applyGitHubExtensionSource(ctx, c, a); err != nil {
		h.logf("github: the extension's sign-in source: %v", err)
	}
	if _, chosen := cs.Options["github"]; chosen || !c.Manifest().Has(connector.CapOptions) {
		return
	}
	if own := h.ownGitHub(); own != nil {
		if err := c.SetOptions(own.Options()); err != nil {
			h.logf("github: the extension's inbox settings: %v", err)
		}
	}
}

var errNoGitHub = errors.New("no GitHub connector")

// ghTokenFor answers an extension's credentials.gh_token (one granted gh_login, the GitHub connector that took the
// hub's place): the token of machine's gh login, as the hub's own connector reads it. The token goes to that program
// only, never to a log; why there is none goes as the connector protocol's codes.
func (h *Manager) ghTokenFor(ctx context.Context, machine, host string) (string, error) {
	if host == "" {
		host = "github.com"
	}
	tok, err := h.ghToken(machine)(ctx, host)
	if err == nil {
		return tok, nil
	}
	for _, c := range []struct {
		err  error
		code string
	}{{github.ErrNoMachine, serve.CodeNoMachine}, {github.ErrOffline, serve.CodeOffline}, {github.ErrNodeTooOld, serve.CodeNodeTooOld},
		{github.ErrNoGh, serve.CodeNoGh}, {github.ErrNotSignedIn, serve.CodeNotSignedIn}} {
		if errors.Is(err, c.err) {
			return "", &rpc.Error{Code: c.code, Message: err.Error()}
		}
	}
	return "", err
}

// githubMigratedKey records which install of the extension got manager.json's github settings (githubInstall). An
// uninstall deletes an extension's settings, so a reinstall, a new install, gets them again; a key the user cleared in
// the same install stays cleared. A hub before that wrote true, whichever install it was: done while the extension
// holds one of the keys the move would set.
const githubMigratedKey = "github.migrated_to_extension"

// githubInstall names one install of an extension: its id and when it was installed, in Unix seconds as the database
// keeps it. A built-in extension cannot be uninstalled: its id is enough.
func githubInstall(p *extension.Extension) string {
	if p.Record.Source == extension.SourceBuiltin || p.Record.InstalledAt.IsZero() {
		return p.Record.ID
	}
	return fmt.Sprintf("%s@%d", p.Record.ID, p.Record.InstalledAt.Unix())
}

// migrateGitHubConfig moves manager.json's github.proxy and github.auth.host into the settings of the extension whose
// connector took the hub's place, once per install of it (githubMigratedKey), for keys the user has not set there: its
// GitHub traffic goes the way the hub's own did. The extension restarts with them (a config change). A failure is
// logged and tried at the next start.
func (h *Manager) migrateGitHubConfig(ctx context.Context, extensionID string) {
	gh := h.cfg.GitHub
	if gh == nil || (gh.Proxy == "" && gh.Auth.Host == "") || h.store == nil || h.extensions == nil || h.extensions.reg == nil {
		return
	}
	reg := h.extensions.reg
	p := reg.Get(extensionID)
	if p == nil {
		return
	}
	cfg := map[string]any{}
	_ = json.Unmarshal(p.Record.Config, &cfg)
	props := map[string]json.RawMessage{}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if json.Unmarshal(p.Full.Configuration, &schema) == nil {
		props = schema.Properties
	}
	moves := map[string]string{}
	for key, v := range map[string]string{"proxy": gh.Proxy, "host": gh.Auth.Host} {
		if _, has := props[key]; has && v != "" {
			moves[key] = v
		}
	}
	held := func(key string) bool { cur, _ := cfg[key].(string); return cur != "" }
	var done any
	if ok, err := h.store.Setting(ctx, githubMigratedKey, &done); err == nil && ok {
		switch d := done.(type) {
		case string:
			if d == githubInstall(p) {
				return
			}
		case bool: // written before installs were told apart
			for key := range moves {
				if d && held(key) {
					return
				}
			}
		}
	}
	moved := false
	for key, v := range moves {
		if held(key) {
			continue
		}
		cfg[key], moved = v, true
	}
	if moved {
		raw, _ := json.Marshal(cfg)
		if err := reg.SetConfig(ctx, extensionID, raw, "hub"); err != nil {
			h.logf("github: moving manager.json's github.proxy / auth.host into the extension's settings: %v", err)
			return
		}
		h.logf("github: manager.json's github.proxy / auth.host moved into 设置 › 扩展 › %s", p.Full.Name)
	}
	if cur := reg.Get(extensionID); cur != nil {
		p = cur
	}
	if err := h.store.PutSetting(ctx, githubMigratedKey, githubInstall(p)); err != nil {
		h.logf("github: marking the move done: %v", err)
	}
}
