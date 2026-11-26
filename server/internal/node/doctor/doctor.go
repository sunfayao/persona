package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantskills"
	"github.com/nwverse/persona/server/internal/claudeplugin"
	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/nodeclient"
	"github.com/nwverse/persona/server/internal/shellcmd"
	"github.com/nwverse/persona/server/internal/textfmt"
)

// `personad node doctor` looks at what a node on this machine needs for the sessions Persona starts to reach the
// phone: the sockets hooks and MCP dial, the files claude loads (compared with what this binary would write now, the
// same bytes the node writes at startup) and the programs those files run. It only reads. --fix writes Persona's
// own generated files again (writeGeneratedConfig, the startup path) and, on a Mac and outside the node, the resident
// node's LaunchAgent's ProcessType (launchagent.go); nothing else: the user's own Claude Code settings are only ever
// looked at. The path-access check (preflight.go) reads what a claude start reads, in a child it kills when a read
// hangs. Background: a redeploy once deleted node.sock and every hook and MCP call fell back to the terminal without a
// word (docs/lessons/server.md).

// Config connects checks to the node's generated files and installed extensions.
type Config struct {
	PluginMinVersion     [3]int
	GeneratedFiles       func(bin, dir string) ([]GeneratedFile, error)
	WriteGeneratedConfig func(dir, bin string) error
	CheckAgyHerdr        func(context.Context, string, string, i18n.Lang) (DoctorCheck, bool)
	CheckAgentSandbox    func(string, i18n.Lang) DoctorCheck
	LiveRepliesDir       string
	NodePluginsDir       string
	OwnsPlugin           func(root, id string) bool
	ValidDigest          func(string) bool
	LookPath             func(string) (string, error)
	After                func(time.Duration) <-chan time.Time
}

type Doctor struct {
	cfg  Config
	home string
}

func New(cfg Config) *Doctor {
	home, _ := os.UserHomeDir()
	if cfg.After == nil {
		cfg.After = time.After
	}
	return &Doctor{cfg: cfg, home: home}
}

// Check statuses.
const (
	DoctorOK   = "ok"
	DoctorWarn = "warn"
	DoctorFail = "fail"
)

// DoctorCheck is one line of the report; the names are stable for scripts.
type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	// State is current / stale / missing for a generated file or directory, "" for the other checks.
	State  string   `json:"state"`
	Detail string   `json:"detail"`
	Hint   string   `json:"hint"`
	Items  []string `json:"items"` // what differs, which commands, ...; never null
}

// DoctorReport is what `node doctor` prints (--json: as it is).
type DoctorReport struct {
	OK     bool          `json:"ok"` // no check failed
	Dir    string        `json:"dir"`
	Checks []DoctorCheck `json:"checks"`
	// Fixed names what --fix put right: the generated files it wrote again; on a Mac launchd-process-type when it
	// wrote ProcessType into the LaunchAgent and launchd-reload when it loaded the agent again.
	Fixed []string `json:"fixed"`
}

