package slashcommands

import (
	"bytes"
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestACPReportsOnlyCommandMetadata(t *testing.T) {
	t.Parallel()
	var sent bytes.Buffer
	stream := strings.NewReader(`{"id":1,"result":{"protocolVersion":1}}
{"method":"session/update","params":{"update":{"sessionUpdate":"usage_update"}}}
{"method":"session/update","params":{"update":{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"compact","description":"Make space"},{"name":"workspace-task","description":"First line\nsecond line","path":"/example/private"}]}}}`)
	rows, err := acpCommands(&sent, stream, "/example/project")
	if err != nil {
		t.Fatal(err)
	}
	if requests := sent.String(); !strings.Contains(requests, "session/new") || strings.Contains(requests, "session/prompt") {
		t.Fatalf("unexpected ACP request %q", requests)
	}
	got := normalize(builtinFirst("agy", rows), label(rows, nil, "agy"))
	if !slices.Contains(got, proto.SlashCommand{Name: "/workspace-task", Source: proto.SlashSkill, Description: "First line second line"}) {
		t.Fatalf("workspace command missing from %v", got)
	}
	if slices.ContainsFunc(got, func(row proto.SlashCommand) bool { return row.Name == "/boost" }) {
		t.Fatal("advertised an ACP command missing from the CLI response")
	}
}

func TestClaudeMatchesItsInitializeResponse(t *testing.T) {
	t.Parallel()
	var sent bytes.Buffer
	wire := strings.NewReader(`{"type":"control_response","response":{"request_id":"someone-else","response":{"commands":[{"name":"wrong"}]}}}
{"type":"control_response","response":{"request_id":"persona-slash","response":{"commands":[{"name":"test-skill","description":"A test action"}],"slash_commands":["status"]}}}`)
	rows, err := claudeCommands(&sent, wire)
	if err != nil || len(rows) != 2 || rows[0].Name != "test-skill" || rows[1].Name != "status" {
		t.Fatalf("command catalog = %v, %v", rows, err)
	}
	if !strings.Contains(sent.String(), `"subtype":"initialize"`) {
		t.Fatalf("initialize not sent: %q", sent.String())
	}
}

func TestCodexFiltersTheFeaturesDisabledByItsCLI(t *testing.T) {
	t.Parallel()
	rows := codexCommands("apps stable false\nplugins stable true\ngoals stable false\ntui_workspace stable true")
	has := func(name string) bool {
		return slices.ContainsFunc(rows, func(row proto.SlashCommand) bool { return row.Name == name })
	}
	if !has("status") || !has("plugins") || !has("worktree") || has("apps") || has("goal") || has("plan") {
		t.Fatalf("unexpected Codex catalog %v", rows)
	}
}

func TestCacheIsolatesTargetsAndExpires(t *testing.T) {
	t.Parallel()
	now := time.Unix(100, 0)
	catalog := New(func() time.Time { return now })
	seen := 0
	fail := func(context.Context, ...string) (*exec.Cmd, error) {
		seen++
		return nil, ErrTarget
	}
	list := func(cwd string) []proto.SlashCommand {
		return catalog.List(context.Background(), Target{Agent: "codex", Launcher: "example", Cwd: cwd}, fail, nil)
	}
	for _, cwd := range []string{"/example/a", "/example/a", "/example/b"} {
		if rows := list(cwd); len(rows) != 0 {
			t.Fatalf("failed probe gave %v", rows)
		}
	}
	if seen != 2 {
		t.Fatalf("same directory started %d probes across two targets", seen)
	}
	now = now.Add(retryIn)
	_ = list("/example/a")
	if seen != 3 {
		t.Fatalf("expired cache did not refresh: %d probes", seen)
	}
}
