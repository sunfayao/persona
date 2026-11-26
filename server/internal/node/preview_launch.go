package node

// The dev servers a preview can show (docs/web/preview.md "Servers"): the entries of the project's .claude/launch.json, in
// the shape Claude Desktop reads (configurations[]: name, runtimeExecutable, runtimeArgs, port, optional url, cwd and
// env), each with whether something listens on its port now. preview.start runs one of them in a new terminal tab.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/host"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/node/preview"
	"github.com/nwverse/persona/server/internal/portprobe"
)

type previewServer = preview.Server
type previewServers = preview.Servers

// previewServersRPC is preview.servers: launch.json's entries for a session's folder, probed.
func (n *Node) previewServersRPC(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Cwd string `json:"cwd"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Cwd == "" || !filepath.IsAbs(p.Cwd) {
		return nil, errors.New("preview.servers: need an absolute cwd")
	}
	res := previewServers{Servers: []previewServer{}}
	res.Browser = n.previewHost().BrowserName()
	res.File = n.previewHost().FindLaunchFile(filepath.Clean(p.Cwd))
	if res.File == "" {
		return res, nil
	}
	servers, err := preview.ReadLaunchFile(res.File)
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	pctx, cancel := context.WithTimeout(ctx, portprobe.Wait+time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := range servers {
		if servers[i].Problem != "" {
			continue
		}
		wg.Add(1)
		go func(s *previewServer) {
			defer wg.Done()
			pr := portprobe.Probe(pctx, s.Port, "", time.Now)
			s.Listening, s.Title = pr.HTTP || pr.Title != "", pr.Title
			if !s.Listening { // something that is not HTTP (yet): it listens all the same
				if c, err := portprobe.Dial(pctx, s.Port, ""); err == nil {
					c.Close()
					s.Listening = true
				}
			}
		}(&servers[i])
	}
	wg.Wait()
	sort.SliceStable(servers, func(i, j int) bool { return servers[i].Problem == "" && servers[j].Problem != "" })
	res.Servers = servers
	return res, nil
}

// previewStartRPC is preview.start: run a launch.json entry in a new terminal tab of the session's workspace, named
// after it, so its output is there to read and Ctrl-C stops it. The panel showed the command and the user confirmed
// it; the node runs the entry as the file says now, the program and its arguments each quoted (no shell string).
func (n *Node) previewStartRPC(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Cwd       string `json:"cwd"`
		Name      string `json:"name"`
		Workspace string `json:"workspace"`
		Kind      string `json:"kind"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Cwd == "" || p.Name == "" || p.Workspace == "" {
		return nil, errors.New("preview.start: need cwd, name and workspace")
	}
	runtimeSpace := n.isRuntimeWorkspace(p.Workspace)
	file := n.previewHost().FindLaunchFile(filepath.Clean(p.Cwd))
	if file == "" {
		return nil, fmt.Errorf("no %s for %s", preview.LaunchFile, p.Cwd)
	}
	servers, err := preview.ReadLaunchFile(file)
	if err != nil {
		return nil, err
	}
	var s *previewServer
	for i := range servers {
		if servers[i].Name == p.Name {
			s = &servers[i]
			break
		}
	}
	switch {
	case s == nil:
		return nil, fmt.Errorf("%s has no entry %q", preview.LaunchFile, p.Name)
	case s.Problem != "":
		return nil, fmt.Errorf("entry %q cannot be used: %s", p.Name, s.Problem)
	case len(s.Command) == 0:
		return nil, fmt.Errorf("entry %q only opens %s: start its server yourself", p.Name, s.URL)
	}
	if runtimeSpace {
		// A runtime's workspace (a Paseo session's workspace_id) has no terminal: the server runs in the terminal
		// engine's workspace that holds its folder, else in a new workspace there; never in one that only happens to be
		// first.
		if p.Kind, p.Workspace = n.launchSpaceHolding(ctx, s.Cwd); p.Workspace == "" {
			launch, created, err := n.launchSpace(ctx, "", s.Cwd, s.Name)
			switch {
			case errors.Is(err, errNoLauncher):
				return nil, refusal(codePreviewNoTerminal, i18n.T(i18n.Default, "rpc-errors."+codePreviewNoTerminal), nil)
			case err != nil:
				return nil, fmt.Errorf("could not open a terminal tab: %w", err)
			}
			return n.runPreview(ctx, launch, created, s)
		}
	}
	var launch host.Launcher
	for _, h := range n.hosts.All() {
		if l, ok := h.(host.Launcher); ok && (p.Kind == "" || h.Kind() == p.Kind) {
			if _, has := n.spaceOf(h.Kind(), p.Workspace); has {
				launch = l
				break
			}
		}
	}
	if launch == nil {
		return nil, fmt.Errorf("workspace %s is not on a host that can open tabs", p.Workspace)
	}
	created, err := launch.CreateSlot(ctx, p.Workspace, s.Cwd, tabLabel(s.Name, n.tabLabels(ctx, launch, p.Workspace)))
	if err != nil {
		return nil, fmt.Errorf("could not open a terminal tab: %w", err)
	}
	return n.runPreview(ctx, launch, created, s)
}

// codePreviewNoTerminal: a preview's server has nowhere to run, the machine enabling no terminal engine (its words
// are the hub's, in the request's language: rpc-errors.preview_no_terminal).
const codePreviewNoTerminal = "preview_no_terminal"

// runPreview runs entry s in the new tab created and answers with its pane and port.
func (n *Node) runPreview(ctx context.Context, launch host.Launcher, created *host.Created, s *previewServer) (any, error) {
	if created == nil || created.Pane.ID == "" {
		return nil, errors.New("could not open a terminal tab: the host returned no pane")
	}
	time.Sleep(300 * time.Millisecond) // the fresh shell loads its rc files; typed input is buffered either way
	if err := launch.Run(ctx, created.Pane, preview.LaunchCommand(s)); err != nil {
		return nil, fmt.Errorf("could not run %s: %w", s.Command[0], err)
	}
	_ = n.refreshHosts(ctx, launch.(host.Host))
	pane := created.Pane.String()
	return map[string]any{"pane": pane, "terminal": panePrefix + pane, "port": s.Port}, nil
}