// DoctorOptions says where to look; DefaultDoctorOptions fills them in for this machine (tests give their own).
type DoctorOptions struct {
	// Lang words this run's human-readable fields. Empty keeps the legacy zh-Hans report; it never comes from the
	// resident node's environment.
	Lang i18n.Lang
	Dir  string // the node's directory (~/.persona)
	Bin  string // the program the generated files should run: this binary
	// Sockets: node.sock, runtime.sock and herdr's API socket.
	NodeSock, RuntimeSock, HerdrSock string
	// ClaudeConfigDir is where the user's own Claude Code settings are (read, never written).
	ClaudeConfigDir string
	// AgyConfigDir is agy's config directory, where herdr installs its Antigravity hook (agyHookDir); "" leaves the
	// agy-herdr line out.
	AgyConfigDir string
	// ClaudeVersion asks claude's version ("" when unknown); ClaudeProbe, when set, is asked instead and also says
	// whether the shell found no claude at all (claudeProbe), so a version that could not be read this time is not
	// reported as Claude Code missing.
	ClaudeVersion func(context.Context) string
	ClaudeProbe   func(context.Context) (version string, missing bool)
	// LaunchAgent is the resident node's launchd agent on a Mac (NodeLaunchAgentPath; pickNodeLaunchdLabel says which
	// when a Mac keeps its previously installed label); "" elsewhere, and the launchd-process-type check is
	// left out.
	LaunchAgent string
	// LaunchdTarget is that agent as launchctl names it (gui/<uid>/com.persona.node, or the older label).
	LaunchdTarget string
	// Launchctl runs launchctl and returns its output; nil: launchd is not asked (the check reads the plist only).
	Launchctl func(ctx context.Context, args ...string) (string, error)
	// InsideNode: the doctor runs in the resident node (node.doctor) or under it. --fix then leaves the LaunchAgent
	// alone: loading it again stops the node, and with it the doctor, half way.
	InsideNode bool
	// RestartWait caps the wait, after --fix loaded the agent again, for the new node to answer (0: nodeRestartWait).
	RestartWait time.Duration
	// Preflight is the path-access check (preflight.go); nil leaves it out. It receives this run's language so a
	// terminal --json run can keep the legacy bytes even when the terminal is not Chinese.
	Preflight func(context.Context, i18n.Lang) DoctorCheck
	// Fix writes the generated files again when they are stale or missing, and (outside the node) writes
	// ProcessType=Interactive into the LaunchAgent and loads it again.
	Fix bool
}

// WithLaunchAgent points the check at the agent pickNodeLaunchdLabel picks in domain, asking launchctl.
func (o *DoctorOptions) WithLaunchAgent(ctx context.Context, home, domain, self string,
	launchctl func(context.Context, ...string) (string, error)) {
	label := pickNodeLaunchdLabel(ctx, home, domain, self, launchctl)
	o.LaunchAgent, o.LaunchdTarget, o.Launchctl = NodeLaunchAgentPath(home, label), domain+"/"+label, launchctl
}

// GeneratedFile is a file the node writes for the sessions it starts: Group is the unit the doctor reports on, Rel
// the path under the node's directory.
type GeneratedFile struct {
	Group   string
	Rel     string
	Content []byte
}

const AgentPluginGroup = "agent-plugin"

// Run runs the checks. With opts.Fix it first puts right what it may (the generated files; outside the node, the
// LaunchAgent's ProcessType), then checks again.
func (d *Doctor) Run(ctx context.Context, opts DoctorOptions) DoctorReport {
	if opts.Lang == "" {
		opts.Lang = i18n.Default
	} else {
		opts.Lang = i18n.Resolve(string(opts.Lang))
	}
	if pre := opts.Preflight; pre != nil { // read once: nothing a fix does changes what it reads
		var once sync.Once
		var c DoctorCheck
		opts.Preflight = func(ctx context.Context, lang i18n.Lang) DoctorCheck {
			once.Do(func() { c = pre(ctx, lang) })
			return c
		}
	}
	rep := d.checks(ctx, opts)
	if !opts.Fix {
		return rep
	}
	var fixed []string
	var failed []DoctorCheck
	var stale []string
	for _, c := range rep.Checks {
		if c.State == "stale" || c.State == "missing" {
			stale = append(stale, c.Name)
		}
	}
	if len(stale) > 0 {
		if err := d.cfg.WriteGeneratedConfig(opts.Dir, opts.Bin); err != nil {
			failed = append(failed, DoctorCheck{Name: "fix", Status: DoctorFail,
				Detail: i18n.T(opts.Lang, "node-doctor.fix_regenerate_failed", i18n.Args{"error": err.Error()}),
				Hint:   i18n.T(opts.Lang, "node-doctor.fix_regenerate_hint", i18n.Args{"dir": opts.Dir}), Items: []string{}})
		} else {
			fixed = append(fixed, stale...)
		}
	}
	var notBack []string
	logPath := ""
	if opts.LaunchAgent != "" && !opts.InsideNode {
		a := inspectLaunchAgent(ctx, opts)
		wrote, reloaded, err := d.fixLaunchAgent(ctx, opts, a)
		if err != nil {
			failed = append(failed, DoctorCheck{Name: "fix", Status: DoctorFail, Detail: err.Error(),
				Hint: i18n.T(opts.Lang, "node-doctor.fix_reinstall_hint"), Items: []string{opts.LaunchAgent}})
		}
		if wrote {
			fixed = append(fixed, launchdCheck)
		}
		if reloaded {
			fixed = append(fixed, launchdReloaded)
			// bootstrap returns before the new node listens: checked at once, its sockets would look dead.
			notBack, logPath = d.waitForRestart(ctx, opts, rep), a.logPath
		}
	}
	if len(fixed) == 0 && len(failed) == 0 {
		return rep
	}
	rep = d.checks(ctx, opts)
	notBackYet(&rep, notBack, opts.RestartWait, logPath, opts.Lang)
	rep.Fixed = append(rep.Fixed, fixed...)
	if len(failed) > 0 {
		rep.Checks = append(rep.Checks, failed...)
		rep.OK = false
	}
	return rep
}

