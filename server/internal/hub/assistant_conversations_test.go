package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistant"
	"github.com/nwverse/persona/server/internal/proto"
)

const convDir = "/home/me/.persona/assistant"

// conversationsHub is a hub whose assistant runs on a fake node with a main conversation and three side ones (one
// of them Codex), plus a coding agent elsewhere; settings are kept in memory.
func conversationsHub(t *testing.T) (*Hub, *assistantNode) {
	t.Helper()
	h := newTestHub(t)
	h.store = &settingsStore{vals: map[string][]byte{}}
	n := newAssistantNode(t, h)
	n.m.info.Home = "/home/me"
	if err := h.store.PutSetting(context.Background(), "assistant", assistant.Settings{MachineID: "server", Main: "main-0001", Timezone: "Asia/Shanghai"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	list := []proto.Session{
		{SessionID: "main-0001", Cwd: convDir, Title: "长期对话", UpdatedAt: now.Add(-time.Minute), LastText: "早上好"},
		{SessionID: "trip-0002", Cwd: convDir, Title: "1030 | 探索 | 周末出游", UpdatedAt: now.Add(-2 * time.Hour), LastText: "订好了酒店"},
		{SessionID: "code-0003", Cwd: convDir, Title: "1031 | 修复 | 登录报错", UpdatedAt: now.Add(-3 * time.Hour), Status: proto.StatusWorking},
		{SessionID: "codex-0004", Cwd: convDir, Title: "codex 的对话", Agent: "codex", UpdatedAt: now.Add(-4 * time.Hour)},
		{SessionID: "fresh-0006", Cwd: convDir, Title: "assistant", UpdatedAt: now.Add(-5 * time.Hour)}, // not named yet
		{SessionID: "agent-0005", Cwd: "/code/app", Title: "不是助手的"},
	}
	editSessions(n.m, func([]proto.Session) []proto.Session { return list })
	for _, s := range list {
		n.known[s.SessionID] = true
	}
	connectAssistant(t, h)
	return h, n
}

func tool(t *testing.T, h *Hub, n *assistantNode, self, name, args string) (string, bool) {
	t.Helper()
	r := h.callAssistantTool(context.Background(), n.m, self, name, json.RawMessage(args))
	if len(r.Content) == 0 {
		t.Fatalf("%s: no content", name)
	}
	return r.Content[0].Text, r.IsError
}

func ids(list []proto.Session) string {
	var out []string
	for _, s := range list {
		id := s.SessionID
		if s.Archived {
			id += "(archived)"
		}
		out = append(out, id)
	}
	return strings.Join(out, " ")
}

func TestConversationReadTool(t *testing.T) {
	h, n := conversationsHub(t)
	n.items = []proto.Item{{Role: "user", Kind: proto.KindText, Text: "订周六的酒店"},
		{Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"ls"}`)},
		{Role: "assistant", Kind: proto.KindText, Text: "订好了酒店"}}
	out, isErr := tool(t, h, n, "main-0001", "conversation_read", `{"id":"trip-00"}`)
	if !isErr || !strings.Contains(out, "no conversation") {
		t.Fatalf("a prefix needs 8 characters: %s", out)
	}
	out, isErr = tool(t, h, n, "main-0001", "conversation_read", `{"id":"trip-0002","turns":1}`)
	if isErr || !strings.HasPrefix(out, "「1030 | 探索 | 周末出游」 trip-0002") || !strings.Contains(out, "User: 订周六的酒店") ||
		!strings.Contains(out, "→ Bash ls") || !strings.Contains(out, "Agent: 订好了酒店") {
		t.Fatalf("%s", out)
	}
	if _, isErr = tool(t, h, n, "main-0001", "conversation_read", `{"id":"agent-0005"}`); !isErr {
		t.Fatal("a coding agent is not a conversation")
	}
}

// #7391: a request and its answer are one exchange. The main conversation and another go back and forth three times
// (each first message back keeps the request's hop) and the fourth request is past the limit: a chain counts its
// exchanges, and two conversations that keep writing to each other stop after 2 × 3 messages.
func TestConversationSendAnswerKeepsHop(t *testing.T) {
	h, n := conversationsHub(t)
	for i, want := range []int{1, 1, 2, 2, 3, 3} {
		from, to := "main-0001", "trip-0002"
		if i%2 == 1 {
			from, to = to, from
		}
		out, isErr := tool(t, h, n, from, "conversation_send", fmt.Sprintf(`{"id":%q,"text":"第 %d 条"}`, to, i+1))
		if isErr || !strings.Contains(out, "Sent to") {
			t.Fatalf("message %d, %s → %s: %s", i+1, from, to, out)
		}
		if p := n.lastSend(t); p["session_id"] != to || !strings.Contains(p["text"].(string), fmt.Sprintf("第 %d 跳", want)) {
			t.Fatalf("message %d went in as %v, want hop %d", i+1, p, want)
		}
		if got := hopOf(t, h, to); got != want {
			t.Fatalf("message %d: %s works at hop %d, want %d", i+1, to, got, want)
		}
	}
	// main-0001 is the main conversation: its fourth request is not refused for the hop (#10099) and goes at the
	// last hop; seven messages are far from the round-trip cut (#10400). The other side may answer it, and is cut at
	// the last hop as before.
	for i, st := range []struct{ from, to string }{{"main-0001", "trip-0002"}, {"trip-0002", "main-0001"}} {
		out, isErr := tool(t, h, n, st.from, "conversation_send", fmt.Sprintf(`{"id":%q,"text":"第 %d 条"}`, st.to, i+7))
		if isErr || !strings.Contains(out, "Sent to") || !strings.Contains(n.lastSend(t)["text"].(string), "第 3 跳") {
			t.Fatalf("message %d, %s → %s: %s", i+7, st.from, st.to, out)
		}
	}
	if out, isErr := tool(t, h, n, "trip-0002", "conversation_send", `{"id":"main-0001","text":"第 9 条"}`); !isErr || !strings.Contains(out, "hop 4") {
		t.Fatalf("the other side's fourth request: %s", out)
	}
}
