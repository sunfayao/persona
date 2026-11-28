package slashcommands

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/osproc"
	"github.com/sunfayao/persona/server/internal/proto"
)

func discover(ctx context.Context, target Target, command Command, local Skills) []proto.SlashCommand {
	switch target.Agent {
	case "codex":
		cmd, err := command(ctx, "features", "list")
		if err == nil {
			if out, err := cmd.Output(); err == nil {
				return normalize(codexCommands(string(out)))
			}
		}
	case "claude":
		version, err := command(ctx, "--version")
		if err != nil {
			break
		}
		out, err := version.Output()
		if err != nil || !strings.Contains(string(out), "Claude Code") {
			break
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		cmd, err := command(probeCtx, "-p", "--input-format", "stream-json", "--output-format", "stream-json",
			"--verbose", "--no-session-persistence", "--strict-mcp-config", "--mcp-config", "{}", "--settings", `{"disableAllHooks":true}`)
		var native []proto.SlashCommand
		if err == nil {
			native, _ = exchange(probeCtx, cmd, claudeCommands)
		}
		cancel()
		files := local(ctx)
		if native != nil {
			return normalize(builtins("claude"), label(native, files, "claude"))
		}
		// Only the documented TUI rows and the launcher's visible local files are available here.
		return normalize(builtins("claude"), files)
	case "agy", "traex":
		args := []string{"acp"}
		if target.Agent == "traex" {
			args = append(args, "serve")
		}
		cmd, err := command(ctx, args...)
		if err == nil {
			rows, err := exchange(ctx, cmd, func(w io.Writer, r io.Reader) ([]proto.SlashCommand, error) {
				return acpCommands(w, r, target.Cwd)
			})
			if err == nil {
				return normalize(builtinFirst(target.Agent, rows), label(rows, local(ctx), target.Agent))
			}
		}
	}
	return []proto.SlashCommand{}
}

// Codex exposes effective feature values through its own config-aware CLI.
func codexCommands(features string) []proto.SlashCommand {
	enabled := map[string]bool{}
	for line := range strings.SplitSeq(features, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			enabled[fields[0]] = fields[len(fields)-1] == "true"
		}
	}
	if len(enabled) == 0 {
		return nil
	}
	gates := map[string]string{"plan": "collaboration_modes", "apps": "apps", "plugins": "plugins",
		"goal": "goals", "worktree": "tui_workspace"}
	var rows []proto.SlashCommand
	for _, row := range builtins("codex") {
		if feature := gates[row.Name]; feature == "" || enabled[feature] {
			rows = append(rows, row)
		}
	}
	return rows
}

// ACP can omit terminal-only commands. Agent/workspace commands exposed through ACP
// still need the CLI's own availability decision.
func builtinFirst(agent string, native []proto.SlashCommand) []proto.SlashCommand {
	reported := map[string]bool{}
	for _, row := range native {
		reported[row.Name] = true
	}
	var rows []proto.SlashCommand
	for _, row := range builtins(agent) {
		switch row.Name {
		case "goal", "compact", "schedule", "browser", "plan", "grill-me", "teamwork", "learn", "boost":
			if !reported[row.Name] {
				continue
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func label(rows, local []proto.SlashCommand, agent string) []proto.SlashCommand {
	kinds := map[string]string{}
	for _, row := range local {
		kinds[strings.TrimPrefix(row.Name, "/")] = row.Source
	}
	for i := range rows {
		kind := kinds[rows[i].Name]
		if kind == "" {
			kind = proto.SlashSkill
			if agent == "claude" {
				kind = proto.SlashCustom
			}
		}
		rows[i].Source = kind
	}
	return rows
}

func exchange(ctx context.Context, cmd *exec.Cmd, read func(io.Writer, io.Reader) ([]proto.SlashCommand, error)) ([]proto.SlashCommand, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = osproc.StopGroup(cmd.Process.Pid, true)
		_ = cmd.Wait()
	}()
	closeReader := context.AfterFunc(ctx, func() { _ = out.Close() })
	defer closeReader()
	return read(in, out)
}

func scan(r io.Reader, receive func([]byte) (bool, []proto.SlashCommand)) ([]proto.SlashCommand, error) {
	lines := bufio.NewScanner(io.LimitReader(r, 4<<20))
	lines.Buffer(make([]byte, 16384), 1<<20)
	for lines.Scan() {
		if done, rows := receive(lines.Bytes()); done {
			return rows, nil
		}
	}
	if err := lines.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("CLI returned no slash command catalog")
}

func claudeCommands(w io.Writer, r io.Reader) ([]proto.SlashCommand, error) {
	const requestID = "persona-slash"
	if _, err := io.WriteString(w, `{"type":"control_request","request_id":"`+requestID+`","request":{"subtype":"initialize"}}`+"\n"); err != nil {
		return nil, err
	}
	return scan(r, func(line []byte) (bool, []proto.SlashCommand) {
		var msg struct {
			Type     string `json:"type"`
			Response struct {
				ID   string `json:"request_id"`
				Body struct {
					Commands []proto.SlashCommand `json:"commands"`
					Names    []string             `json:"slash_commands"`
				} `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal(line, &msg) != nil {
			return false, nil
		}
		if msg.Type == "control_response" && msg.Response.ID == requestID {
			rows := msg.Response.Body.Commands
			for _, name := range msg.Response.Body.Names {
				rows = append(rows, proto.SlashCommand{Name: name})
			}
			return true, rows
		}
		return false, nil
	})
}

func acpCommands(w io.Writer, r io.Reader, cwd string) ([]proto.SlashCommand, error) {
	enc := json.NewEncoder(w)
	request := func(id int, method string, params map[string]any) error {
		return enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	}
	if err := request(1, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}); err != nil {
		return nil, err
	}
	if err := request(2, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}}); err != nil {
		return nil, err
	}
	rows, err := scan(r, func(line []byte) (bool, []proto.SlashCommand) {
		var msg struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Error  json.RawMessage `json:"error"`
			Params struct {
				Update struct {
					Kind     string               `json:"sessionUpdate"`
					Commands []proto.SlashCommand `json:"availableCommands"`
				} `json:"update"`
			} `json:"params"`
		}
		if json.Unmarshal(line, &msg) != nil {
			return false, nil
		}
		if (msg.ID == 1 || msg.ID == 2) && len(msg.Error) > 0 {
			return true, nil
		}
		return msg.Method == "session/update" && msg.Params.Update.Kind == "available_commands_update", msg.Params.Update.Commands
	})
	if rows == nil && err == nil {
		err = errors.New("ACP command discovery did not complete")
	}
	return rows, err
}