func (d *Doctor) checks(ctx context.Context, opts DoctorOptions) DoctorReport {
	rep := DoctorReport{Dir: opts.Dir, Fixed: []string{}}
	add := func(c DoctorCheck) {
		if c.Items == nil {
			c.Items = []string{}
		}
		rep.Checks = append(rep.Checks, c)
	}
	var pre chan DoctorCheck
	if opts.Preflight != nil { // reads in a child of its own, alongside the other checks
		pre = make(chan DoctorCheck, 1)
		go func() { pre <- opts.Preflight(ctx, opts.Lang) }()
	}
	add(checkNodeSocketIn(ctx, opts.NodeSock, opts.Lang))
	add(checkRuntimeSocketIn(ctx, opts.RuntimeSock, opts.Lang))
	for _, c := range d.CheckGenerated(opts) {
		add(c)
	}
	add(d.CheckCommandPaths(opts.Dir, opts.Lang))
	add(d.checkClaudeVersion(ctx, opts.claudeProbe(), opts.Lang))
	add(checkHerdrIn(ctx, opts.HerdrSock, opts.Lang))
	if c, ok := d.cfg.CheckAgyHerdr(ctx, opts.HerdrSock, opts.AgyConfigDir, opts.Lang); ok { // agy with herdr only (agyhook.go)
		add(c)
	}
	add(d.checkGlobalPermissionHooksIn(opts.ClaudeConfigDir, opts.Dir, opts.Lang))
	add(d.cfg.CheckAgentSandbox(opts.Dir, opts.Lang))
	if opts.LaunchAgent != "" {
		add(checkLaunchAgent(inspectLaunchAgent(ctx, opts), opts))
	}
	if pre != nil {
		add(<-pre)
	}
	rep.OK = !slices.ContainsFunc(rep.Checks, func(c DoctorCheck) bool { return c.Status == DoctorFail })
	return rep
}

const DialTimeout = 3 * time.Second

