package node

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"sync"

	"github.com/sunfayao/persona/server/internal/node/harness/codex"
	"github.com/sunfayao/persona/server/internal/proto"
)

type traexHistoryRevisionCache struct {
	stamp    string
	byThread map[string]string
}

var traexHistoryRevisions = struct {
	sync.Mutex
	byDB map[string]traexHistoryRevisionCache
}{byDB: map[string]traexHistoryRevisionCache{}}

// Read all threads' small projection markers once per DB/WAL change, including changes to active turn state.
// A missing revision or failed read keeps the whole database stamp so older schemas still refresh their history.
func traexHistoryStamp(db, thread, stamp string) string {
	traexHistoryRevisions.Lock()
	defer traexHistoryRevisions.Unlock()
	c, ok := traexHistoryRevisions.byDB[db]
	if !ok || c.stamp != stamp {
		c = traexHistoryRevisionCache{stamp: stamp, byThread: map[string]string{}}
		var rows []struct {
			ThreadID           string  `json:"thread_id"`
			NextRolloutOrdinal *int64  `json:"next_rollout_ordinal"`
			ActiveTurnID       *string `json:"active_turn_id"`
		}
		if err := querySQLite(context.Background(), db,
			`SELECT thread_id, next_rollout_ordinal, active_turn_id FROM thread_history_projection_state`, &rows); err == nil {
			for _, row := range rows {
				if row.ThreadID == "" || row.NextRolloutOrdinal == nil || *row.NextRolloutOrdinal < 0 {
					continue
				}
				// Include active state even when the ordinal has not advanced, without reading projection context.
				if revision, err := json.Marshal(row); err == nil {
					c.byThread[row.ThreadID] = "projection:" + db + ":" + string(revision)
				}
			}
		}
		traexHistoryRevisions.byDB[db] = c
	}
	if revision, ok := c.byThread[thread]; ok {
		return revision
	}
	return stamp
}

// TRAE CLI 0.215 stores new conversations in state_<n>.sqlite and thread_history_<n>.sqlite, without a rollout.
// Reuse the paginated Codex reader: both CLIs store the same ThreadItems. Writer locks still identify the exact
// process and conversation. Subagent histories never become independent terminal conversations.
func (n *Node) traexThreads() map[string]codex.AppThread {
	return codexPaginatedThreads(n.traexHome())
}

func traexConversation(t codex.AppThread) bool {
	var source map[string]json.RawMessage
	return validID(t.ID) && t.Source != "subagent" &&
		!(json.Unmarshal([]byte(t.Source), &source) == nil && source["subagent"] != nil)
}

func (n *Node) traexThread(id string) (codex.AppThread, bool) {
	t, ok := n.traexThreads()[id]
	t.IncludeRunning = true
	return t, ok && traexConversation(t)
}

// Keep a conversation readable and listed after its terminal exits, as for rollout-backed agents.
func (n *Node) traexSessions(seen map[string]bool) []proto.Session {
	var out []proto.Session
	for id, t := range n.traexThreads() {
		if seen[id] || !traexConversation(t) {
			continue
		}
		s := proto.Session{SessionID: id, Agent: traexAgent, Title: t.Title, Cwd: t.Cwd, Model: t.Model,
			Effort: t.Effort, Status: proto.StatusOffline, UpdatedAt: t.Updated(), CodexRoute: codexRouteTerminal}
		var m quickMeta
		var hasMeta bool
		if indexed, rev, ok := n.indexedHistoryMeta(id); ok {
			m = n.traexEnrichMeta(id, "", indexed)
			hasMeta = true
			s.LastText, s.Rev = snippet(m.lastText), rev
		} else if p := n.traexPath(id); p != "" {
			if st, err := os.Stat(p); err == nil {
				m = n.traexEnrichMeta(id, p, n.meta(p, st))
				hasMeta = true
				s.LastText, s.Rev = snippet(m.lastText), st.Size()
				m.turnErr.set(&s)
			}
		} else if rp := n.traexRolloutPath(id); rp != "" {
			if st, err := os.Stat(rp); err == nil {
				m = n.meta(rp, st)
				hasMeta = true
			}
		}
		if hasMeta {
			s.Plan = m.plan
			if s.Effort == "" && m.effort != "" {
				s.Effort = m.effort
			}
			n.setSessionContext(&s, m)
			if s.ContextTokens > 0 && s.ContextWindow == 0 {
				if w := lookupTraexModelWindow(s.Model); w > 0 {
					s.ContextWindow = proto.Tokens32(w)
					if existing := s.ContextBreakdown(); existing != nil {
						cp := existing.Clone()
						cp.Free = proto.Int(max(int(s.ContextWindow)-int(s.ContextTokens), 0))
						s.SetContextBreakdown(cp)
					}
				}
			}
		}
		n.decorate(&s)
		n.markStopped(&s)
		out = append(out, s)
	}
	// The final list's stable time sort must not inherit random map order for ties.
	sort.Slice(out, func(i, j int) bool { return out[i].SessionID < out[j].SessionID })
	return out
}
