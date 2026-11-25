package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/identity"
	"github.com/nwverse/persona/server/internal/proto"
)

// A migrated Desktop conversation can retain both a rollout and its native index
// row without the legacy sidebar keys. Require both origins for release-only
// continuation. A Desktop-looking rollout alone does not establish ownership.
func codexDesktopContinuationChat(src codexSource, pass *codexListPass) bool {
	if codexDesktopChatIn(src, pass) {
		return true
	}
	origin := readCodexOrigin(src.path)
	if !origin.desktop || origin.exec || origin.sub || origin.parent != "" {
		return false
	}
	row, indexed := codexAppThreads(src.home)[src.thread]
	return indexed && strings.TrimSpace(row.Source) == "vscode"
}

// A released Desktop conversation can explicitly resume on the same-home local app-server.
// It never goes through the stop/confirmation path for a terminal that still owns a thread.
func (n *Node) releasedCodexDesktopRow(src codexSource, s *proto.Session, pass *codexListPass) *proto.CodexTakeover {
	if n.sessionScope != nil || s.IsSubagent || s.Status == proto.StatusWorking || s.Status == proto.StatusBlocked ||
		s.Cwd == "" || codexArchived(src.home, src.path) || !codexDesktopWriterReleased(src) {
		return nil
	}
	if src.path == "" {
		if _, ok := pass.paginatedThreadOf(src); !ok {
			return nil
		}
	}
	return &proto.CodexTakeover{CanResume: true, ReleasedDesktop: true}
}

