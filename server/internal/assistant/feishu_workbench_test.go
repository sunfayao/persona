package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/feishubot"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// The workbench (feishu_workbench*.go, feishu_companions.go, #1961): off, the bot is what it was; on, /home pins one
// home card and keeps it the only one, every card gets a way back to it, /start asks for a permission mode, and a
// polling notifications companion takes the notices. All ids are placeholders.

var workbenchOnFeatures = FeishuFeatures{Main: true, Agents: true, Pushes: true, Reports: true, Workbench: true}

// workbenchEnv is the board's env with the workbench's transport calls recorded.
type workbenchEnv struct {
	*boardEnv
	pins      []map[string]any // feishu.pin
	recalls   []map[string]any // feishu.recall
	recallErr error
}

func newWorkbenchEnv(t *testing.T, f FeishuFeatures) *workbenchEnv {
	t.Helper()
	e := &workbenchEnv{boardEnv: newBoardEnv(t)}
	e.f.answerApp(AppFeishu, FeishuPin, func(p json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		e.mu.Lock()
		defer e.mu.Unlock()
		e.pins = append(e.pins, m)
		return map[string]any{}, nil
	})
	e.f.answerApp(AppFeishu, FeishuRecall, func(p json.RawMessage) (any, error) {
		var m map[string]any
		_ = json.Unmarshal(p, &m)
		e.mu.Lock()
		defer e.mu.Unlock()
		e.recalls = append(e.recalls, m)
		return map[string]any{}, e.recallErr
	})
	e.setFeatures(f)
	_, _ = e.s.feishuFeatures(context.Background())
	return e
}

func (e *workbenchEnv) command(cmd string) (bool, string) {
	e.t.Helper()
	f, _ := e.s.feishuFeatures(context.Background())
	return e.s.botCommand(context.Background(), f, "om_cmd", cmd, "", "")
}

func (e *workbenchEnv) postJSON(i int) string {
	e.t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	if i < 0 {
		i += len(e.posts)
	}
	if i < 0 || i >= len(e.posts) {
		e.t.Fatalf("no post %d of %d", i, len(e.posts))
	}
	s, _ := e.posts[i]["card"].(string)
	return s
}

func (e *workbenchEnv) counts() (posts, pins, recalls, updates int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.posts), len(e.pins), len(e.recalls), len(e.updates)
}

// Off (the default), nothing of it exists: the commands go on as text, /start has no permission field, no card gets a
// workbench button and /help does not mention it.
func TestWorkbenchOffChangesNothing(t *testing.T) {
	e := newWorkbenchEnv(t, FeishuFeatures{Main: true, Agents: true})
	for _, cmd := range []string{"/home", "/today", "/inbox", "/工作台"} {
		if h, rest := e.command(cmd); h || rest != cmd {
			t.Fatalf("%s with the workbench off: handled %v, %q", cmd, h, rest)
		}
	}
	if js := e.s.startAgentCard(context.Background(), "").JSON(); strings.Contains(js, `"name":"mode"`) {
		t.Fatalf("/start has a permission field with the workbench off: %s", js)
	}
	if _, err := e.s.botPost(context.Background(), "", feishubot.NewCard("t", feishubot.Blue, "t").Markdown("hello"), ""); err != nil {
		t.Fatal(err)
	}
	if js := e.postJSON(-1); strings.Contains(js, `"to":"home"`) {
		t.Fatalf("a card got the workbench button with the workbench off: %s", js)
	}
	l := e.s.botLang(context.Background())
	if strings.Contains(botHelp(l, FeishuFeatures{Main: true, Agents: true}), "/home") {
		t.Fatal("/help mentions /home with the workbench off")
	}
	if _, pins, recalls, _ := e.counts(); pins+recalls != 0 {
		t.Fatalf("pins %d, recalls %d with the workbench off", pins, recalls)
	}
}

