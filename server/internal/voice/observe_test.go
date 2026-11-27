package voice

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

// docs/assistant/voice.md "Per-round tracking", "Announcements" (#3503): turns the call did not hand over, turns that stopped and are not over, a
// heartbeat while it works, and the user cutting in on the model.

// noRunFrames: the app got no run frame (an observed run is the voice's to tell, not one of the call's).
func (a *app) noRunFrames() {
	a.t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.types["run"] {
		a.t.Error("a run frame for a turn the call did not hand over")
	}
}

// A heartbeat not said in time is dropped; one waiting when its run has more to say gives way to that.
func TestStaleHeartbeatIsDropped(t *testing.T) {
	c := &conversation{}
	now := time.Now()
	c.queue = []*sentWords{{words: words{text: "beat"}, prio: prioHeartbeat, run: "r1", expires: now.Add(-time.Second)}}
	if s := c.nextToSay(now); s != nil || len(c.queue) != 0 {
		t.Fatalf("a stale heartbeat: %+v, %d queued", s, len(c.queue))
	}
}

// speak sends the app's audio: d of the user's voice (or of silence), in 20 ms frames at 16 kHz.
func (a *app) speak(d time.Duration, voiced bool) {
	amp := int16(0)
	if voiced {
		amp = 3000
	}
	for range int(d / (20 * time.Millisecond)) {
		_ = a.c.Write(context.Background(), websocket.MessageBinary, tone(320, amp))
	}
}

// cleared: the app got a clear within a while.
func (a *app) cleared(within time.Duration) bool {
	time.Sleep(within)
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.types["clear"]
}

// heardAudio waits for the model's speech to reach the app.
func (a *app) heardAudio() {
	a.t.Helper()
	select {
	case <-a.audio:
	case <-time.After(5 * time.Second):
		a.t.Fatal("no audio")
	}
}

// The energy rule's stretch: voiced audio adds up across gaps up to bargeGap, a longer gap starts it over.
func TestBargeStretch(t *testing.T) {
	var b barge
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }
	if got := b.add(true, ms(300)); got != ms(300) {
		t.Fatalf("voiced: %v", got)
	}
	b.add(false, ms(100))
	if got := b.add(true, ms(100)); got != ms(500) {
		t.Fatalf("across a short gap: %v", got)
	}
	b.add(false, ms(100))
	b.add(false, ms(100))
	if got := b.add(true, ms(100)); got != ms(100) {
		t.Fatalf("after a long gap: %v", got)
	}
	if got := (&barge{}).add(false, time.Second); got != 0 {
		t.Fatalf("silence: %v", got)
	}
}

// Where a turn the call did not hand over begins: its message when it is in and no turn ended after it (else after the
// last turn's end), and as it is over, the first message after the last turn that ended before it.
func TestTurnStart(t *testing.T) {
	user := func(seq int) proto.Item { return proto.Item{Seq: seq, Role: "user", Kind: proto.KindText, Text: "x"} }
	words := func(seq int) proto.Item {
		return proto.Item{Seq: seq, Role: "assistant", Kind: proto.KindText, Text: "y"}
	}
	ended := func(seq int) proto.Item { return proto.Item{Seq: seq, TurnDuration: &proto.TurnDuration{}} }
	for _, c := range []struct {
		name         string
		items        []proto.Item
		after        int
		msg, endedAt int
	}{
		{"its message is in", []proto.Item{user(5), words(6)}, 0, 5, 0},
		{"its message is not in yet", []proto.Item{user(5), words(6), ended(7)}, 0, 0, 7},
		{"read before", []proto.Item{user(5), words(6)}, 6, 0, 0},
		{"after a turn", []proto.Item{user(5), words(6), ended(7), user(8), user(9)}, 0, 8, 7},
	} {
		msg, at := turnStart(c.items, c.after)
		if got := 0; msg != nil {
			got = msg.Seq
			if got != c.msg {
				t.Errorf("%s: message %d, want %d", c.name, got, c.msg)
			}
		} else if c.msg != 0 {
			t.Errorf("%s: no message, want %d", c.name, c.msg)
		}
		if at != c.endedAt {
			t.Errorf("%s: ended at %d, want %d", c.name, at, c.endedAt)
		}
	}
	if got := opener([]proto.Item{user(5), words(6), ended(7), user(8), user(9), words(10), ended(11)}); got == nil || got.Seq != 8 {
		t.Errorf("opener %+v", got)
	}
	if got := opener([]proto.Item{words(6), ended(7)}); got != nil {
		t.Errorf("opener of no message: %+v", got)
	}
}

// The sort of step a heartbeat says is in the call's language, as the hub words it for the apps; never the node's
// Chinese line, nor what it acts on.
func TestActivityIn(t *testing.T) {
	for _, c := range []struct {
		lang           i18n.Lang
		activity, want string
	}{
		{i18n.ZhHans, "在跑 npm test -- --token=abc", "在跑"},
		{i18n.ZhHans, "在等 CI", "在等 CI"},
		{i18n.En, "在跑 npm test -- --token=abc", "Running"},
		{i18n.Ja, "在跑 npm test -- --token=abc", "実行中"},
		{i18n.En, "在跑测试", "Running tests"},
		{i18n.Ja, "在读 runs.go", "読み取り中"},
		{i18n.En, "在等 CI", i18n.T(i18n.En, "hub-transcript_text.act_ci")},
		{i18n.En, "子任务完成 review_api", "Subtask done"},
		{i18n.Ja, "在联系子任务 review_api", "サブタスクに連絡中"},
		{i18n.En, "在想 runs.go", ""},
		{i18n.En, "", ""},
	} {
		if got := activityIn(c.lang, c.activity); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.lang, c.activity, got, c.want)
		}
	}
}
