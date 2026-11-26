package node

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/extension/sandbox"
	"github.com/sunfayao/persona/server/internal/herdr"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/identity"
	"github.com/sunfayao/persona/server/internal/node/doctor"
	"github.com/sunfayao/persona/server/internal/nodeclient"
)

type DoctorCheck = doctor.DoctorCheck
type DoctorReport = doctor.DoctorReport
type DoctorOptions = doctor.DoctorOptions

const (
	DoctorOK                = doctor.DoctorOK
	DoctorWarn              = doctor.DoctorWarn
	DoctorFail              = doctor.DoctorFail
	ClaudeVersionUnanswered = doctor.ClaudeVersionUnanswered
	doctorDialTimeout       = doctor.DialTimeout
	NodeLaunchdLabel        = doctor.NodeLaunchdLabel
	legacyNodeLaunchdLabel  = doctor.LegacyNodeLaunchdLabel
)

func newDoctor() *doctor.Doctor {
	return doctor.New(doctor.Config{
		PluginMinVersion: pluginMinVersion, GeneratedFiles: generatedFiles, WriteGeneratedConfig: writeGeneratedConfig,
		CheckAgyHerdr: checkAgyHerdrIn, CheckAgentSandbox: checkAgentSandboxIn,
		LiveRepliesDir: liveRepliesDir, NodePluginsDir: nodePluginsDir,
		OwnsPlugin:  func(root, id string) bool { return (&pluginHost{dir: root}).owns(id) },
		ValidDigest: digestPattern.MatchString, LookPath: exec.LookPath,
	})
}

func RunDoctor(ctx context.Context, opts DoctorOptions) DoctorReport {
	return newDoctor().Run(ctx, opts)
}

func PrintDoctor(out io.Writer, rep DoctorReport) { doctor.PrintDoctor(out, rep) }

func PrintDoctorIn(out io.Writer, rep DoctorReport, lang i18n.Lang) {
	doctor.PrintDoctorIn(out, rep, lang)
}

func NodeLaunchAgentPath(home, label string) string {
	return doctor.NodeLaunchAgentPath(home, label)
}

func runLaunchctl(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
	return string(out), err
}

// DefaultDoctorOptions is this machine's: the same places the node itself uses, without creating or moving any.
func DefaultDoctorOptions() (DoctorOptions, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return DoctorOptions{}, err
	}
	dir := identity.DataDir(home)
	// The node's own spelling of this program (selfprogram.go): the generated files name it, and another one would
	// read as stale here and --fix would write them back the other way.
	_, bin, err := selfProgram(home, dir)
	if err != nil {
		return DoctorOptions{}, err
	}
	opts := DoctorOptions{
		Dir: dir, Bin: bin,
		NodeSock: nodeclient.SocketPath(), RuntimeSock: RuntimeSocketPath(), HerdrSock: herdr.SocketPath(),
		ClaudeConfigDir: claudeDirOf(home), ClaudeVersion: claudeVersion, ClaudeProbe: claudeProbe,
		AgyConfigDir: agyHookDir(home),
	}
	withDoctorLaunchd(&opts, home)
	// Under the node's agent (a claude the node started runs the doctor) a reload would stop the doctor too: under
	// either label, today's or the one a Mac set up before the rename keeps.
	opts.InsideNode = doctor.IsNodeLaunchdLabel(os.Getenv("XPC_SERVICE_NAME"))
	if runtime.GOOS != "windows" {
		opts.Preflight = preflightFromNode(opts.NodeSock, home, opts.ClaudeConfigDir)
	}
	return opts, nil
}

// withLaunchd points the launchd-process-type check at the resident node's agent, on a Mac only.
func withDoctorLaunchd(o *DoctorOptions, home string) {
	if runtime.GOOS != "darwin" {
		return
	}
	// launchctl is asked only when both labels have a plist; bounded, as everything else here.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o.WithLaunchAgent(ctx, home, "gui/"+strconv.Itoa(os.Getuid()), os.Getenv("XPC_SERVICE_NAME"), runLaunchctl)
}

// DoctorCommand is `personad node doctor [--json] [--fix]`: it prints the report to out and says whether a check
// failed (the caller exits non-zero then).
func DoctorCommand(ctx context.Context, args []string, out io.Writer) (failed bool, err error) {
	return doctorCommand(ctx, args, out, DefaultDoctorOptions)
}

func doctorCommand(ctx context.Context, args []string, out io.Writer, defaults func() (DoctorOptions, error)) (failed bool, err error) {
	if len(args) > 0 && args[0] == preflightChildArg { // the preflight's reads, in a child of the doctor's (preflight.go)
		preflightChild(args[1:], out)
		return false, nil
	}
	return newDoctor().Command(ctx, args, out, i18n.Terminal(), defaults)
}

func checkAgentSandbox(dir string) DoctorCheck {
	return checkAgentSandboxIn(dir, i18n.Default)
}