// /home posts the home card, pins it as the bot's only pin and makes it the board; the next /home posts a fresh one,
// pins it and withdraws the one before. One too old to withdraw is redrawn to say where the workbench went.
func TestWorkbenchHomeIsPinnedAndTheOnlyOne(t *testing.T) {
	e := newWorkbenchEnv(t, workbenchOnFeatures)
	l := e.s.botLang(context.Background())
	if h, _ := e.command("/home"); !h {
		t.Fatal("/home not handled with the workbench on")
	}
	if js := e.postJSON(0); !strings.Contains(js, wbT(l, "home_title")) || !strings.Contains(js, `"value":"to=start"`) {
		t.Fatalf("the home card: %s", js)
	}
	e.mu.Lock()
	first := fmt.Sprint(e.pins[0]["message_id"])
	exclusive := e.pins[0]["exclusive"]
	e.mu.Unlock()
	if first != "om_board_1" || exclusive != true {
		t.Fatalf("pin %v, exclusive %v", first, exclusive)
	}
	if ref := e.s.boardRef(context.Background()); ref == nil || ref.MessageID != "om_board_1" {
		t.Fatalf("the board is %+v", ref)
	}

	e.command("/home")
	posts, pins, recalls, _ := e.counts()
	e.mu.Lock()
	gone := fmt.Sprint(e.recalls[0]["message_id"])
	e.mu.Unlock()
	if posts != 2 || pins != 2 || recalls != 1 || gone != "om_board_1" {
		t.Fatalf("second /home: posts %d, pins %d, recalls %d (%s)", posts, pins, recalls, gone)
	}
	if ref := e.s.boardRef(context.Background()); ref.MessageID != "om_board_2" {
		t.Fatalf("the board is %+v", ref)
	}

	e.mu.Lock()
	e.recallErr = errors.New("too old to withdraw")
	e.mu.Unlock()
	e.command("/home")
	e.mu.Lock()
	defer e.mu.Unlock()
	last := e.updates[len(e.updates)-1]
	if last["message_id"] != "om_board_2" || !strings.Contains(fmt.Sprint(last["card"]), wbT(l, "desk_moved_title")) {
		t.Fatalf("the old workbench was not redrawn: %v", last)
	}
}