func checkNodeSocketIn(ctx context.Context, path string, lang i18n.Lang) DoctorCheck {
	c := DoctorCheck{Name: "node.sock"}
	if _, err := os.Stat(path); err != nil {
		c.Status, c.Detail = DoctorFail, i18n.T(lang, "node-doctor.socket_missing")
		c.Hint = i18n.T(lang, "node-doctor.node_socket_missing_hint")
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	line, err := nodeclient.DialAt(ctx, path, nodeclient.Request{Kind: "ping"})
	if err != nil {
		c.Status = DoctorFail
		c.Detail = i18n.T(lang, "node-doctor.node_socket_unanswered", i18n.Args{"error": textfmt.Shorten(err.Error(), 80)})
		c.Hint = i18n.T(lang, "node-doctor.socket_stale_hint")
		return c
	}
	if !json.Valid(line) {
		c.Status, c.Detail = DoctorFail, i18n.T(lang, "node-doctor.node_socket_invalid")
		c.Hint = i18n.T(lang, "node-doctor.node_socket_invalid_hint")
		return c
	}
	c.Status, c.Detail = DoctorOK, i18n.T(lang, "node-doctor.node_socket_ok")
	return c
}

func checkRuntimeSocketIn(ctx context.Context, path string, lang i18n.Lang) DoctorCheck {
	c := DoctorCheck{Name: "runtime.sock"}
	if _, err := os.Stat(path); err != nil {
		c.Status, c.Detail = DoctorWarn, i18n.T(lang, "node-doctor.socket_missing")
		c.Hint = i18n.T(lang, "node-doctor.runtime_socket_missing_hint")
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		c.Status = DoctorFail
		c.Detail = i18n.T(lang, "node-doctor.runtime_socket_unavailable", i18n.Args{"error": textfmt.Shorten(err.Error(), 80)})
		c.Hint = i18n.T(lang, "node-doctor.socket_stale_hint")
		return c
	}
	conn.Close()
	c.Status, c.Detail = DoctorOK, i18n.T(lang, "node-doctor.runtime_socket_ok")
	return c
}

// CheckGenerated compares each generated file with what this binary would write: current, stale (differs, or part
// of a plugin is missing) or missing.
func (d *Doctor) CheckGenerated(opts DoctorOptions) []DoctorCheck {
	files, err := d.cfg.GeneratedFiles(opts.Bin, opts.Dir)
	if err != nil {
		return []DoctorCheck{{Name: "generated-files", Status: DoctorFail, Detail: err.Error(), Items: []string{}}}
	}
	var out []DoctorCheck
	for _, g := range GeneratedGroups() {
		var issues []string
		total := 0
		for _, f := range files {
			if f.Group != g {
				continue
			}
			total++
			got, err := os.ReadFile(filepath.Join(opts.Dir, f.Rel))
			switch {
			case err != nil:
				issues = append(issues, "missing "+filepath.ToSlash(f.Rel))
			case !bytes.Equal(got, f.Content):
				issues = append(issues, "differs "+filepath.ToSlash(f.Rel))
			}
		}
		allMissing := len(issues) == total && allPrefix(issues, "missing")
		out = append(out, generatedCheck(g, issues, allMissing, true, opts.Lang))
	}
	issues := assistantskills.PluginIssues(filepath.Join(opts.Dir, AgentPluginGroup))
	allMissing := len(issues) == 1 && issues[0] == "missing"
	return append(out, generatedCheck(AgentPluginGroup, issues, allMissing, false, opts.Lang))
}

// generatedCheck turns what differs in one generated file or directory into its check. critical: the sessions
// lose their hooks or MCP without it (missing fails), otherwise only the skills (missing warns).
func generatedCheck(name string, issues []string, allMissing, critical bool, lang i18n.Lang) DoctorCheck {
	c := DoctorCheck{Name: name, Items: issues}
	if c.Items == nil {
		c.Items = []string{}
	}
	switch {
	case len(issues) == 0:
		c.State, c.Status, c.Detail = "current", DoctorOK, i18n.T(lang, "node-doctor.generated_current")
	case allMissing:
		c.State, c.Status, c.Detail = "missing", DoctorWarn, i18n.T(lang, "node-doctor.generated_missing")
		if critical {
			c.Status = DoctorFail
			c.Hint = i18n.T(lang, "node-doctor.generated_missing_critical_hint")
		} else {
			c.Hint = i18n.T(lang, "node-doctor.generated_missing_plugin_hint")
		}
	default:
		c.State, c.Status = "stale", DoctorWarn
		c.Detail = i18n.T(lang, "node-doctor.generated_stale", i18n.Args{"count": len(issues), "issue": textfmt.Shorten(issues[0], 60)})
		c.Hint = i18n.T(lang, "node-doctor.generated_stale_hint")
	}
	return c
}

func allPrefix(items []string, prefix string) bool {
	return !slices.ContainsFunc(items, func(s string) bool { return !strings.HasPrefix(s, prefix) })
}

// configCommands are the programs a settings file or hooks plugin (top-level "hooks") runs: a shell-form hook's first
// word, an exec-form one's (with "args") command as it is.
func configCommands(b []byte) []string {
	var f struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"hooks"`
		} `json:"hooks"`
		MCPServers map[string]struct {
			Command string `json:"command"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	var out []string
	for _, matchers := range f.Hooks {
		for _, m := range matchers {
			for _, h := range m.Hooks {
				w := h.Command
				if h.Args == nil {
					w = shellcmd.FirstWord(h.Command)
				}
				if w != "" {
					out = append(out, w)
				}
			}
		}
	}
	for _, s := range f.MCPServers {
		if s.Command != "" {
			out = append(out, s.Command)
		}
	}
	return out
}

// CheckCommandPaths checks that each generated file's program exists and can be run. A file moved or removed under a
// running node (a redeploy to another path) leaves hooks that fail on every call.
func (d *Doctor) CheckCommandPaths(dir string, lang i18n.Lang) DoctorCheck {
	c := DoctorCheck{Name: "command-paths", Items: []string{}}
	files := []string{"claude-settings.json", "assistant-settings.json", "mcp.json"}
	for _, kind := range []string{"claude", "assistant"} {
		files = append(files, filepath.Join(kind+"-hooks", "hooks", "hooks.json"))
	}
	files = append(files, filepath.Join(d.cfg.LiveRepliesDir, "hooks", "hooks.json"))
	seen := map[string]bool{}
	var bad []string
	read := 0
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			continue
		}
		read++
		for _, p := range configCommands(b) {
			if seen[p] {
				continue
			}
			seen[p] = true
			if err := d.runnable(p); err != nil {
				bad = append(bad, fmt.Sprintf("%s: %v", p, err))
			}
		}
	}
	switch {
	case len(bad) > 0:
		slices.Sort(bad)
		c.Status, c.Items = DoctorFail, bad
		c.Detail = i18n.T(lang, "node-doctor.command_paths_bad", i18n.Args{"count": len(bad), "issue": textfmt.Shorten(bad[0], 80)})
		c.Hint = i18n.T(lang, "node-doctor.command_paths_bad_hint")
	case read == 0:
		c.Status, c.Detail = DoctorWarn, i18n.T(lang, "node-doctor.command_paths_none")
		c.Hint = i18n.T(lang, "node-doctor.command_paths_none_hint")
	default:
		c.Status, c.Detail = DoctorOK, i18n.T(lang, "node-doctor.command_paths_ok", i18n.Args{"count": len(seen)})
	}
	return c
}

