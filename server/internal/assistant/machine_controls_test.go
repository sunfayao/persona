package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/extmgr"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// controlsExt is extensions that say what can be done on each Mac (machineControlSource), and nothing else.
type controlsExt struct {
	ExtensionTools
	controls map[string]extmgr.MachineControl
}

func (e *controlsExt) MachineControls() map[string]extmgr.MachineControl { return e.controls }

// AgentTools: no connector of this fake offers tools, so the instructions name no mailbox and no subscription.
func (e *controlsExt) AgentTools(context.Context, bool) (agents, assistantOnly []proto.Tool) {
	return nil, nil
}

// machineLines are the instructions' line for each computer, by id.
func machineLines(t *testing.T, md string, machines ...Device) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, line := range strings.Split(md, "\n") {
		for _, m := range machines {
			if strings.HasPrefix(line, "  - "+m.Name+" (`"+m.ID+"`)") {
				out[m.ID] = line
			}
		}
	}
	if len(out) != len(machines) {
		t.Fatalf("the instructions leave out machines: %v", out)
	}
	return out
}

// The instructions said "hand work to the Agents on it" of every computer, also of a Mac the assistant can operate
// itself (pain point 4 of the tool audit): each line now says what the user turned on there, and where its live
// screen is.
func TestMachineLinesSayWhatCanBeDoneThere(t *testing.T) {
	ctx := context.Background()
	machines := []Device{{ID: "server", Name: "Server", Kind: "local"}, {ID: "mac", Name: "Mac", Kind: "link"},
		{ID: "mini", Name: "Mini", Kind: "link"}, {ID: "box", Name: "Box", Kind: "ssh"}}
	f := claudeMDHub(machines...)
	f.answerApp(AppFeishu, FeishuStatusGet, func(json.RawMessage) (any, error) {
		return FeishuStatus{PublicURL: "https://hub.example.com/"}, nil
	})
	s := newTestService(t, f)
	plain := s.claudeMD(ctx, "server", false)
	ext := &controlsExt{}
	s.SetExtensions(ext)
	if got := s.claudeMD(ctx, "server", false); got != plain || strings.Contains(got, "Operating a Mac") {
		t.Fatal("with nothing turned on, the instructions changed")
	}
	ext.controls = map[string]extmgr.MachineControl{
		"mac":  {ComputerUse: "lcu", Driver: "lcu", Browser: true, Reported: true, ScreenSharing: true, ScreenPermission: "missing", ScreenAsked: true},
		"mini": {Browser: true, ScreenPermission: "missing"}, // macOS has not been asked yet: its alert is still to come
	}
	md := s.claudeMD(ctx, "server", false)
	lines, before := machineLines(t, md, machines...), machineLines(t, plain, machines...)
	for _, want := range []string{"hand work to the Agents on it; computer use (LCU); browser; live screen https://hub.example.com/screen/mac",
		"until the user allows it in System Settings › Privacy & Security › Screen & System Audio Recording › Persona Screen"} {
		if !strings.Contains(lines["mac"], want) {
			t.Errorf("the Mac's line lacks %q: %s", want, lines["mac"])
		}
	}
	if want := "hand work to the Agents on it; browser; live screen https://hub.example.com/screen/mini"; !strings.HasSuffix(lines["mini"], want) {
		t.Errorf("the browser-only Mac's line: %s", lines["mini"])
	}
	if lines["box"] != before["box"] || lines["server"] != before["server"] {
		t.Errorf("computers without either changed: %v", lines)
	}
	if !strings.Contains(md, "- **Operating a Mac or its browser**") || !strings.Contains(md, "opens in View only") {
		t.Fatal("no word on operating a Mac")
	}
	// What a Mac said last (away, Screen Sharing off) is machines_list's: it never rewrites the instructions, which would
	// start a Claude conversation's process again.
	mac := ext.controls["mac"]
	mac.Reported, mac.ScreenSharing, mac.Driver = false, false, "auto"
	ext.controls["mac"] = mac
	if got := s.claudeMD(ctx, "server", false); got != md {
		t.Fatal("a Mac's last status changed the instructions")
	}
	mac.ComputerUse = "auto"
	ext.controls["mac"] = mac
	if got := machineLines(t, s.claudeMD(ctx, "server", false), machines...)["mac"]; !strings.Contains(got, "computer use (LCU or Cua Driver)") {
		t.Fatalf("auto: %s", got)
	}
	// Without the hub's address there is no link, and the rest stays.
	f.answerApp(AppFeishu, FeishuStatusGet, func(json.RawMessage) (any, error) { return FeishuStatus{}, nil })
	if got := machineLines(t, s.claudeMD(ctx, "server", false), machines...)["mini"]; !strings.HasSuffix(got, "hand work to the Agents on it; browser") {
		t.Fatalf("no address: %s", got)
	}
}