func checkAgentSandboxIn(dir string, lang i18n.Lang) DoctorCheck {
	c := DoctorCheck{Name: "agent-sandbox", Status: DoctorOK, Items: []string{}}
	on, from := agentSandboxOn(dir)
	support := agentSandboxProbe() // macOS runs sandbox-exec true; Linux runs a real default-allow bwrap probe
	sup := sandbox.AgentSupport(support)
	supWhy := sup.WhyIn(lang)
	last := readAgentSandboxStatus(dir)
	wheres := make([]string, 0, len(last))
	for w := range last {
		wheres = append(wheres, w)
	}
	slices.Sort(wheres)
	for _, w := range wheres {
		r := last[w]
		key := "node-doctor.sandbox_status_item"
		args := i18n.Args{"where": w, "status": agentSandboxWord(lang, r.State), "at": r.At.Local().Format("01-02 15:04")}
		if r.Why != "" {
			key = "node-doctor.sandbox_status_item_why"
			args["why"] = r.WhyIn(lang)
		}
		c.Items = append(c.Items, i18n.T(lang, key, args))
	}
	switch {
	case !on:
		if from == "" {
			c.Detail = i18n.T(lang, "node-doctor.sandbox_off_default")
		} else {
			c.Detail = i18n.T(lang, "node-doctor.sandbox_off_from", i18n.Args{"from": from})
		}
		switch sup.State {
		case sandbox.AgentActive:
			c.Detail = i18n.T(lang, "node-doctor.sandbox_off_available", i18n.Args{"detail": c.Detail})
			c.Hint = i18n.T(lang, "node-doctor.sandbox_enable_hint", i18n.Args{"path": filepath.Join(dir, sandbox.AgentOnFile)})
		case sandbox.AgentUnsupported:
			c.Detail = i18n.T(lang, "node-doctor.sandbox_off_unsupported", i18n.Args{"detail": c.Detail, "why": supWhy})
		default:
			c.Detail = i18n.T(lang, "node-doctor.sandbox_off_unavailable", i18n.Args{"detail": c.Detail, "why": supWhy})
		}
	case sup.State == sandbox.AgentUnsupported:
		c.Status = DoctorWarn
		c.Detail = i18n.T(lang, "node-doctor.sandbox_on_unsupported", i18n.Args{"why": supWhy})
		c.Hint = i18n.T(lang, "node-doctor.sandbox_runs_outside_hint")
	case sup.State == sandbox.AgentUnavailable:
		c.Status = DoctorWarn
		c.Detail = i18n.T(lang, "node-doctor.sandbox_on_unavailable", i18n.Args{"why": supWhy})
		c.Hint = i18n.T(lang, "node-doctor.sandbox_runs_outside_hint")
	default:
		c.Detail = i18n.T(lang, "node-doctor.sandbox_on", i18n.Args{"from": from, "limits": agentSandboxLimitsIn(support.OS, lang)})
		c.Hint = i18n.T(lang, "node-doctor.sandbox_environment_hint", i18n.Args{"env": agentSandboxEnv})
		for _, w := range wheres {
			if s := last[w].State; s == sandbox.AgentSkipped || s == sandbox.AgentUnavailable {
				c.Status = DoctorWarn
				c.Detail = i18n.T(lang, "node-doctor.sandbox_on_skipped", i18n.Args{"detail": c.Detail})
				c.Hint = i18n.T(lang, "node-doctor.sandbox_skipped_hint")
				break
			}
		}
	}
	if len(c.Items) > 0 {
		c.Detail = i18n.T(lang, "node-doctor.sandbox_with_recent", i18n.Args{
			"detail": c.Detail, "items": strings.Join(c.Items, i18n.T(lang, "node-doctor.item_separator")),
		})
	}
	return c
}

// agentSandboxLimits is what the doctor reminds of while the sandbox is on: what it does not
// stop, which agents it does not cover, and platform-specific behavior that needs a restart or stops working.
func agentSandboxLimits(goos string) string {
	return agentSandboxLimitsIn(goos, i18n.Default)
}

func agentSandboxLimitsIn(goos string, lang i18n.Lang) string {
	limits := i18n.T(lang, "node-doctor.sandbox_limits")
	switch goos {
	case "darwin":
		return i18n.T(lang, "node-doctor.sandbox_limits_darwin", i18n.Args{"limits": limits})
	case "linux":
		return i18n.T(lang, "node-doctor.sandbox_limits_linux", i18n.Args{"limits": limits})
	default:
		return limits
	}
}

// agentSandboxWord is one status in the doctor's words. Unknown future states keep the old empty word.
func agentSandboxWord(lang i18n.Lang, state string) string {
	key := map[string]string{
		sandbox.AgentOff:         "node-doctor.sandbox_state_off",
		sandbox.AgentActive:      "node-doctor.sandbox_state_active",
		sandbox.AgentSkipped:     "node-doctor.sandbox_state_skipped",
		sandbox.AgentUnsupported: "node-doctor.sandbox_state_unsupported",
		sandbox.AgentUnavailable: "node-doctor.sandbox_state_unavailable",
	}[state]
	if key == "" {
		return ""
	}
	return i18n.T(lang, key)
}

func checkCommandPaths(dir string) DoctorCheck {
	return newDoctor().CheckCommandPaths(dir, i18n.Default)
}