func (d *Doctor) runnable(path string) error {
	if !filepath.IsAbs(path) { // a bare name: found on PATH like claude would
		if _, err := d.cfg.LookPath(path); err != nil {
			return errors.New("not on PATH")
		}
		return nil
	}
	st, err := os.Stat(path)
	if err != nil {
		return errors.New("not found")
	}
	if st.IsDir() || (runtime.GOOS != "windows" && st.Mode()&0o111 == 0) {
		return errors.New("not executable")
	}
	return nil
}

// ClaudeVersionUnanswered is the claude-version check's state when no version was read but the shell did find claude
// (it ran out of time, or printed none): the assistant's readiness then says unknown, not claude_missing. The hub
// matches the literal (assistant/readiness.go); "" is every other outcome, as with nodes from before it.
const ClaudeVersionUnanswered = "unanswered"

// claudeProbe is ClaudeProbe, or ClaudeVersion read as it always was: no version is no claude.
func (o DoctorOptions) claudeProbe() func(context.Context) (string, bool) {
	if o.ClaudeProbe != nil {
		return o.ClaudeProbe
	}
	if o.ClaudeVersion == nil {
		return nil
	}
	return func(ctx context.Context) (string, bool) {
		v := o.ClaudeVersion(ctx)
		return v, v == ""
	}
}

func (d *Doctor) checkClaudeVersion(ctx context.Context, ask func(context.Context) (string, bool), lang i18n.Lang) DoctorCheck {
	c := DoctorCheck{Name: "claude-version"}
	v, missing := "", true
	if ask != nil {
		v, missing = ask(ctx)
	}
	switch {
	case v == "" && !missing:
		c.Status, c.State, c.Detail = DoctorWarn, ClaudeVersionUnanswered, i18n.T(lang, "node-doctor.claude_unanswered")
		c.Hint = i18n.T(lang, "node-doctor.claude_unanswered_hint")
	case v == "":
		c.Status, c.Detail = DoctorWarn, i18n.T(lang, "node-doctor.claude_missing")
		c.Hint = i18n.T(lang, "node-doctor.claude_missing_hint")
	case !claudeplugin.VersionAtLeast(v, d.cfg.PluginMinVersion):
		c.Status = DoctorWarn
		min := fmt.Sprintf("%d.%d.%d", d.cfg.PluginMinVersion[0], d.cfg.PluginMinVersion[1], d.cfg.PluginMinVersion[2])
		c.Detail = i18n.T(lang, "node-doctor.claude_too_old", i18n.Args{"version": v, "minimum": min})
		c.Hint = i18n.T(lang, "node-doctor.claude_too_old_hint")
	default:
		c.Status, c.Detail = DoctorOK, i18n.T(lang, "node-doctor.claude_ok", i18n.Args{"version": v})
	}
	return c
}