// CLAUDE.md is read by Claude Code only; AGENTS.md by Codex and Antigravity, which have no Monitor, no AskUserQuestion,
// no auto memory and no ~/.claude, and Antigravity reaches the tools outside its list through persona_call.
func TestInstructionsAreWordedForTheirRuntime(t *testing.T) {
	claudeOnly := []string{"Monitor (at most 30 minutes", "run_in_background", "AskUserQuestion", "(auto memory)",
		"`ls ~/.claude/skills`", "~/.claude/projects"}
	for _, guide := range []bool{false, true} {
		render := func(runtime string) string {
			return prompts.RenderAssistantClaudeMD(prompts.AssistantClaudeMD{WorkGuide: guide, Runtime: runtime,
				Machines: []prompts.AssistantMachine{{Name: "Server", ID: "server", Local: true}}})
		}
		claude := render(prompts.RuntimeClaude)
		if render("") != claude {
			t.Fatal("no runtime is not Claude's")
		}
		for _, want := range claudeOnly {
			if !strings.Contains(claude, want) {
				t.Errorf("work guide %t: CLAUDE.md lost %q", guide, want)
			}
		}
		for _, runtime := range []string{prompts.RuntimeCodex, prompts.RuntimeAgy} {
			got := render(runtime)
			for _, phrase := range claudeOnly {
				if strings.Contains(got, phrase) {
					t.Errorf("work guide %t: %s's AGENTS.md says %q", guide, runtime, phrase)
				}
			}
			for _, want := range []string{"`schedule_followup` brings you back to look again", "your memory, as Persona's memory notes describe it",
				"use `ask_user` to confirm or choose", "Write every compaction summary in"} {
				if !strings.Contains(got, want) {
					t.Errorf("work guide %t: %s's AGENTS.md lacks %q", guide, runtime, want)
				}
			}
			if strings.Contains(got, "persona_call") != (runtime == prompts.RuntimeAgy) {
				t.Errorf("work guide %t: %s and persona_call", guide, runtime)
			}
		}
	}
}

// The prepare before every message gives the node AGENTS.md in the words of the main chat's launcher, beside
// CLAUDE.md, and for Antigravity how much of it that reads (agents_md_split); a change of launcher rewrites AGENTS.md
// only.
func TestPrepareWritesAgentsMDForTheMainChatsLauncher(t *testing.T) {
	ctx := context.Background()
	f := claudeMDHub(Device{ID: "server", Name: "Server", Kind: "local"})
	s := newTestService(t, f)
	prepared := func() map[string]any {
		t.Helper()
		if _, err := s.prepare(ctx, "server"); err != nil {
			t.Fatal(err)
		}
		return f.prepared[len(f.prepared)-1]
	}
	p := prepared()
	claude, _ := p["claude_md"].(string)
	agents, _ := p["agents_md"].(string)
	if claude != s.claudeMD(ctx, "server", false) || !strings.Contains(claude, "AskUserQuestion") {
		t.Fatalf("CLAUDE.md: %v", p)
	}
	if agents == "" || strings.Contains(agents, "AskUserQuestion") || strings.Contains(agents, "persona_call") {
		t.Fatalf("AGENTS.md for a Claude main chat is Codex's: %q", agents)
	}
	if _, ok := p["agents_work_guide_md"]; ok {
		t.Fatal("a work guide AGENTS.md while the guide is off")
	}
	if _, ok := p["agents_md_split"]; ok {
		t.Fatal("Codex's AGENTS.md is split: it reads the file whole")
	}

	setSettings(t, s, func(v *Settings) { v.Launcher = "agy" })
	if _, wrote, err := s.prepareSpaceIf(ctx, "server", "", true); err != nil || !wrote {
		t.Fatalf("the refresh after a change of launcher wrote %t: %v", wrote, err)
	}
	p = f.prepared[len(f.prepared)-1]
	if p["claude_md"] != claude || !strings.Contains(p["agents_md"].(string), "persona_call") {
		t.Fatalf("an Antigravity main chat: %v", p)
	}
	// Antigravity reads a rule file only so far: the node puts what follows in GEMINI.md (node splitRules).
	if p["agents_md_split"] != float64(agyRuleFileMax) {
		t.Fatalf("an Antigravity main chat's AGENTS.md split at %v, want %d", p["agents_md_split"], agyRuleFileMax)
	}
	if _, wrote, err := s.prepareSpaceIf(ctx, "server", "", true); err != nil || wrote {
		t.Fatalf("unchanged, the refresh wrote %t: %v", wrote, err)
	}

	on := true
	setSettings(t, s, func(v *Settings) { v.WorkGuide = &on })
	p = prepared()
	guide, _ := p["agents_work_guide_md"].(string)
	if !strings.Contains(guide, "persona-work") || !strings.Contains(guide, "persona_call") || p["work_guide_md"] == guide {
		t.Fatalf("the work guide's AGENTS.md: %v", p)
	}
}

// AGENTS.md for each runtime, on the computers of TestGoldenAssistantClaudeMD with a Mac the assistant may operate.
func TestGoldenAssistantInstructionsPerRuntime(t *testing.T) {
	ctx := context.Background()
	f := claudeMDHub(Device{ID: "server", Name: "服务器", Kind: "local"}, Device{ID: "laptop", Name: "笔记本", Kind: "ssh", SSHHost: "laptop.example"},
		Device{ID: "desk", Name: "台式机", Kind: "ssh"})
	f.hello.Owner = "示例用户"
	f.hello.OwnerNotes = "  我在示例公司做后端。\n常用 skills：demo-skill。  "
	f.answerApp(AppFeishu, FeishuStatusGet, func(json.RawMessage) (any, error) {
		return FeishuStatus{PublicURL: "https://hub.example.com"}, nil
	})
	s := newTestService(t, f)
	s.SetExtensions(&controlsExt{controls: map[string]extmgr.MachineControl{
		"laptop": {ComputerUse: "cua", Driver: "cua", Browser: true, Reported: true, ScreenSharing: true, ScreenPermission: "missing", ScreenAsked: true},
	}})
	d := s.assistantPrompt(ctx, "server", false, s.settings(ctx))
	for _, runtime := range []string{prompts.RuntimeClaude, prompts.RuntimeCodex, prompts.RuntimeAgy} {
		d.Runtime = runtime
		golden(t, "assistant_claude_md.controls."+runtime, prompts.RenderAssistantClaudeMD(d))
	}
}
