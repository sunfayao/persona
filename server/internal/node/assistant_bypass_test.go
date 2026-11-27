package node

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestAssistantUnselectedModeStartsBypass(t *testing.T) {
	f := cliTest(t)
	id, err := f.n.assistantSend(context.Background(), "", t.TempDir(), "claude", "hello", nil, "", "", cliExec())
	if err != nil {
		t.Fatal(err)
	}
	f.idle(t, id)
	if argv := f.file("cli-argv"); !strings.Contains(argv, "--permission-mode "+proto.ModeBypass) {
		t.Fatalf("startup args %q, want --permission-mode bypassPermissions", argv)
	}
	if err := f.n.setModes(context.Background(), id, "default", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.n.assistantSend(context.Background(), id, "", "", "next", nil, "", ""); err != nil {
		t.Fatal(err)
	}
	f.idle(t, id)
	if mode, _ := f.n.modesOf(id); mode != "default" {
		t.Fatalf("resumed mode = %q, lost explicit selection", mode)
	}
}

func TestAssistantBypassCommandGate(t *testing.T) {
	for _, tc := range []struct {
		name, mode, command string
		assistant, health   bool
		want                string
		cards               int
	}{
		{"read", proto.ModeBypass, "ex GET /health", true, false, "allow", 0},
		{"authorized write", proto.ModeBypass, "ex POST /notes '{}'", true, false, "allow", 0},
		{"compound command", proto.ModeBypass, "for value in one; do ex POST /notes '{}'; done", true, false, "allow", 0},

		{"incident env read redirect filter", proto.ModeBypass, "NO_COLOR=1 NO_PROGRESS=1 lark-cli docs +fetch --doc-id example > /tmp/doc.json | jq '.data'", true, false, "allow", 0},
		{"incident without risk metadata", proto.ModeBypass, "NO_COLOR=1 NO_PROGRESS=1 lark-cli docs +fetch --doc-id example > /tmp/doc.json | jq '.data'", true, false, "allow", 0},
		{"CLI ordinary write", proto.ModeBypass, "lark-cli docs +create --title example", true, false, "allow", 0},
		{"CLI send", proto.ModeBypass, "lark-cli im +messages-send --as user --text hello", true, false, "deny", 1},
		{"CLI bot send", proto.ModeBypass, "lark-cli im +messages-send --as bot --text hello", true, false, "allow", 0},
		{"CLI bot delete", proto.ModeBypass, "lark-cli drive files delete --as bot --file-token example", true, false, "deny", 1},
		{"CLI native send", proto.ModeBypass, "lark-cli im messages create --text hello", true, false, "deny", 1},
		{"CLI reply", proto.ModeBypass, "lark-cli im +messages-reply --text hello", true, false, "deny", 1},
		{"CLI comment", proto.ModeBypass, "lark-cli drive files comments create --text hello", true, false, "deny", 1},
		{"CLI mail", proto.ModeBypass, "lark-cli mail user_mailboxes messages send --data '{}'", true, false, "deny", 1},
		{"CLI delete", proto.ModeBypass, "lark-cli drive files delete --file-token example", true, false, "deny", 1},
		{"API send", proto.ModeBypass, "lark-cli api POST /open-apis/im/v1/messages --data '{}'", true, false, "deny", 1},
		{"API delete", proto.ModeBypass, "lark-cli api DELETE /open-apis/drive/v1/files/example", true, false, "deny", 1},
		{"gate send", proto.ModeBypass, "ex POST /conversations/example/messages '{}'", true, false, "deny", 1},
		{"gate delete", proto.ModeBypass, "ex DELETE /files/example", true, false, "deny", 1},
		{"loop read", proto.ModeBypass, "for value in one; do NO_COLOR=1 lark-cli docs +fetch --doc-id example | head; done", true, false, "allow", 0},
		{"loop ordinary write", proto.ModeBypass, "for value in one; do lark-cli docs +create --title example; done", true, false, "allow", 0},
		{"loop send", proto.ModeBypass, "for value in one; do lark-cli im +messages-send --text hello; done", true, false, "deny", 1},
		{"substitution read", proto.ModeBypass, "echo $(lark-cli api GET /open-apis/docx/v1/documents/example)", true, false, "deny", 1},
		{"substitution send", proto.ModeBypass, "echo $(lark-cli im +messages-send --text hello)", true, false, "deny", 1},
		{"script read", proto.ModeBypass, "sh -c 'lark-cli docs +fetch --doc-id example | head'", true, false, "allow", 0},
		{"script send", proto.ModeBypass, "sh -c 'lark-cli im +messages-send --text hello'", true, false, "deny", 1},
		{"bot API delete action", proto.ModeBypass, "lark-cli api POST /open-apis/drive/v1/files/example/comments/delete --as bot", true, false, "deny", 1},
		{"unknown script argv", proto.ModeBypass, `python3 -c 'run(["lark-cli", "im", "+messages-send"])'`, true, false, "deny", 1},
		{"unknown script", proto.ModeBypass, `python3 -c 'run("lark-cli im +messages-send")'`, true, false, "deny", 1},
		{"mixed read and send", proto.ModeBypass, "lark-cli docs +fetch --doc-id example | jq '.data'; lark-cli im +messages-send --text hello", true, false, "deny", 1},
		{"mixed read and unknown gate", proto.ModeBypass, "ex GET /health; for value in one; do ex POST $route; done", true, false, "deny", 1},
		{"unknown CLI write", proto.ModeBypass, "for value in one; do lark-cli custom $action; done", true, false, "deny", 1},
		{"explicit deny loop", proto.ModeBypass, "for value in one; do ex DELETE /notes/one; done", true, false, "deny", 0},
		{"explicit CLI deny loop", proto.ModeBypass, "for value in one; do lark-cli im +messages-send --text hello; done", true, false, "deny", 0},
		{"proxy write loop", proto.ModeBypass, "for value in one; do ex POST /notes '{}'; done", true, false, "deny", 0},
		{"proxy write script", proto.ModeBypass, "sh -c 'ex POST /notes {}'", true, false, "deny", 0},
		{"explicit default", "default", "ex POST /notes '{}'", true, false, "deny", 1},
		{"ordinary agent", proto.ModeBypass, "ex POST /notes '{}'", false, false, "deny", 1},
		{"health", proto.ModeBypass, "ex POST /notes '{}'", true, true, "deny", 1},
		{"health execution directory", proto.ModeBypass, "ex POST /notes '{}'", true, false, "deny", 1},
		{"explicit deny", proto.ModeBypass, "ex DELETE /notes/one", true, false, "deny", 0},
		{"credential guard", proto.ModeBypass, "echo $EXAMPLE_TOKEN", true, false, "deny", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := codexTestNode(t)
			fh := withFakeHub(t, n)
			const sid = "11111111-1111-4111-8111-111111111111"
			cfg := olCfg(proto.LarkRule{Method: "DELETE", Path: "/notes/**", Action: "deny"})
			if tc.name == "explicit CLI deny loop" {
				cfg.Rules = []proto.LarkRule{{Method: "CLI", Path: "/im/+messages-send", Action: "deny"}}
			}
			if strings.HasPrefix(tc.name, "proxy write") {
				cfg.Gates[0].Auth = &proto.GateAuth{Header: "Authorization", Value: "Bearer ${EXAMPLE_TOKEN}"}
			}
			if tc.name != "incident without risk metadata" {
				cfg.CLI = &proto.LarkCLI{Bin: "/nonexistent/lark-cli"}
			}
			n.larkRisks = map[string]string{"\x00docs +fetch": "read", "\x00docs +create": "write"}
			n.mu.Lock()
			n.larkCfg, n.larkCfgAt = cfg, time.Now().Add(time.Hour)
			if tc.assistant {
				n.procs[sid] = &assistantProc{id: sid, agent: "claude", exited: make(chan struct{})}
			}
			n.mu.Unlock()
			n.persistModes(sid, tc.mode, "")
			if tc.health {
				n.gates().markSpace(sid)
			}
			fh.mu.Lock()
			fh.answer = func(proto.Interaction) proto.Resolution {
				no := false
				return proto.Resolution{Allow: &no}
			}
			fh.mu.Unlock()
			cwd := t.TempDir()
			if tc.name == "health execution directory" {
				cwd = "/home/me/.persona/tracks/qa-private"
			}
			decision, reason, err := n.larkCheck(context.Background(), sid, cwd, tc.command,
				proto.Interaction{SessionID: sid, Cwd: "/home/me/.persona/assistant", ExpiresAt: time.Now().Add(time.Minute)})
			if err != nil || decision != tc.want || len(fh.interactions()) != tc.cards {
				t.Fatalf("decision=%q reason=%q err=%v cards=%d, want %q/%d", decision, reason, err, len(fh.interactions()), tc.want, tc.cards)
			}
			if tc.assistant && tc.mode == proto.ModeBypass && tc.cards > 0 {
				for _, card := range fh.interactions() {
					if len(card.AlwaysKeys) != 0 {
						t.Fatalf("hard boundary offers Always: %v", card.AlwaysKeys)
					}
				}
			}
			if tc.cards == 0 && decision == "allow" && strings.Contains(reason, "手机") {
				t.Fatalf("claimed a phone answer without a card: %q", reason)
			}
		})
	}
}