// checkHerdrIn dials herdr's API socket. The engine is chosen by the hub when it connects and is not kept on this
// machine, so a socket that is not there is a warning: it only matters when herdr is this machine's engine.
func checkHerdrIn(ctx context.Context, path string, lang i18n.Lang) DoctorCheck {
	c := DoctorCheck{Name: "herdr"}
	if path == "" {
		c.Status, c.Detail = DoctorWarn, i18n.T(lang, "node-doctor.herdr_no_path")
		c.Hint = i18n.T(lang, "node-doctor.herdr_no_path_hint")
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, DialTimeout)
	defer cancel()
	conn, err := herdr.Dial(ctx, path)
	if err != nil {
		c.Status, c.Detail = DoctorWarn, i18n.T(lang, "node-doctor.herdr_unavailable")
		c.Hint = i18n.T(lang, "node-doctor.herdr_unavailable_hint")
		return c
	}
	conn.Close()
	c.Status, c.Detail = DoctorOK, i18n.T(lang, "node-doctor.herdr_ok")
	return c
}

// checkGlobalPermissionHooksIn looks at the user's own settings.json for synchronous PermissionRequest hooks (other
// tools put them there) and says how many; it never changes the file. One whose program is in a package of an
// extension installed on this computer (dataDir's node-plugins, extensionHookOwner) is that extension's, named and
// not warned about (#11973).
func (d *Doctor) checkGlobalPermissionHooksIn(configDir, dataDir string, lang i18n.Lang) DoctorCheck {
	c := DoctorCheck{Name: "global-permission-hooks", Items: []string{}}
	b, err := os.ReadFile(filepath.Join(configDir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		c.Status, c.Detail = DoctorOK, i18n.T(lang, "node-doctor.global_hooks_no_file")
		return c
	}
	if err != nil {
		c.Status = DoctorWarn
		c.Detail = i18n.T(lang, "node-doctor.global_hooks_unreadable", i18n.Args{"error": textfmt.Shorten(err.Error(), 60)})
		return c
	}
	var f struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string   `json:"type"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
				Async   bool     `json:"async"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(b, &f) != nil {
		c.Status, c.Detail = DoctorWarn, i18n.T(lang, "node-doctor.global_hooks_invalid")
		c.Hint = i18n.T(lang, "node-doctor.global_hooks_invalid_hint")
		return c
	}
	var foreign, extensions []string
	fromExtensions := 0
	for _, m := range f.Hooks["PermissionRequest"] {
		for _, h := range m.Hooks {
			if h.Async || h.Type != "command" && h.Type != "" {
				continue
			}
			c.Items = append(c.Items, textfmt.Shorten(h.Command, 80))
			if name := d.extensionHookOwner(dataDir, d.hookProgram(h.Command, h.Args)); name != "" {
				fromExtensions++
				if !slices.Contains(extensions, name) {
					extensions = append(extensions, name)
				}
				continue
			}
			foreign = append(foreign, textfmt.Shorten(h.Command, 80))
		}
	}
	names := strings.Join(extensions, i18n.T(lang, "node-doctor.list_separator"))
	switch {
	case len(c.Items) == 0:
		c.Status, c.Detail = DoctorOK, i18n.T(lang, "node-doctor.global_hooks_none")
	case len(foreign) == 0: // all the user's own choice: installed extensions'
		c.Status = DoctorOK
		c.Detail = i18n.T(lang, "node-doctor.global_hooks_extensions", i18n.Args{"count": fromExtensions, "extensions": names})
	default:
		c.Status = DoctorWarn
		c.Detail = i18n.T(lang, "node-doctor.global_hooks_found", i18n.Args{"count": len(foreign), "hook": foreign[0]})
		if fromExtensions > 0 {
			c.Detail = i18n.T(lang, "node-doctor.global_hooks_found_with_extensions", i18n.Args{
				"count": len(foreign), "hook": foreign[0], "extension_count": fromExtensions, "extensions": names,
			})
		}
		c.Hint = i18n.T(lang, "node-doctor.global_hooks_found_hint")
	}
	return c
}

// Command runs the doctor command with the selected terminal language.
func (d *Doctor) Command(ctx context.Context, args []string, out io.Writer, lang i18n.Lang, defaults func() (DoctorOptions, error)) (failed bool, err error) {
	flags := flag.NewFlagSet("node doctor", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, i18n.T(lang, "node-doctor.flag_json"))
	fix := flags.Bool("fix", false, i18n.T(lang, "node-doctor.flag_fix"))
	if err := flags.Parse(args); err != nil {
		return false, err
	}
	if flags.NArg() != 0 {
		return false, errors.New(i18n.T(lang, "node-doctor.positional_arguments"))
	}
	if *asJSON {
		// JSON is machine output: every human field keeps its legacy zh-Hans bytes, including a preflight answer
		// requested from the resident node. The terminal language is used only for flag help and argument errors.
		lang = i18n.Default
	}
	opts, err := defaults()
	if err != nil {
		return false, err
	}
	opts.Fix = *fix
	opts.Lang = lang
	rep := d.Run(ctx, opts)
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return !rep.OK, enc.Encode(rep)
	}
	PrintDoctorIn(out, rep, lang)
	return !rep.OK, nil
}

