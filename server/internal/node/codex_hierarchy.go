package node

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/gitworktree"
	"github.com/nwverse/persona/server/internal/node/harness/codex"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

type projectRootClock struct{}

func (projectRootClock) Now() time.Time { return time.Now() }

func (projectRootClock) WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

func (n *Node) projectRoot(cwd string, pass map[string]string) string {
	n.projectRootsOnce.Do(func() {
		n.projectRoots = gitworktree.NewProjectRoots(git, projectRootClock{})
	})
	return n.projectRoots.Resolve(cwd, pass)
}

// decorateCodexSessionIn is decorateCodexSession with the App's state looked up once per home in a list pass
// (codexListPass).
func (n *Node) decorateCodexSessionIn(s *proto.Session, src codexSource, legacy string, projects map[string]string, pass *codexListPass) {
	origin := codexThreadOrigin(src) // a rollout's first line, else the App's database row (#8191)
	s.ParentThreadID = firstNonEmpty(s.ParentThreadID, origin.Parent)
	s.IsSubagent = origin.Subagent || s.ParentThreadID != ""
	if s.ParentThreadID != "" {
		s.ParentSessionID = codex.SourceID(src.home, s.ParentThreadID, legacy)
	}
	if s.IsSubagent && origin.Root != "" && origin.Root != src.thread {
		s.RootSessionID = codex.SourceID(src.home, origin.Root, legacy)
	}
	if s.IsSubagent {
		s.Capabilities.Send, s.Capabilities.Attach, s.Capabilities.Interrupt = false, false, false
		s.Capabilities.Rewind = proto.CanModes(false)
	}
	if origin.Started != nil && s.Hierarchy == nil {
		s.Hierarchy = origin.Started // when the thread began, read with its origin: no read of its own (#5120)
	}
	if !origin.Desktop {
		// An App chat whose rollout is gone is grouped as one with a rollout (codex_appdb.go).
		if _, app := codexAppThreadOf(src); !app {
			return
		}
	}
	s.AgentName, s.AgentSurface = "Codex App", transcript.SurfaceDesktop
	app := pass.appState(src.home)
	s.SpaceKind = "codex_app"
	chat := src.thread
	if s.IsSubagent {
		chat = firstNonEmpty(origin.Root, s.ParentThreadID, chat)
	}
	cwd := s.Cwd
	if pid, _ := app.project(chat, cwd); pid != "" {
		project := app.projects[pid]
		if len(project.RootPaths) == 1 && filepath.IsAbs(project.RootPaths[0]) {
			cwd = project.RootPaths[0]
		}
	}
	s.RepoRoot = n.projectRoot(cwd, projects)
	if app != nil && !app.inNoProject(chat) {
		if pid := app.assigned[chat]; pid != "" {
			if p, ok := app.projects[pid]; ok {
				s.SpaceID = codexDesktopProjectID(src.home, pid)
				s.SpaceName = strings.TrimSpace(p.Name)
				if s.SpaceName == "" && s.RepoRoot != "" {
					s.SpaceName = filepath.Base(s.RepoRoot)
				}
				s.SpaceName = firstNonEmpty(s.SpaceName, "Codex project")
				s.SpaceIndex = int32(slices.Index(app.order, pid) + 1)
				return
			}
		}
	}
	// A chat the App keeps in no project (projectless-thread-ids) is under "Codex chats", as the App's sidebar lists it
	// and placeCodexDesktop placed it, whatever folder the App made for it (~/Documents/Codex/<date>/<name>): by its
	// folder it was a project of its own. A subagent goes by its chat: resolveCodexHierarchy gives it its chat's place,
	// but not when the chat is not listed (archived in the App, or read from its database without sqlite3).
	// A project is named as the App names it when its folder is the repository's root (folderName), else by the folder,
	// and numbered by its place in the App's sidebar (space_index, folderPlace; 0, none, for any other).
	if s.RepoRoot != "" && !app.inNoProject(chat) {
		s.SpaceID, s.SpaceName = projectSpace+s.RepoRoot, firstNonEmpty(app.folderName(s.RepoRoot), filepath.Base(s.RepoRoot))
		s.SpaceIndex = int32(app.folderPlace(s.RepoRoot))
	} else {
		s.SpaceID, s.SpaceName = "codex-chats:"+src.home, "Codex 对话"
	}
}

// projectSpace prefixes the space of a repository's desktop-app sessions on a computer, the root following it: the Codex
// App's chats in a project (decorateCodexSession) and the Claude desktop app's sessions (placeClaudeDesktop) alike, so
// both apps' sessions of a repository are one project. The name is the Codex App's from before Claude's were placed:
// pins and clients' remembered sections name spaces by it.
const projectSpace = "codex-project:"

// programRun: a one-shot run a program started (`codex exec`, an SDK; an evaluation opens dozens at once, #2131),
// known only from its rollout: not one of the user's agents. A thread a shared app-server holds is drivable and stays,
// as does a run the Codex App lists in its sidebar.
func (n *Node) programRun(id string, src codexSource, pass *codexListPass) bool {
	if src.path == "" || !readCodexOrigin(src.path).Exec {
		return false
	}
	if _, _, err := n.codexOwner(id); err != errCodexNoOwner {
		return false
	}
	return !codexDesktopChatIn(src, pass)
}