func TestAssistantBypassPreservesRuntimeSelection(t *testing.T) {
	n, hub := runtimeNode(t)
	assistantHub(hub)
	host := dialRuntime(t, n, assistantRuntimeHello())
	sid := startedAssistant(t, n, host)
	for _, mode := range []string{"plan", ""} {
		if _, err := nodeCall(t, n, "assistant.send", map[string]any{"session_id": sid, "text": "next", "permission_mode": mode}); err != nil {
			t.Fatal(err)
		}
	}
	sends := host.got("session.send")
	var last struct{ Mode string }
	if err := json.Unmarshal(sends[len(sends)-1], &last); err != nil || last.Mode != "plan" {
		t.Fatalf("runtime selection overwritten: %s, %v", sends[len(sends)-1], err)
	}
	n.mu.Lock()
	delete(n.modes, sid)
	n.mu.Unlock()
	if _, err := nodeCall(t, n, "assistant.send", map[string]any{"session_id": sid, "text": "after restart"}); err != nil {
		t.Fatal(err)
	}
	sends = host.got("session.send")
	if err := json.Unmarshal(sends[len(sends)-1], &last); err != nil || last.Mode != "plan" {
		t.Fatalf("runtime selection lost after memory reset: %s, %v", sends[len(sends)-1], err)
	}
}