// PrintDoctor writes the legacy zh-Hans text report. New display paths choose their language explicitly.
func PrintDoctor(out io.Writer, rep DoctorReport) {
	PrintDoctorIn(out, rep, i18n.Default)
}

// PrintDoctorIn writes rep, one line per check: label | name: what was found; what to do about it.
func PrintDoctorIn(out io.Writer, rep DoctorReport, lang i18n.Lang) {
	fmt.Fprintln(out, i18n.T(lang, "node-doctor.title", i18n.Args{"dir": rep.Dir}))
	counts := map[string]int{}
	for _, c := range rep.Checks {
		counts[c.Status]++
		args := i18n.Args{"label": doctorLabel(lang, c.Status), "name": c.Name, "detail": c.Detail}
		key := "node-doctor.check_line"
		if c.Hint != "" && c.Status != DoctorOK {
			key = "node-doctor.check_line_hint"
			args["hint"] = c.Hint
		}
		fmt.Fprintln(out, i18n.T(lang, key, args))
	}
	var regenerated []string
	for _, f := range rep.Fixed {
		switch f {
		case launchdCheck:
			fmt.Fprintln(out, i18n.T(lang, "node-doctor.fixed_launchd"))
		case launchdReloaded:
			fmt.Fprintln(out, i18n.T(lang, "node-doctor.fixed_reloaded"))
		default:
			regenerated = append(regenerated, f)
		}
	}
	if len(regenerated) > 0 {
		fmt.Fprintln(out, i18n.T(lang, "node-doctor.fixed_regenerated", i18n.Args{
			"items": strings.Join(regenerated, i18n.T(lang, "node-doctor.list_separator")),
		}))
	}
	fmt.Fprintln(out, i18n.T(lang, "node-doctor.summary", i18n.Args{
		"total": len(rep.Checks), "ok": counts[DoctorOK], "warn": counts[DoctorWarn], "fail": counts[DoctorFail],
	}))
}

func doctorLabel(lang i18n.Lang, status string) string {
	key := map[string]string{
		DoctorOK: "node-doctor.label_ok", DoctorWarn: "node-doctor.label_warn", DoctorFail: "node-doctor.label_fail",
	}[status]
	if key == "" {
		return ""
	}
	return i18n.T(lang, key)
}

// GeneratedGroups returns the report order of the node's critical configuration.
func GeneratedGroups() []string {
	return []string{"claude-settings.json", "claude-hooks", "assistant-settings.json", "assistant-hooks", "mcp.json"}
}