// With the workbench on, a card the bot posts gets the workbench button (once), the home card itself none; switched
// off, the next card has none.
func TestWorkbenchButtonOnEveryCard(t *testing.T) {
	e := newWorkbenchEnv(t, workbenchOnFeatures)
	e.command("/home")
	if js := e.postJSON(0); strings.Contains(js, `"to":"home"`) {
		t.Fatalf("the home card links to itself: %s", js)
	}
	card := feishubot.NewCard("t", feishubot.Blue, "t").Markdown("an answer")
	if _, err := e.s.botPost(context.Background(), "", card, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.botPost(context.Background(), "", card, ""); err != nil { // the same card again (a retry)
		t.Fatal(err)
	}
	if js := e.postJSON(-1); strings.Count(js, `"to":"home"`) != 1 {
		t.Fatalf("the workbench button on a card: %s", js)
	}
	e.setFeatures(FeishuFeatures{Main: true, Agents: true})
	_, _ = e.s.feishuFeatures(context.Background())
	if _, err := e.s.botPost(context.Background(), "", feishubot.NewCard("t", feishubot.Blue, "t").Markdown("later"), ""); err != nil {
		t.Fatal(err)
	}
	if js := e.postJSON(-1); strings.Contains(js, `"to":"home"`) {
		t.Fatalf("the workbench button after switching it off: %s", js)
	}
}

// buttonPayload finds a button by its localized label anywhere in a card, including a form.
func buttonPayload(t *testing.T, card *feishubot.Card, label string) map[string]any {
	t.Helper()
	var root any
	if err := json.Unmarshal([]byte(card.JSON()), &root); err != nil {
		t.Fatal(err)
	}
	var walk func(any) map[string]any
	walk = func(node any) map[string]any {
		switch v := node.(type) {
		case map[string]any:
			if v["tag"] == "button" {
				if text, _ := v["text"].(map[string]any); text["content"] == label {
					return v
				}
			}
			for _, child := range v {
				if found := walk(child); found != nil {
					return found
				}
			}
		case []any:
			for _, child := range v {
				if found := walk(child); found != nil {
					return found
				}
			}
		}
		return nil
	}
	if found := walk(root); found != nil {
		return found
	}
	t.Fatalf("no button %q in %s", label, card.JSON())
	return nil
}

func behaviorPayload(t *testing.T, button map[string]any, kind string) map[string]any {
	t.Helper()
	behaviors, _ := button["behaviors"].([]any)
	for _, raw := range behaviors {
		behavior, _ := raw.(map[string]any)
		if behavior["type"] == kind {
			return behavior
		}
	}
	t.Fatalf("no %s behavior in %v", kind, button)
	return nil
}

// Navigation added at different times still forms one footer. Approval and form callbacks keep their own row, and the
// card-size guard still leaves Workbench off an oversized card (#5317).
func TestWorkbenchFooterKeepsNavigationTogether(t *testing.T) {
	e := newWorkbenchEnv(t, workbenchOnFeatures)
	t.Run("navigation", func(t *testing.T) {
		card := feishubot.NewCard("", "", "answer").Note("Main chat").Buttons(feishubot.LinkBtn("Open on web", "https://example.com"))
		e.s.workbenchStamp(context.Background(), card)
		if got := strings.Count(card.JSON(), `"tag":"column_set"`); got != 1 {
			t.Fatalf("navigation actions occupy %d rows: %s", got, card.JSON())
		}
		row := card.Body.Elements[len(card.Body.Elements)-1].(map[string]any)
		if got := len(row["columns"].([]any)); got != 2 {
			t.Fatalf("the shared navigation row has %d buttons: %s", got, card.JSON())
		}
		open := behaviorPayload(t, buttonPayload(t, card, "Open on web"), "open_url")
		if got := open["default_url"]; got != "https://example.com" {
			t.Fatalf("the original navigation URL changed: %v", got)
		}
		homeLabel := wbT(e.s.botLang(context.Background()), "stamp_home")
		if strings.Contains(homeLabel, "\n") {
			t.Fatalf("the localized Workbench label wraps: %q", homeLabel)
		}
		homeButton := buttonPayload(t, card, homeLabel)
		if homeButton["type"] != "text" || homeButton["size"] != "small" {
			t.Fatalf("the way back is not a small text button: %v", homeButton)
		}
		home := behaviorPayload(t, homeButton, "callback")
		value, _ := home["value"].(map[string]any)
		if value["a"] != "nav" || value["to"] != "home" {
			t.Fatalf("the Workbench callback changed: %v", value)
		}
	})
	t.Run("callbacks stay independent", func(t *testing.T) {
		card := feishubot.NewCard("", "", "approval").Buttons(feishubot.Btn("Allow", feishubot.Primary, map[string]any{"allow": true}))
		e.s.workbenchStamp(context.Background(), card)
		if got := strings.Count(card.JSON(), `"tag":"column_set"`); got != 2 {
			t.Fatalf("an approval action was merged with navigation: %s", card.JSON())
		}
		allow := behaviorPayload(t, buttonPayload(t, card, "Allow"), "callback")
		if value, _ := allow["value"].(map[string]any); value["allow"] != true {
			t.Fatalf("the approval callback changed: %v", value)
		}
	})
	t.Run("mixed actions stay independent", func(t *testing.T) {
		card := feishubot.NewCard("", "", "approval").Buttons(
			feishubot.LinkBtn("Open on web", "https://example.com"),
			feishubot.Btn("Allow", feishubot.Primary, map[string]any{"allow": true}))
		e.s.workbenchStamp(context.Background(), card)
		if got := strings.Count(card.JSON(), `"tag":"column_set"`); got != 2 {
			t.Fatalf("a mixed action row was merged with navigation: %s", card.JSON())
		}
		if got := behaviorPayload(t, buttonPayload(t, card, "Open on web"), "open_url")["default_url"]; got != "https://example.com" {
			t.Fatalf("the mixed row's URL changed: %v", got)
		}
		allow := behaviorPayload(t, buttonPayload(t, card, "Allow"), "callback")
		if value, _ := allow["value"].(map[string]any); value["allow"] != true {
			t.Fatalf("the mixed row's callback changed: %v", value)
		}
	})
	t.Run("form callbacks stay independent", func(t *testing.T) {
		card := feishubot.NewCard("", "", "form").Form("approval", nil,
			feishubot.Submit("Send", feishubot.Primary, "send", map[string]any{"sent": true}))
		e.s.workbenchStamp(context.Background(), card)
		send := buttonPayload(t, card, "Send")
		if send["form_action_type"] != "submit" {
			t.Fatalf("the form action changed: %v", send)
		}
		callback := behaviorPayload(t, send, "callback")
		if value, _ := callback["value"].(map[string]any); value["sent"] != true {
			t.Fatalf("the form callback value changed: %v", value)
		}
	})
	t.Run("size guard", func(t *testing.T) {
		card := feishubot.NewCard("", "", "large").Markdown(strings.Repeat("x", workbenchStampMost)).Buttons(feishubot.LinkBtn("Open on web", "https://example.com"))
		e.s.workbenchStamp(context.Background(), card)
		if strings.Contains(card.JSON(), `"to":"home"`) {
			t.Fatalf("the oversized card gained a workbench button: %d bytes", len(card.JSON()))
		}
	})
}

// /start asks for a permission mode before what to do, offering every mode the agents take; the mode picked goes to
// the agent's start, one that is not a mode does not.
func TestWorkbenchStartAsksForAPermissionMode(t *testing.T) {
	e := newWorkbenchEnv(t, workbenchOnFeatures)
	js := e.s.startAgentCard(context.Background(), "").JSON()
	if !strings.Contains(js, `"name":"mode"`) || !strings.Contains(js, `"bypassPermissions"`) || !strings.Contains(js, `"acceptEdits"`) {
		t.Fatalf("the /start form: %s", js)
	}
	if strings.Index(js, `"name":"mode"`) > strings.Index(js, `"name":"prompt"`) {
		t.Fatalf("the permission field comes after what to do: %s", js)
	}

	f := newFakeHub().withMachine()
	f.sessions["box"] = nil
	f.devices = []Device{{ID: "box", Name: "box", Home: "/home/me", Online: true, Reachable: true}}
	s := newTestService(t, f)
	f.answerApp(AppFeishu, FeishuStatusGet, func(json.RawMessage) (any, error) {
		return FeishuStatus{Ready: true, Enabled: true, Owner: "ou_me", Features: workbenchOnFeatures}, nil
	})
	f.answerApp(AppFeishu, FeishuText, func(json.RawMessage) (any, error) { return map[string]any{}, nil })
	f.answerApp(AppFeishu, FeishuPost, func(json.RawMessage) (any, error) { return map[string]any{"message_id": "om_started"}, nil })
	for i, c := range []struct{ mode, want string }{{"bypassPermissions", "bypassPermissions"}, {"everything", ""}, {"", ""}} {
		if err := s.botStartAgent("box", "/code", "", c.mode); err != nil {
			t.Fatalf("mode %q: %v", c.mode, err)
		}
		f.mu.Lock()
		if len(f.starts) != i+1 {
			f.mu.Unlock()
			t.Fatalf("mode %q: %d starts", c.mode, len(f.starts))
		}
		got, _ := f.starts[i]["permission_mode"].(string)
		f.mu.Unlock()
		if got != c.want {
			t.Fatalf("mode %q: permission_mode %q, want %q", c.mode, got, c.want)
		}
	}
}

// /help with the workbench on names its commands.
func TestWorkbenchHelpNamesItsCommands(t *testing.T) {
	l := i18n.Lang("en")
	if h := botHelp(l, workbenchOnFeatures); !strings.Contains(h, "/home") || !strings.Contains(h, "/inbox") {
		t.Fatalf("help: %s", h)
	}
}

// companionsCall is one of the companions' routes, as a device with every right (the hub relays the extension's).
func (e *workbenchEnv) companionsCall(method, path, body string) (int, map[string]any) {
	e.t.Helper()
	rec := httptest.NewRecorder()
	e.s.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

const companionsBody = `{"bots":[{"kind":"inbox","name":"Inbox","icon":"📥","app_id":"cli_inboxbot01"},{"kind":"notify","name":"Notices","icon":"🔔","app_id":"cli_noticebot1"}]}`

// /inbox lists what needs the owner first, each with Done and (for a reply that can be sent) AI draft; Done files it
// and the card is redrawn without it.
func TestWorkbenchInboxCardFilesAnItem(t *testing.T) {
	e := newWorkbenchEnv(t, workbenchOnFeatures)
	now := time.Now()
	e.s.inbox.mu.Lock()
	if e.s.inbox.items == nil {
		e.s.inbox.items = map[string]*InboxItem{}
	}
	e.s.inbox.items["lark:f1:1"] = &InboxItem{ID: "lark:f1:1", Kind: KindDM, Tier: TierAction, Sender: "Alex", Summary: "asks whether the release is out",
		UpdatedAt: now.Add(-time.Hour), Lark: &InboxLark{FeedID: "f1"}}
	e.s.inbox.items["lark:f2:1"] = &InboxItem{ID: "lark:f2:1", Kind: KindDM, Tier: TierFYI, Sender: "Sam", Summary: "shared the slides",
		UpdatedAt: now, Lark: &InboxLark{FeedID: "f2"}}
	e.s.inbox.mu.Unlock()

	card, _ := e.s.inboxCard(context.Background())
	js := card.JSON()
	if a, b := strings.Index(js, "Alex"), strings.Index(js, "Sam"); a < 0 || b < 0 || a > b {
		t.Fatalf("what needs the owner is not first: %s", js)
	}
	if !strings.Contains(js, `"op":"draft"`) || !strings.Contains(js, `"op":"done"`) {
		t.Fatalf("the Inbox card's buttons: %s", js)
	}
	var a feishubot.CardAction
	a.Action.Value = map[string]any{"a": "ib", "op": "done", "id": "lark:f1:1"}
	a.Context.MessageID = "om_inbox"
	resp := e.s.botCardAction(context.Background(), &a, "")
	if resp == nil || resp.Card == nil {
		t.Fatalf("Done: %+v", resp)
	}
	redrawn, _ := json.Marshal(resp.Card.Data)
	if strings.Contains(string(redrawn), "Alex") || !strings.Contains(string(redrawn), "Sam") {
		t.Fatalf("the redrawn card: %s", redrawn)
	}
}

// A card names where an item came from in words: a connector's label for it, else its source's name (a subscription's
// one row has no label, #5671), never the source's id.
func TestWorkbenchSourceIsAName(t *testing.T) {
	for _, c := range []struct {
		it   InboxItem
		want string
	}{
		{InboxItem{Source: "github", AccountName: "GitHub", Connector: &InboxConnector{Label: "Review requested"}}, "Review requested"},
		{InboxItem{Source: "sub_example", AccountName: "Example group", Connector: &InboxConnector{}}, "Example group"},
		{InboxItem{Source: "sub_example", Connector: &InboxConnector{}}, "sub_example"},
	} {
		if got := wbSource(i18n.En, c.it); got != c.want {
			t.Errorf("%+v: %q, want %q", c.it, got, c.want)
		}
	}
}

// What waits for the owner is rows pressed whole: a card that holds something up comes down. An agent's row says its
// state as a tag, not a coloured dot.
func TestWorkbenchHomeRowsArePressedWhole(t *testing.T) {
	e := newWorkbenchEnv(t, workbenchOnFeatures)
	e.s.fleet.interactions["ix_q"] = &pending{ix: proto.Interaction{ID: "ix_q", MachineID: "box", SessionID: "ag1", Kind: proto.IxQuestion,
		Questions: []proto.Question{{Question: "用哪个库？"}}}}
	card, _, _ := e.s.homeCard(context.Background(), workbenchOnFeatures)
	js := card.JSON()
	if !strings.Contains(js, `"tag":"interactive_container"`) || !strings.Contains(js, `"value":{"a":"nav","id":"ix_q","to":"ix"}`) ||
		!strings.Contains(js, `"content":"等你 1"`) {
		t.Fatalf("the home card: %s", js)
	}
	sess := proto.Session{State: proto.StateWorking, LastText: "在跑**测试**"}
	row := agentRowMarkdown(i18n.ZhHans, "修登录", "", "box", sess, 70)
	if row != "<text_tag color='blue'>工作中</text_tag> **修登录** · box\n<font color='grey'>在跑测试</font>" {
		t.Fatalf("agent row %q", row)
	}
	// An agent named by its herdr tab shows its other name under the label.
	row = agentRowMarkdown(i18n.ZhHans, "登录", "修登录的测试", "box", sess, 70)
	if row != "<text_tag color='blue'>工作中</text_tag> **登录** · box\n修登录的测试\n<font color='grey'>在跑测试</font>" {
		t.Fatalf("agent row with a secondary name %q", row)
	}
}

// Sending a draft goes out under the owner's name: its button asks first in Feishu's dialog, Redraft does not.
func TestDraftCardSendAsksFirst(t *testing.T) {
	s := newTestService(t, newFakeHub())
	it := InboxItem{ID: "i1", Source: "lark", Title: "项目群", Sender: "张三", Preview: "明天能发吗？"}
	js := s.draftCard(context.Background(), it, "可以，明早发。", nil, "").JSON()
	if n := strings.Count(js, `"confirm":`); n != 1 || !strings.Contains(js, `"op":"send"`) || !strings.Contains(js, `"op":"redraft"`) {
		t.Fatalf("the draft card asks first %d times: %s", n, js)
	}
}