func (n *Node) checkReleasedCodexDesktop(ctx context.Context, id string, src codexSource) (string, error) {
	if n.sessionScope != nil || !uuidRe.MatchString(src.thread) || n.codexBlocked(id) ||
		n.isAssistantSession(id) || n.ownedAssistantSource(src.thread) || codexDesktopAvailable(src.home) ||
		codexArchived(src.home, src.path) || !codexDesktopContinuationChat(src, nil) {
		return "", takeoverRefusal("desktop_resume_unavailable")
	}
	if state, known := n.state.get(id); known && (state.DesktopPendingModel != "" || state.DesktopPendingEffort != "") {
		return "", takeoverRefusal("desktop_resume_unavailable")
	}
	if n.runtimes.codexThreads()[src.thread].session != "" {
		return "", takeoverRefusal("desktop_resume_unavailable")
	}
	if _, live := n.agent(id); live {
		return "", takeoverRefusal("writer_busy")
	}
	if _, _, err := n.codexOwner(id); !errors.Is(err, errCodexNoOwner) {
		return "", takeoverRefusal("writer_busy")
	}
	cwd := ""
	if db := codexAppDBPath(src.home); db != "" {
		// The sidebar cache is advisory. Recheck the native identity, archive bit and cwd.
		where := "archived = 0 AND id = '" + src.thread + "'"
		if src.path == "" {
			if codexHistoryDBPath(src.home) == "" {
				return "", takeoverRefusal("desktop_resume_unavailable")
			}
			where += " AND history_mode = 'paginated'"
		}
		rows, err := readCodexThreads(ctx, db, where)
		row, found := rows[src.thread]
		if err != nil || !found || row.origin().exec || row.origin().sub {
			return "", takeoverRefusal("desktop_resume_unavailable")
		}
		cwd = row.Cwd
	} else if src.path != "" {
		origin := readCodexOrigin(src.path)
		if origin.exec || origin.sub || !pathWithin(src.path, src.home) || rolloutThreadID(src.home, src.path) != src.thread {
			return "", takeoverRefusal("desktop_resume_unavailable")
		}
		cwd = metaOf(src.home, src.path).cwd
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() || !filepath.IsAbs(cwd) ||
		identity.IsAssistantDir(cwd) || !n.sessionScopeAllows(cwd) {
		return "", takeoverRefusal("desktop_resume_unavailable")
	}
	if !codexDesktopWriterReleased(src) {
		return "", takeoverRefusal("writer_busy")
	}
	if src.path != "" {
		writers, err := n.takeoverWriters(src)
		if err != nil || len(writers) > 0 {
			return "", takeoverRefusal("writer_busy")
		}
	}
	return cwd, nil
}

func (n *Node) continueReleasedCodexDesktop(ctx context.Context, id string, plan codexTakeoverPlan) (*proto.Session, error) {
	current := func() (codexSource, error) {
		src, ok := n.codexSource(id)
		src = withoutGoneRollout(src, nil)
		if !ok || src.home != plan.source.home || src.thread != plan.source.thread {
			return codexSource{}, takeoverRefusal("changed")
		}
		cwd, err := n.checkReleasedCodexDesktop(ctx, id, src)
		if err != nil {
			return codexSource{}, err
		}
		if !sameScopePath(cwd, plan.Cwd) {
			return codexSource{}, takeoverRefusal("changed")
		}
		return src, nil
	}
	src, err := current()
	if err != nil {
		return nil, err
	}
	remote, err := n.codexDesktopResumeRemote(ctx, src)
	if err != nil {
		return nil, err
	}
	// Discovery and daemon startup can wait. Recheck immediately before the native
	// claim, whose own writer lock is authoritative if another process wins the race.
	if err := func() error {
		remote.refreshMu.Lock()
		defer remote.refreshMu.Unlock()
		src, err := current()
		if err != nil {
			return err
		}
		return remote.resumeReleasedCodexDesktop(ctx, id, src, plan.Cwd)
	}(); err != nil {
		return nil, err
	}
	remote.rememberJoin(id)
	for _, row := range n.codexSessionsIn(map[string]bool{}, newCodexListPass()) {
		if row.SessionID == id && row.Capabilities != nil && row.Capabilities.Send {
			return &row, nil
		}
	}
	return nil, takeoverRefusal("desktop_resume_unavailable")
}

func codexDesktopResumeEndpoint(home string) string {
	return "unix://" + filepath.Join(home, "app-server-control", "app-server-control.sock")
}

func (n *Node) codexDesktopResumeRemote(ctx context.Context, src codexSource) (*codexRemote, error) {
	endpoint := codexDesktopResumeEndpoint(src.home)
	wait, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ready := func() *codexRemote {
		n.codices.mu.Lock()
		defer n.codices.mu.Unlock()
		for _, remote := range n.codices.remotes {
			if remote.spec.Home != src.home || remote.spec.Endpoint != endpoint {
				continue
			}
			remote.mu.Lock()
			verified := remote.ready && remote.verifiedHome
			remote.mu.Unlock()
			if verified {
				return remote
			}
		}
		return nil
	}
	if remote := ready(); remote != nil {
		return remote, nil
	}
	if _, err := n.sharedTakeoverCodex(wait, src); err != nil {
		return nil, err
	}
	for {
		if remote := ready(); remote != nil {
			return remote, nil
		}
		if !sleepCtx(wait, codexStartPoll) {
			return nil, takeoverRefusal("shared_unavailable")
		}
	}
}

func (c *codexRemote) resumeReleasedCodexDesktop(ctx context.Context, id string, src codexSource, cwd string) error {
	var reply struct {
		Thread      remoteThreadData `json:"thread"`
		Model       string           `json:"model"`
		Effort      string           `json:"reasoningEffort"`
		ServiceTier json.RawMessage  `json:"serviceTier"`
	}
	if err := c.wire.call(ctx, "thread/resume", map[string]any{"threadId": src.thread, "excludeTurns": true}, &reply); err != nil {
		if conflict := codexWriterConflict(err); conflict != nil {
			return takeoverRefusal("writer_busy")
		}
		c.n.logf("codex desktop: native thread resume refused: %v", err)
		return takeoverRefusal("desktop_resume_unavailable")
	}
	t := reply.Thread
	if t.ID != src.thread || t.Ephemeral || t.ParentThreadID != "" || strings.Contains(string(t.Source), "subAgent") ||
		t.Cwd == "" || !sameScopePath(t.Cwd, cwd) {
		return takeoverRefusal("desktop_resume_unavailable")
	}
	c.mu.Lock()
	current := c.threads[src.thread]
	current.id, current.publicID, current.cwd, current.path = src.thread, id, t.Cwd, t.Path
	current.title = firstNonEmpty(t.Name, t.Preview, current.title)
	current.model = firstNonEmpty(reply.Model, t.Model, current.model)
	current.effort = firstNonEmpty(reply.Effort, t.Effort, current.effort)
	current.serviceTier = codexServiceTier(reply.ServiceTier)
	current.joined, current.releasedDesktop = true, true
	current.status = proto.StatusDone
	if t.Status.Type == "active" {
		current.status = proto.StatusWorking
	}
	current.revision++
	c.threads[src.thread] = current
	c.mu.Unlock()
	c.n.notifyCodex()
	return nil
}
