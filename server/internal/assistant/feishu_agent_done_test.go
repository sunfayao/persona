package assistant

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// answerAgentDoneBot pairs the bot (pushes on or off) with a web address for the open button, and records posts.
func answerAgentDoneBot(ft *fleetTest, pushes bool) {
	ft.f.answerApp(AppFeishu, FeishuStatusGet, func(json.RawMessage) (any, error) {
		return FeishuStatus{Ready: true, Enabled: true, Owner: "ou_a", PairedAt: time.Unix(100, 0), PublicURL: "https://hub.example",
			Features: FeishuFeatures{Main: true, Cards: true, Sync: true, Pushes: pushes}}, nil
	})
	ft.f.answerApp(AppFeishu, FeishuPost, func(p json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		return map[string]any{"message_id": "om_done"}, nil
	})
}

// driveAgentDone lists sess working and then done: the working → done transition that ends a turn.
func driveAgentDone(ft *fleetTest, sess proto.Session) {
	work := sess
	work.Status = proto.StatusWorking
	done := sess
	done.Status = proto.StatusDone
	ft.sessions("mac", work)
	ft.sessions("mac", done)
}

func waitAgentDonePosts(ft *fleetTest, n int) []map[string]any {
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := ft.f.appParams(AppFeishu, FeishuPost)
		if len(got) >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func agentSession() proto.Session {
	return proto.Session{SessionID: "s1", Title: "修登录", Cwd: "/code/app", Agent: "claude", PaneID: "p1",
		LastText: "改好了", UpdatedAt: time.Now()}
}

// A result longer than the card's budget still goes out as one card, marked as cut short.
func TestFeishuAgentDoneLongResult(t *testing.T) {
	ft := newFleetTest(t)
	fastDone(ft)
	answerAgentDoneBot(ft, true)
	sess := agentSession()
	sess.LastText = strings.Repeat("很长的结果。", 500)
	driveAgentDone(ft, sess)
	posts := waitAgentDonePosts(ft, 1)
	if len(posts) != 1 {
		t.Fatalf("posted %d cards, want 1", len(posts))
	}
	card, _ := posts[0]["card"].(string)
	if !strings.Contains(card, "只发了开头") {
		t.Fatalf("long result is not marked as cut short:\n%s", card)
	}
}

// When the card's JSON size exceeds answerCardMaxBytes and gets halved, the note and fallback
// must mark it as cut short even if the original text was <= agentDoneClip (#4944).
func TestFeishuAgentDoneHalvedCardMarksClipped(t *testing.T) {
	ft := newFleetTest(t)
	fastDone(ft)
	answerAgentDoneBot(ft, true)
	sess := agentSession()
	sess.Title = strings.Repeat("A", 27000)
	// 600 runes <= agentDoneClip (1000), but with wide title it exceeds answerCardMaxBytes
	// on the first iteration and limit is halved to 500 runes.
	sess.LastText = strings.Repeat("字", 600)
	driveAgentDone(ft, sess)
	posts := waitAgentDonePosts(ft, 1)
	if len(posts) != 1 {
		t.Fatalf("posted %d cards, want 1", len(posts))
	}
	card, _ := posts[0]["card"].(string)
	if !strings.Contains(card, "只发了开头") {
		t.Fatalf("halved card is missing cut short notice:\n%s", card)
	}
	fallback, _ := posts[0]["fallback"].(string)
	if !strings.Contains(fallback, "只发了开头") {
		t.Fatalf("halved fallback is missing cut short notice:\n%s", fallback)
	}
}

// A long result shows its beginning, the rest folded; an agent that asks the owner something is orange; the one
// button opens its conversation, primary.
func TestFeishuAgentDoneLooks(t *testing.T) {
	ft := newFleetTest(t)
	fastDone(ft)
	answerAgentDoneBot(ft, true)
	sess := agentSession()
	sess.LastText = strings.Repeat("第一段说明。", 60) + "\n\n" + strings.Repeat("第二段细节。", 60)
	driveAgentDone(ft, sess)
	posts := waitAgentDonePosts(ft, 1)
	if len(posts) != 1 {
		t.Fatalf("posted %d cards", len(posts))
	}
	card, _ := posts[0]["card"].(string)
	if !strings.Contains(card, `"tag":"collapsible_panel"`) || !strings.Contains(card, "第二段细节") || strings.Count(card, `"tag":"button"`) != 1 ||
		!strings.Contains(card, `"type":"primary"`) {
		t.Fatalf("card %s", card)
	}
}

func TestAgentDoneState(t *testing.T) {
	for _, c := range []struct {
		head   string
		asks   bool
		title  string
		color  string
		tagKey string
	}{
		{"修登录", false, "修登录", "green", "agent_tag_done"},
		{"修登录", true, "修登录", "orange", "agent_tag_asks"},
		{"⏳ 修登录", false, "修登录", "orange", "agent_tag_blocked"},
		{"⚠️ 修登录", false, "修登录", "red", "agent_tag_failed"},
	} {
		title, color, tag := agentDoneState(c.head, proto.Session{AwaitsReply: c.asks})
		if title != c.title || color != c.color || tag != "assistant-feishu_cards."+c.tagKey {
			t.Errorf("%q asks=%v: %q %q %q", c.head, c.asks, title, color, tag)
		}
	}
}