func TestAssistantBypassResumePersistsDefault(t *testing.T) {
	fakeCodexHome(t, testThread)
	n := codexTestNode(t)
	withFakeHub(t, n)
	cwd := t.TempDir()
	n.state.put(testThread, sessionState{Launcher: "codex", Cwd: cwd})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	defer n.stopAtConnectionEnd([]string{testThread}, nil)
	if _, err := n.assistantSend(ctx, testThread, "", "", "next", nil, "", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := n.state.get(testThread)
	if st.AppMode != proto.ModeBypass {
		t.Fatalf("resumed AppMode = %q, want bypassPermissions", st.AppMode)
	}
}

func TestAssistantBypassGateReportsPermissionMode(t *testing.T) {
	api := newFakeAPI(t)
	n, hub, _ := ledgerNode(t, api.URL)
	n.state = newState()
	const sid = "11111111-1111-4111-8111-111111111111"
	n.mu.Lock()
	n.procs = map[string]*assistantProc{sid: {id: sid, agent: "claude", exited: make(chan struct{})}}
	n.modes = map[string]sessionModes{}
	n.mu.Unlock()
	n.noteModes(sid, proto.ModeBypass, "")
	base, release, err := n.gates().open("ex", sid)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	decision, reason, err := n.larkCheck(context.Background(), sid, t.TempDir(), `ex POST /notes/example '{"text":"example"}'`, proto.Interaction{SessionID: sid})
	if err != nil || decision != "allow" {
		t.Fatalf("gate = %s %s %v", decision, reason, err)
	}
	if code, _ := send(t, "POST", base+"/api/v1/notes/example", `{"text":"example"}`); code != http.StatusOK {
		t.Fatalf("proxy status = %d", code)
	}
	reports := hub.reportList(t, 1)
	if reports[0].SessionID != sid || reports[0].LetThrough != (proto.LarkLetThrough{Kind: proto.LarkThroughBypass}) {
		t.Fatalf("report = %+v", reports[0])
	}
	if code, _ := send(t, "POST", base+"/api/v1/notes/example", `{"text":"example"}`); code != http.StatusForbidden {
		t.Fatalf("reused grant status = %d", code)
	}
}

func TestAssistantBypassCodexUnknownCommand(t *testing.T) {
	n := codexTestNode(t)
	fh := withFakeHub(t, n)
	cwd := t.TempDir()
	p, c, f := newFakeCodex(t, n, cwd)
	p.id = testThread
	c.threadID = testThread
	n.procs[testThread] = p
	n.larkCfg, n.larkCfgAt = olCfg(), time.Now().Add(time.Hour)
	n.noteModes(testThread, proto.ModeBypass, "")
	fh.answer = func(proto.Interaction) proto.Resolution { no := false; return proto.Resolution{Allow: &no} }
	res := f.request(1, "item/commandExecution/requestApproval", map[string]any{"threadId": testThread, "itemId": "call_unknown", "command": `C=lark-cli; $C im +messages-send --text hello`, "cwd": cwd})
	t.Logf("classification only: Codex decision=%v cards=%d; want decline/1", res["decision"], len(fh.interactions()))
	if res["decision"] != "decline" || len(fh.interactions()) != 1 {
		t.Fatalf("result=%v cards=%d, want decline/1", res, len(fh.interactions()))
	}
}

// Only the classification and approval adapters see these strings; no command is executed.
func TestAssistantBypassOrdinaryCommands(t *testing.T) {
	for _, command := range []string{
		"python3 -c 'print(1)'",
		"awk '{print}'",
		"sed -n '1p'",
		"find . -type f",
		"eval true",
		"printf hi | xargs echo",
		"source ~/.bashrc",
		". ~/.bashrc",
		"env",
		"exec true",
		"nice true",
		"sudo -n true",
		"busybox true",
		"ssh -G example.com",
		"nohup true",
		"echo $(date +%F)",
		"echo `date`",
		"echo {a,b}",
		"python3.12 -c 'print(1)'",
		"./send.sh",
		"/opt/bin/echo hi",
		"/usr/local/bin/echo hi",
		"~/bin/echo hi",
		"git status; python3 -c 'print(1)'",
		"echo $(date)",
		"time true",
		"builtin true",
		"sh -c 'echo hi'",
		"bash -c 'echo hi'",
		"echo $HOME",
		"ls *.md",
		"/opt/bin/program hi",
		"./program.py",
		"./program.js",
		`sh -c 'echo {a,b}'`,
		`env FOO=1 go test ./...`,
		`echo $(printf hi)`,
		`echo $(echo {a,b})`,
		`cat <<< "$HOME"`,
		`python3 -c 'print("; $C")' <<< input`,
		"cat <<'EOF'\n$C im +messages-send\nEOF",
		`(echo "$HOME")`,
	} {
		t.Run(command, func(t *testing.T) { assertAssistantBypassClassification(t, command, 0) })
	}
}

// Sanitized shapes of read-only commands observed after deployment. These strings
// reach only the two classification adapters; none is executed by a shell.
func TestAssistantBypassReadOnlyCompoundCommands(t *testing.T) {
	for _, tc := range []struct {
		name, command string
	}{
		{"document fetch redirects outside tmp", `NO_COLOR=1 NO_PROGRESS=1 lark-cli docs +fetch --as user --doc example > /home/me/output/doc.json && jq '{top_keys: keys, data_keys: (.data | keys)}' /home/me/output/doc.json`},
		{"document fetch redirect traverses tmp", `NO_COLOR=1 lark-cli docs +fetch --as user --doc example > /tmp/../home/me/output/doc.json && jq '.data' /home/me/output/doc.json`},
		{"document fetch redirect uses a variable", `DEST=/home/me/output; lark-cli docs +fetch --doc example > "$DEST/doc.json" && jq '.data' "$DEST/doc.json"`},
		{"document fetch redirect uses only a variable", `lark-cli docs +fetch > "$DEST"`},
		{"document fetch redirect runs a literal formatter", `lark-cli docs +fetch > "/home/me/$(printf doc).json"`},
		{"non-Lark loop pipes into jq", `for n in 1 2; do example-cli --json review status "$n" -R example/project | jq -c --arg n "$n" '{mr:$n,data:(.data | {status:.merge_request.status,head:.merge_request.source_commit_id,merge:.merge_request.merge_commit_id,failed:.mergeability.failed_checks,review:.review.status})}'; done; `},
		{"non-Lark loop uses command substitution in an assignment", `for n in 1 2; do status=$(example-cli --json review status "$n" -R example/project | jq -r '.review.status'); printf '%s\n' "$status"; done`},
		{"literal shell waits with command substitution assignments", `timeout 90 bash -c 'until f=$(ls -t ~/.claude/projects/*/example-session.jsonl 2>/dev/null | head -1); [ -n "$f" ] && grep -q "\"type\":\"assistant\"" "$f"; do sleep 3; done; echo "$f"'; f=$(ls -t ~/.claude/projects/*/example-session.jsonl 2>/dev/null | head -1); printf '%s\n' "$f"`},
		{"literal shell until loop has no CLI call", `bash -c 'until f=$(ls -t ~/example/*.jsonl 2>/dev/null | head -1); [ -n "$f" ]; do sleep 1; done; echo "$f"'`},
		{"literal shell for loop has no CLI call", `bash -c 'for n in 1 2; do status=$(printf '%s\n' "$n" | head -1); printf '%s\n' "$status"; done'`},
		{"literal shell test brackets are not expansions", `bash -c 'for n in 1 2; do [[ -n "$n" ]] && printf '%s\n' "$n"; done'`},
	} {
		t.Run(tc.name, func(t *testing.T) { assertAssistantBypassClassification(t, tc.command, 0) })
	}
}

func TestAssistantBypassCLIControls(t *testing.T) {
	for _, command := range []string{
		`python3 -c "import os; os.system('lark-cli im +messages-send --text hello')"`,
		`python3.12 -c "import os; os.system('/usr/local/bin/lark-cli im +messages-send --text hello')"`,
		`awk 'BEGIN {system("lark-cli im +messages-send --text hello")}'`,
		`sed -e 'e lark-cli im +messages-send --text hello'`,
		`find . -exec lark-cli im +messages-send --text hello \;`,
		`printf hi | xargs lark-cli im +messages-send --text hello`,
		`env lark-cli im +messages-send --text hello`,
		`exec lark-cli im +messages-send --text hello`,
		`sudo -n lark-cli im +messages-send --text hello`,
		`nice -n 5 lark-cli im +messages-send --text hello`,
		`nohup lark-cli im +messages-send --text hello`,
		`ssh example.com lark-cli im +messages-send --text hello`,
		`eval 'lark-cli im +messages-send --text hello'`,
		`source ./program.sh lark-cli`,
		`sh -c 'lark-cli im +messages-send --text hello'`,
		`/opt/bin/program lark-cli im +messages-send --text hello`,
		`/usr/local/bin/lark-cli im +messages-send --text hello`,
		`./program.sh lark-cli`,
		`echo $(lark-cli im +messages-send --text hello)`,
		`echo {a,b}; lark-cli im +messages-send --text hello`,
		`$C im +messages-send --text hello`,
		`$(printf '%s' "$C") im +messages-send --text hello`,
		"`printf '%s' \"$C\"` im +messages-send --text hello",
		`lark-cli docs +create --title "$DOCUMENT"`, // a read with a variable flag value runs (#8119); a write is confirmed whole
		`lark-cli im +messages-send --text hello < /home/me/message.txt`,
		`echo hi > "/tmp/$(lark-cli im +messages-send --text hello)"`,
		`$C im +messages-send --text hello <<< input`,
		`echo {a,b}; $C im +messages-send --text hello`,
		`$(printf '%s' "$C") im +messages-send --text hello <<< input`,
		`lark\-cli im +messages-send --text hello <<< input`,
		`(true); "$C" im +messages-send --text hello`,
		"$C im +messages-send --text hello\n)",
		"lark\\-cli im +messages-send --text hello\n)",
	} {
		t.Run(command, func(t *testing.T) { assertAssistantBypassClassification(t, command, 1) })
	}
}

func assertAssistantBypassClassification(t *testing.T, command string, cards int, gates ...proto.CommandGate) {
	t.Helper()

	for _, adapter := range []string{"claude", "codex"} {
		t.Run(adapter, func(t *testing.T) {
			n := codexTestNode(t)
			fh := withFakeHub(t, n)
			cwd := t.TempDir()
			p, c, f := newFakeCodex(t, n, cwd)
			p.id, c.threadID = testThread, testThread
			n.procs[testThread] = p
			n.larkCfg, n.larkCfgAt = olCfg(), time.Now().Add(time.Hour)
			if len(gates) > 0 {
				n.larkCfg.Gates = gates
			}
			n.larkCfg.CLI = &proto.LarkCLI{Bin: "/nonexistent/lark-cli"}
			// Seed metadata without starting the CLI or executing the classified commands.
			n.larkRisks = map[string]string{"\x00docs +fetch": "read", "\x00docs +create": "write", "\x00im messages get": larkCLIMissing}
			// The login shell's PATH: what the assistant's Bash resolves programs in, which this process's own PATH
			// may lack (#12483). No shell is asked in tests.
			n.larkPATH = []string{"/opt/login/bin"}
			n.larkPATHAt = time.Now()
			n.noteModes(testThread, proto.ModeBypass, "")
			fh.answer = func(proto.Interaction) proto.Resolution { no := false; return proto.Resolution{Allow: &no} }
			decision, want := "", "allow"
			if cards > 0 {
				want = "deny"
			}
			if adapter == "claude" {
				raw, _ := json.Marshal(map[string]string{"command": command})
				out, err := n.larkGate(context.Background(), hookInput{SessionID: testThread, Cwd: cwd, ToolInput: raw}, proto.Interaction{SessionID: testThread, ExpiresAt: time.Now().Add(time.Minute)})
				if err != nil {
					t.Fatal(err)
				}
				if out == nil {
					// The empty gate decision falls through to this conversation's bypass mode.
					mode, _ := n.modesOf(testThread)
					if mode != proto.ModeBypass {
						t.Fatalf("fallback mode=%q", mode)
					}
					decision = "allow"
				} else {
					decision, _ = out.(map[string]any)["hookSpecificOutput"].(map[string]any)["permissionDecision"].(string)
				}
			} else {
				want = "accept"
				if cards > 0 {
					want = "decline"
				}
				res := f.request(1, "item/commandExecution/requestApproval", map[string]any{"threadId": testThread, "itemId": "call_ordinary", "command": command, "cwd": cwd})
				decision, _ = res["decision"].(string)
			}
			t.Logf("classification only: %s decision=%s cards=%d; want %s/%d", adapter, decision, len(fh.interactions()), want, cards)
			if decision != want || len(fh.interactions()) != cards {
				t.Fatalf("decision=%s cards=%d; want %s/%d", decision, len(fh.interactions()), want, cards)
			}
		})
	}
}

func TestAssistantBypassRespectsConfiguredAllowRules(t *testing.T) {
	for _, tc := range []struct {
		name      string
		command   string
		gateRules []proto.LarkRule
		cliRules  []proto.LarkRule
		health    bool
		healthCwd bool
		want      string
		cards     int
	}{
		{
			name:    "configured conversation send allow",
			command: `ex POST /conversations/allowed_chat/messages '{"text":"hi"}'`,
			gateRules: []proto.LarkRule{
				{Method: "POST", Path: "/conversations/allowed_chat/messages", Action: "allow"},
			},
			want:  "allow",
			cards: 0,
		},
		{
			name:    "configured message replies allow",
			command: `ex POST /messages/msg_123/replies '{"text":"reply"}'`,
			gateRules: []proto.LarkRule{
				{Method: "POST", Path: "/messages/*/replies", Action: "allow"},
			},
			want:  "allow",
			cards: 0,
		},
		{
			name:    "configured CLI send allow",
			command: `lark-cli im +messages-send --as user --text hello`,
			cliRules: []proto.LarkRule{
				{Method: "CLI", Path: "/im/+messages-send", Action: "allow"},
			},
			want:  "allow",
			cards: 0,
		},
		{
			name:    "unlisted conversation still confirms",
			command: `ex POST /conversations/other_chat/messages '{"text":"hi"}'`,
			gateRules: []proto.LarkRule{
				{Method: "POST", Path: "/conversations/allowed_chat/messages", Action: "allow"},
			},
			want:  "deny",
			cards: 1,
		},
		{
			name:    "unlisted deletion still confirms",
			command: `ex POST /messages/msg_123/recall '{}'`,
			gateRules: []proto.LarkRule{
				{Method: "POST", Path: "/messages/*/replies", Action: "allow"},
			},
			want:  "deny",
			cards: 1,
		},
		{
			name:    "health session overrides configured allow",
			command: `ex POST /conversations/allowed_chat/messages '{"text":"hi"}'`,
			gateRules: []proto.LarkRule{
				{Method: "POST", Path: "/conversations/allowed_chat/messages", Action: "allow"},
			},
			health: true,
			want:   "deny",
			cards:  1,
		},
		{
			name:    "health execution directory overrides configured allow",
			command: `ex POST /conversations/allowed_chat/messages '{"text":"hi"}'`,
			gateRules: []proto.LarkRule{
				{Method: "POST", Path: "/conversations/allowed_chat/messages", Action: "allow"},
			},
			healthCwd: true,
			want:      "deny",
			cards:     1,
		},
		{
			name:    "explicit deny before allow still denies without card",
			command: `ex POST /conversations/allowed_chat/messages '{"text":"hi"}'`,
			gateRules: []proto.LarkRule{
				{Method: "POST", Path: "/conversations/allowed_chat/messages", Action: "deny"},
				{Method: "POST", Path: "/conversations/*/messages", Action: "allow"},
			},
			want:  "deny",
			cards: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := codexTestNode(t)
			fh := withFakeHub(t, n)
			const sid = "22222222-2222-4222-8222-222222222222"
			cfg := olCfg(tc.gateRules...)
			cfg.Rules = tc.cliRules
			cfg.CLI = &proto.LarkCLI{Bin: "/nonexistent/lark-cli"}
			n.mu.Lock()
			n.larkCfg, n.larkCfgAt = cfg, time.Now().Add(time.Hour)
			n.procs[sid] = &assistantProc{id: sid, agent: "claude", exited: make(chan struct{})}
			n.mu.Unlock()
			n.persistModes(sid, proto.ModeBypass, "")
			if tc.health {
				n.gates().markSpace(sid)
			}
			fh.mu.Lock()
			fh.answer = func(proto.Interaction) proto.Resolution {
				no := false
				return proto.Resolution{Allow: &no}
			}
			fh.mu.Unlock()
			cwd := t.TempDir()
			if tc.healthCwd {
				cwd = "/home/me/.persona/tracks/qa-private"
			}
			decision, reason, err := n.larkCheck(context.Background(), sid, cwd, tc.command,
				proto.Interaction{SessionID: sid, Cwd: "/home/me/.persona/assistant", ExpiresAt: time.Now().Add(time.Minute)})
			if err != nil || decision != tc.want || len(fh.interactions()) != tc.cards {
				t.Fatalf("decision=%q reason=%q err=%v cards=%d, want %q/%d", decision, reason, err, len(fh.interactions()), tc.want, tc.cards)
			}
			if tc.cards > 0 {
				for _, card := range fh.interactions() {
					if len(card.AlwaysKeys) != 0 {
						t.Fatalf("hard boundary offers Always: %v", card.AlwaysKeys)
					}
				}
			}
		})
	}
}
