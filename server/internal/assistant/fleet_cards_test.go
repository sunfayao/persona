package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	approvalops "github.com/sunfayao/persona/server/internal/approval"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/devicewire"
	"github.com/sunfayao/persona/server/internal/link"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
	"github.com/sunfayao/persona/server/internal/screen"
)

// The cards the manager makes itself: a TUI dialog read off the screen (fleet_ix_screen.go), the Codex cards a node
// lists (fleet_ix_codex.go), a plan answered in the terminal (fleet_ix_terminal.go). Moved from the hub's tests with them.

// Screens with a dialog the manager can read (screen.ParseDialog).
const (
	pickModel    = "  Pick a model\n\n› 1. gpt-6\n  2. gpt-6-mini\n\n  Press enter to confirm or esc to go back\n"
	pickEffort   = "  Pick a reasoning level\n\n› 1. low\n  2. high\n\n  Press enter to confirm or esc to go back\n"
	dialogScreen = "────────────────────\n Do you want to proceed?\n ❯ 1. Yes\n   2. Type something.\n   3. No\n"
)

// cardNode is a node that shows a screen (session.screen) and takes Codex answers (codex.answer). Like a current
// node it says the revision of the dialog on the screen it gives (dialog_rev), unless dialogRev says otherwise: ""
// is a node from before #5538 that says none, anything else the revision a node of another build parses.
type cardNode struct {
	mu        sync.Mutex
	answers   []map[string]any
	err       error
	screen    string
	dialogRev *string
	trae      *screen.Dialog
	keysErr   error // what session.keys answers
}

func (c *cardNode) setScreen(s string) {
	c.mu.Lock()
	c.screen = s
	c.mu.Unlock()
}

// newCardFleet is a manager whose machine "mac" has a cardNode.
func newCardFleet(t *testing.T, store bool) (*fleetTest, *cardNode) {
	t.Helper()
	ft := newFleetTestNoStore(t)
	if store {
		ft = newFleetTest(t)
	}
	c := &cardNode{}
	ft.d.mu.Lock()
	ft.d.machines["mac"].call = func(method string, params json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		c.mu.Lock()
		defer c.mu.Unlock()
		switch method {
		case "session.screen":
			if c.screen == "node-error" {
				return nil, errNode
			}
			if c.trae != nil && p["trae_question"] == true {
				return map[string]any{"text": "", "trae_question": c.trae}, nil
			}
			res := map[string]any{"text": c.screen}
			if c.dialogRev != nil {
				if *c.dialogRev != "" {
					res["dialog_rev"] = *c.dialogRev
				}
			} else if d := screen.ParseDialog(c.screen); d != nil && p["lines"] == float64(screen.DialogLines) {
				res["dialog_rev"] = d.Rev
			}
			return res, nil
		case "session.keys":
			if c.keysErr != nil {
				return nil, c.keysErr
			}
		case "codex.answer":
			c.answers = append(c.answers, p)
			if c.err != nil {
				return nil, c.err
			}
			return map[string]any{"ok": true}, nil
		}
		return map[string]any{}, nil
	}
	ft.d.mu.Unlock()
	return ft, c
}

func TestTraeQuestionCardUsesNativeRevisionAndStaysWithTheUser(t *testing.T) {
	ft, node := newCardFleet(t, false)
	d := &screen.Dialog{Question: "Which fixture color?", Rev: screen.TraeQuestionRev + "fixture",
		Options: []screen.Option{{Key: "1", Label: "Blue"}, {Key: "2", Label: "Type your answer", Kind: screen.KindTypeSomething}}}
	node.trae = d
	raw, got, err := ft.s.screenDialog(context.Background(), "mac", "synthetic")
	if err != nil || raw != "" || got == nil || got.Rev != d.Rev || len(got.Options) != 2 {
		t.Fatalf("native question projection: %q %+v %v", raw, got, err)
	}
	if terminalQuestion(proto.Interaction{Kind: proto.IxScreen, Dialog: got}) {
		t.Fatal("a native question was routed for automatic answers")
	}
	p, err := screenAnswer("synthetic", got, proto.Resolution{Option: "2", Text: "Teal"})
	if err != nil || p["rev"] != d.Rev || p["option"] != "2" || p["text"] != "Teal" {
		t.Fatalf("native answer parameters: %+v %v", p, err)
	}
}

func pendingIDs(s *Service) map[string]string {
	out := map[string]string{}
	for _, ix := range s.pendingInteractions() {
		out[ix.ID] = ix.Kind
	}
	return out
}

// waitCards waits up to d for n interactions and says how long it took.
func waitCards(s *Service, n int, d time.Duration) (map[string]string, time.Duration) {
	start := time.Now()
	for len(pendingIDs(s)) < n && time.Since(start) < d {
		time.Sleep(10 * time.Millisecond)
	}
	return pendingIDs(s), time.Since(start)
}

func blockSession(ft *fleetTest, sess proto.Session) {
	sess.Status = proto.StatusBlocked
	ft.d.setSessions("mac", []proto.Session{sess})
	ft.s.syncScreenInteractions("mac", []proto.Session{sess})
}

func listCodex(s *Service, ixs ...proto.Interaction) {
	b, _ := json.Marshal(map[string]any{"interactions": ixs})
	s.syncCodexInteractions("mac", b)
}

// endedCards records the cards the channels are told are over, for a test to ask about.
func endedCards(s *Service) func(id string) bool {
	var mu sync.Mutex
	seen := map[string]bool{}
	s.fleetEnded = func(ix proto.Interaction, _ *proto.Resolution) {
		mu.Lock()
		seen[ix.ID] = true
		mu.Unlock()
	}
	return func(id string) bool {
		mu.Lock()
		defer mu.Unlock()
		return seen[id]
	}
}

// ---- screen cards ----

func TestScreenCardWait(t *testing.T) {
	claude, codex := proto.Session{Agent: "claude"}, proto.Session{Agent: "codex"}
	if screenCardWait(claude, false) != 0 || screenCardWait(claude, true) != 1500*time.Millisecond ||
		screenCardWait(codex, false) != 3*time.Second {
		t.Fatal("waits")
	}
}

// Blocked a moment before the dialog is drawn: the card made then has no dialog. Once the dialog shows, its card
// replaces that one.
func TestScreenCardUpgradesWhenTheDialogShows(t *testing.T) {
	ft, n := newCardFleet(t, false)
	s := ft.s
	shortScreenUpgrade(s)
	n.setScreen("  ✢ Percolating… (running PreToolUse hooks… 1/2 · 3s)\n")
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	got, _ := waitCards(s, 1, 2*time.Second)
	if len(got) != 1 || got["scr_mac_s1"] == "" {
		t.Fatalf("the card before the dialog: %v", got)
	}
	n.setScreen(pickModel)
	start := time.Now()
	for time.Since(start) < 2*time.Second {
		if ids := pendingIDs(s); len(ids) == 1 && ids["scr_mac_s1"] == "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	list := s.pendingInteractions()
	if len(list) != 1 || list[0].ID == "scr_mac_s1" || list[0].Dialog == nil || len(list[0].Dialog.Options) != 2 {
		t.Fatalf("after the dialog showed: %+v", list)
	}
}

// When a screen card is ended while upgradeScreenCard is polling, the replacement check
// must not re-finish or recreate the card (#5013).
func TestScreenCardUpgradeAbortedWhenCardEnded(t *testing.T) {
	ft, n := newCardFleet(t, false)
	s := ft.s
	shortScreenUpgrade(s)
	n.setScreen("  ✢ Percolating…\n")
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	got, _ := waitCards(s, 1, 2*time.Second)
	if len(got) != 1 || got["scr_mac_s1"] == "" {
		t.Fatalf("card before dialog: %v", got)
	}
	// Finish the card before the dialog is parsed.
	s.finishInteraction("scr_mac_s1")
	n.setScreen(pickModel)
	time.Sleep(100 * time.Millisecond)
	if list := s.pendingInteractions(); len(list) != 0 {
		t.Fatalf("expected no replacement card created after card was ended, got: %+v", list)
	}
}

// Cancellation stops future polls, but an accepted upgrade whose notification window already expired still rings its
// current card once. Earlier cancellation, an earlier ring, or a replaced card stays silent.
func TestScreenCardUpgradeCancellationRingsOnlyDueCurrentCard(t *testing.T) {
	for _, c := range []struct {
		name               string
		window             time.Duration
		announced, current bool
		want               int
	}{
		{"due current card", -time.Second, false, true, 1},
		{"already announced", -time.Second, true, true, 0},
		{"window still open", time.Hour, false, true, 0},
		{"card replaced", -time.Second, false, false, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			ft, _ := newCardFleet(t, false)
			s := ft.s
			s.fleet.upgradeEvery, s.fleet.upgradeFor = time.Hour, c.window
			runCtx, cancel := context.WithCancel(context.Background())
			s.running.Store(&runCtx)
			p := &pending{ix: proto.Interaction{ID: "scr_mac_s1", MachineID: "mac", SessionID: "s1", Kind: proto.IxScreen}, announced: c.announced}
			s.fleet.mu.Lock()
			s.ixMapLocked()[p.ix.ID] = p
			if !c.current {
				s.ixMapLocked()[p.ix.ID] = &pending{}
			}
			s.fleet.mu.Unlock()
			cancel()
			s.upgradeScreenCard("mac", "s1", p.ix.ID, p)
			if got := ft.pushesOf(assistantwire.NotifyInput, c.want, 50*time.Millisecond); len(got) != c.want {
				t.Fatalf("pushes after cancellation = %d, want %d", len(got), c.want)
			}
		})
	}
}

// Another manager's upgrade loop is still running at the default timing, as one an earlier test leaves behind:
// shortening this manager's timing touches nothing that loop reads, so -race has nothing to report (#4554), and this
// manager's card still upgrades.
func TestScreenCardUpgradeTimingIsPerService(t *testing.T) {
	other, on := newCardFleet(t, false)
	on.setScreen("  ✢ Percolating…\n")
	blockSession(other, proto.Session{SessionID: "s1", Agent: "claude"})
	if got, _ := waitCards(other.s, 1, 2*time.Second); len(got) != 1 {
		t.Fatalf("the other card: %v", got)
	}
	t.Cleanup(func() { // its loop ends at its next look
		other.d.setSessions("mac", []proto.Session{{SessionID: "s1", Agent: "claude", Status: proto.StatusWorking}})
	})

	ft, n := newCardFleet(t, false)
	s := ft.s
	shortScreenUpgrade(s)
	n.setScreen("  ✢ Percolating…\n")
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	if got, _ := waitCards(s, 1, 2*time.Second); len(got) != 1 || got["scr_mac_s1"] == "" {
		t.Fatalf("the card before the dialog: %v", got)
	}
	n.setScreen(pickModel)
	// upgradeScreenCard ends the card without a dialog, then makes the dialog's: wait for that one, not for a gap.
	for start := time.Now(); time.Since(start) < 2*time.Second; time.Sleep(10 * time.Millisecond) {
		if ids := pendingIDs(s); len(ids) == 1 && ids["scr_mac_s1"] == "" {
			break
		}
	}
	if list := s.pendingInteractions(); len(list) != 1 || list[0].ID == "scr_mac_s1" || list[0].Dialog == nil {
		t.Fatalf("after the dialog showed: %+v", list)
	}
}

func waitReplaced(s *Service, oldID string, d time.Duration) (string, bool) {
	for deadline := time.Now().Add(d); time.Now().Before(deadline); {
		if got := pendingIDs(s); len(got) == 1 && got[oldID] == "" {
			for id := range got {
				return id, true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return "", false
}

// noDialogPlaceholder blocks a session whose screen has no dialog yet, with the upgrade loop's timing set per
// Service before the card is made (#4665): the loop reads its timing as it starts.
func noDialogPlaceholder(t *testing.T, screenText string, poll, window time.Duration) (*fleetTest, *cardNode) {
	t.Helper()
	ft, n := newCardFleet(t, false)
	ft.s.fleet.upgradeEvery, ft.s.fleet.upgradeFor = poll, window
	n.setScreen(screenText)
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	waitCards(ft.s, 1, 2*time.Second)
	return ft, n
}

// A card made while its dialog is not drawn yet goes to the apps but does not ring (#4450: it used to ring twice).
func TestScreenCardWithoutDialogShowsWithoutAPush(t *testing.T) {
	ft, n := noDialogPlaceholder(t, "  ✢ Percolating… (running PreToolUse hooks… 1/2 · 3s)\n", 20*time.Millisecond, 600*time.Millisecond)
	told := ""
	for _, params := range ft.toldOf(assistantwire.EventFleetInteraction) {
		told += string(params)
	}
	if !strings.Contains(told, `"id":"scr_mac_s1"`) {
		t.Fatal("the placeholder was not put on the apps")
	}
	// Persona Link's summary lists it, but notifies only the real card, which the phone is pushed for.
	summary := func() link.Attention {
		agents, ixs, told := ft.s.fleetInputs()
		return linkAttention("en", agents, ixs, told)
	}
	if a := summary(); len(a.Items) != 1 || a.Items[0].InteractionID != "scr_mac_s1" || a.Items[0].Notify {
		t.Fatalf("summary with the placeholder %+v", a)
	}
	n.setScreen(pickModel)
	realID, ok := waitReplaced(ft.s, "scr_mac_s1", 2*time.Second)
	if !ok {
		t.Fatal("no real card")
	}
	waitUntil(t, func() bool {
		a := summary()
		return len(a.Items) == 1 && a.Items[0].InteractionID == realID && a.Items[0].Notify
	})
}

// The dialog answered on the computer: the agent moves on and its screen card goes, cancelled in the history too.
func TestScreenCardGoneWhenTheAgentMovesOnIsRecorded(t *testing.T) {
	ft, n := newCardFleet(t, true)
	s := ft.s
	n.setScreen(pickModel)
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	got, _ := waitCards(s, 1, 2*time.Second)
	var id string
	for k := range got {
		id = k
	}
	if id == "" {
		t.Fatal("no card")
	}
	ft.sessions("mac", proto.Session{SessionID: "s1", Agent: "claude", Status: proto.StatusWorking})
	if len(pendingIDs(s)) != 0 {
		t.Fatalf("card still pending: %v", pendingIDs(s))
	}
	if status, _, _ := ft.st.InteractionOutcome(t.Context(), id); status != "cancelled" {
		t.Fatalf("history says %q, want cancelled", status)
	}
}

// The agent moved on while its node was away and later asks the same dialog again: that is a new question, pushed.
func TestScreenCardAskedAgainAfterTheNodeWasAwayIsPushed(t *testing.T) {
	ft, n := newCardFleet(t, false)
	s := ft.s
	n.setScreen(pickModel)
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	if got, _ := waitCards(s, 1, 2*time.Second); len(got) != 1 {
		t.Fatalf("cards %v", got)
	}
	ft.d.setSessions("mac", []proto.Session{{SessionID: "s1", Agent: "claude", Status: proto.StatusUnknown}})
	s.NodeDetached("mac") // gone past the grace: its sessions show unknown, no card is read
	if got := pendingIDs(s); len(got) != 0 {
		t.Fatalf("cards while the node is away: %v", got)
	}
	ft.sessions("mac", proto.Session{SessionID: "s1", Agent: "claude", Status: proto.StatusWorking}) // back, working
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	if got, _ := waitCards(s, 1, 2*time.Second); len(got) != 1 {
		t.Fatalf("cards %v", got)
	}
	if pushes := ft.pushesOf(assistantwire.NotifyInput, 2, time.Second); len(pushes) != 2 {
		t.Fatalf("asked again after moving on: pushed %d times", len(pushes))
	}
}

// Away longer than resumeGrace: the dialog read once the node is back is pushed again.
func TestScreenCardBackAfterTheGraceIsPushed(t *testing.T) {
	old := resumeGrace
	resumeGrace = 50 * time.Millisecond
	t.Cleanup(func() { resumeGrace = old })
	ft, n := newCardFleet(t, false)
	s := ft.s
	n.setScreen(pickModel)
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	if got, _ := waitCards(s, 1, 2*time.Second); len(got) != 1 {
		t.Fatalf("cards %v", got)
	}
	ft.d.setSessions("mac", []proto.Session{{SessionID: "s1", Agent: "claude", Status: proto.StatusUnknown}})
	s.NodeDetached("mac")
	time.Sleep(2 * resumeGrace)
	blockSession(ft, proto.Session{SessionID: "s1", Agent: "claude"})
	if got, _ := waitCards(s, 1, 2*time.Second); len(got) != 1 {
		t.Fatalf("cards %v", got)
	}
	if pushes := ft.pushesOf(assistantwire.NotifyInput, 2, time.Second); len(pushes) != 2 {
		t.Fatalf("back after the grace: pushed %d times", len(pushes))
	}
}

// #5538: the card carries the revision the node will check the answer against. A node says its own (dialog_rev), so
// a hub and a node of different builds that parse the screen differently still agree; a node from before that says
// none and checks the old formula, which the card then carries.
func TestScreenCardTakesTheNodesRevision(t *testing.T) {
	d := screen.ParseDialog(pickModel)
	for _, c := range []struct {
		name, nodeSays, want string
	}{
		{"another build", "0123456789ab", "0123456789ab"},
		{"same build", d.Rev, d.Rev},
		{"before #5538", "", d.LegacyRev},
	} {
		t.Run(c.name, func(t *testing.T) {
			ft, n := newCardFleet(t, false)
			n.setScreen(pickModel)
			n.mu.Lock()
			n.dialogRev = &c.nodeSays
			n.mu.Unlock()
			blockSession(ft, proto.Session{SessionID: "s1"})
			got, _ := waitCards(ft.s, 1, 5*time.Second)
			if len(got) != 1 || got["scr_mac_s1_"+c.want] != proto.IxScreen {
				t.Fatalf("want the card scr_mac_s1_%s: %v", c.want, got)
			}
			for _, ix := range ft.s.pendingInteractions() {
				if ix.Dialog == nil || ix.Dialog.Rev != c.want {
					t.Fatalf("dialog %+v", ix.Dialog)
				}
				if p, err := screenAnswer("s1", ix.Dialog, proto.Resolution{Option: "2"}); err != nil || p["rev"] != c.want {
					t.Fatalf("answer %v %v", p, err)
				}
			}
		})
	}
}

// #5538: the node refuses an answer because it reads another dialog than the card's (prompt_changed). The card made
// next is named by what the node reads, so it is a new card. Named by the hub's own reading, it came back under the
// old id while the hub still read the old dialog, and every answer to it was refused again.
func TestScreenCardRebuiltAfterPromptChanged(t *testing.T) {
	ft, n := newCardFleet(t, false)
	n.setScreen(pickModel)
	first, second := "aaaaaaaaaaaa", "bbbbbbbbbbbb"
	n.mu.Lock()
	n.dialogRev = &first
	n.keysErr = &rpc.Error{Code: screen.PromptChanged, Message: "变了", Data: json.RawMessage(`{"delivered":"none"}`)}
	n.mu.Unlock()
	blockSession(ft, proto.Session{SessionID: "s1"})
	if got, _ := waitCards(ft.s, 1, 5*time.Second); len(got) != 1 || got["scr_mac_s1_"+first] != proto.IxScreen {
		t.Fatalf("first card %v", got)
	}
	n.mu.Lock()
	n.dialogRev = &second // the node now reads the screen as another dialog
	n.mu.Unlock()
	var re *rpc.Error
	if err := ft.s.resolve("scr_mac_s1_"+first, proto.Resolution{Option: "1"}); !errors.As(err, &re) || re.Code != screen.PromptChanged {
		t.Fatalf("answer: %v", err)
	}
	if got, _ := waitCards(ft.s, 1, 5*time.Second); len(got) != 1 || got["scr_mac_s1_"+second] != proto.IxScreen {
		t.Fatalf("after prompt_changed: %v", got)
	}
}

// #5265, #5266: Claude Code's safety check with a countdown makes one card, whose dialog says what the apps lay it out
// from: the command, the agent's words for it, what the warning is about, and when the countdown runs out by the hub's
// clock. The countdown moving (and the pane narrowed by a phone) is the same card: no new one, no second push.
func TestScreenCardForASafetyCheckWithACountdown(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile("../screen/testdata/claude-2.1.322-danger-shell-" + name + ".txt")
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	ft, n := newCardFleet(t, false)
	n.setScreen(read("120"))
	before := time.Now()
	blockSession(ft, proto.Session{SessionID: "s1"})
	got, _ := waitCards(ft.s, 1, 5*time.Second)
	if len(got) != 1 {
		t.Fatalf("cards %v", got)
	}
	ix := ft.s.pendingInteractions()[0]
	d := ix.Dialog
	if d == nil || !strings.HasPrefix(d.Command, "cd ./dist && F=(") || d.Description != "Count the example names in first-screen chunks" ||
		d.Risk != screen.RiskDelete || d.DeadlineAction != screen.DeadlineDeny {
		t.Fatalf("dialog %+v", d)
	}
	if left := d.DeadlineAt.Sub(before); left < time.Duration(d.DeadlineIn-2)*time.Second || left > time.Duration(d.DeadlineIn+2)*time.Second {
		t.Fatalf("deadline_at %v, %d s left at the read", d.DeadlineAt, d.DeadlineIn)
	}
	b, _ := json.Marshal(d)
	if !strings.Contains(string(b), `"deadline_at":"`) || !strings.Contains(string(b), `"command":"cd ./dist`) {
		t.Fatalf("json %s", b)
	}
	for _, later := range []string{"later-120", "later-57"} {
		n.setScreen(read(later))
		blockSession(ft, proto.Session{SessionID: "s1"})
		if now := pendingIDs(ft.s); len(now) != 1 || now[ix.ID] == "" {
			t.Fatalf("%s: cards %v, want only %s", later, now, ix.ID)
		}
	}
}

func TestScreenAnswer(t *testing.T) {
	d := screen.ParseDialog(dialogScreen)
	p, err := screenAnswer("s1", d, proto.Resolution{Option: "3"})
	if err != nil || p["rev"] != d.Rev || p["option"] != "3" || !slices.Equal(p["keys"].([]string), []string{"3"}) {
		t.Fatalf("%v %v", p, err)
	}
	p, _ = screenAnswer("s1", d, proto.Resolution{Option: "2", Text: "use the staging db"})
	if !slices.Equal(p["pre_keys"].([]string), []string{"2"}) || !slices.Equal(p["keys"].([]string), []string{"enter"}) || p["text"] != "use the staging db" {
		t.Fatalf("type something: %v", p)
	}
	p, _ = screenAnswer("s1", d, proto.Resolution{Keys: []string{"1"}})
	if p["rev"] != d.Rev || p["option"] != nil || !slices.Equal(p["keys"].([]string), []string{"1"}) {
		t.Fatalf("legacy: %v", p)
	}
	if _, err := screenAnswer("s1", d, proto.Resolution{Option: "9"}); err == nil {
		t.Fatal("no such option")
	}
	if p, _ := screenAnswer("s1", nil, proto.Resolution{Keys: []string{"esc"}}); p["rev"] != nil {
		t.Fatalf("%v", p)
	}
}

// The dialog changed under the answer: nothing pressed, the old card is gone (a new one follows); over the route the
// hub relays, 409 with the code, 410 once gone.
func TestResolveScreenChanged(t *testing.T) {
	ft := newFleetTestNoStore(t)
	s := ft.s
	changed := &rpc.Error{Code: screen.PromptChanged, Message: "变了", Data: json.RawMessage(`{"delivered":"none"}`)}
	put := func(id string) {
		s.fleet.mu.Lock()
		s.ixMapLocked()[id] = &pending{ix: proto.Interaction{ID: id, Kind: proto.IxScreen}, local: func(context.Context, proto.Resolution) error { return changed }}
		s.fleet.mu.Unlock()
	}
	put("scr_a")
	err := s.resolve("scr_a", proto.Resolution{Option: "1"})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != screen.PromptChanged {
		t.Fatalf("got %v", err)
	}
	if len(s.pendingInteractions()) != 0 {
		t.Fatal("the old card stays gone (a new one follows)")
	}
	if err := s.resolve("scr_a", proto.Resolution{}); !errors.Is(err, errNotPending) {
		t.Fatalf("second answer: %v", err)
	}
	put("scr_b")
	h := s.fleetHandler(http.NotFoundHandler())
	for _, want := range []int{http.StatusConflict, http.StatusGone} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/interactions/scr_b/resolve", strings.NewReader(`{"option":"1"}`)))
		if w.Code != want {
			t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body)
		}
		if want == http.StatusConflict && !strings.Contains(w.Body.String(), `"code":"prompt_changed"`) {
			t.Fatal(w.Body.String())
		}
	}
}

// ---- Codex cards ----

func TestCodexCardsFollowTheNodesList(t *testing.T) {
	ft, n := newCardFleet(t, false)
	s := ft.s
	s.fleet.mu.Lock()
	s.ixMapLocked()["scr_mac_s1"] = &pending{ix: proto.Interaction{ID: "scr_mac_s1", MachineID: "mac", SessionID: "s1", Kind: proto.IxScreen},
		local: func(context.Context, proto.Resolution) error { return nil }}
	s.fleet.mu.Unlock()
	ended := endedCards(s)
	q := proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour),
		Questions: []proto.Question{{Question: "Which color?", Options: []proto.QuestionOption{{Label: "Red"}}}}}
	a := proto.Interaction{ID: "cxa_1", SessionID: "s2", Kind: proto.IxPermission, Agent: "codex", Summary: "curl -sI https://example.com",
		ExpiresAt: time.Now().Add(time.Hour)}
	listCodex(s, q, a)
	if got := pendingIDs(s); len(got) != 2 || got["cxq_1"] != proto.IxQuestion || got["cxa_1"] != proto.IxPermission {
		t.Fatalf("pending %v", got)
	}
	if !ended("scr_mac_s1") {
		t.Fatal("the replaced screen card was never said to be over (the bot's card for it stays open)")
	}
	listCodex(s, q, a)
	listCodex(s, a)
	if got := pendingIDs(s); len(got) != 1 || got["cxa_1"] == "" {
		t.Fatalf("after the question was answered at the computer: %v", got)
	}
	yes := true
	if err := s.resolve("cxa_1", proto.Resolution{Allow: &yes, Always: true}); err != nil {
		t.Fatal(err)
	}
	n.mu.Lock()
	got := n.answers
	n.mu.Unlock()
	if len(got) != 1 || got[0]["id"] != "cxa_1" || got[0]["session_id"] != "s2" ||
		got[0]["resolution"].(map[string]any)["always"] != true {
		t.Fatalf("node asked %v", got)
	}
	if len(pendingIDs(s)) != 0 {
		t.Fatalf("still pending after the answer: %v", pendingIDs(s))
	}
}

type answerProvenanceDevices struct {
	Devices
	owners []bool
}

func (d *answerProvenanceDevices) CallNode(ctx context.Context, id, method string, params json.RawMessage) (json.RawMessage, error) {
	if method == "codex.answer" {
		d.owners = append(d.owners, devicewire.IsOwnerAnswer(ctx))
	}
	return d.Devices.CallNode(ctx, id, method, params)
}

func TestCodexCardAnswerCarriesBusProvenance(t *testing.T) {
	ft, _ := newCardFleet(t, false)
	d := &answerProvenanceDevices{Devices: ft.s.devs}
	ft.s.devs = d
	handler := ft.s.fleetHandler(http.NotFoundHandler())
	for _, owner := range []bool{true, false} {
		id := fmt.Sprintf("cxq_%t", owner)
		listCodex(ft.s, proto.Interaction{ID: id, SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex",
			ExpiresAt: time.Now().Add(time.Hour)})
		response, err := serveHTTP(context.Background(), handler, assistantwire.HTTPRequest{
			Method: http.MethodPost, Path: "/api/v1/interactions/" + id + "/resolve", OwnerAnswer: owner,
			Header: http.Header{"Owner-Answer": {"true"}},
			Body:   []byte(`{"answers":{"Color?":"Red"},"owner_input":true,"owner_answer":true}`),
		}, nil)
		if err != nil || response.Status != http.StatusOK {
			t.Fatalf("resolve with owner=%t: response %+v, error %v", owner, response, err)
		}
	}
	if !slices.Equal(d.owners, []bool{true, false}) {
		t.Errorf("node call provenance: %v", d.owners)
	}
}

// The node could not type the answer (a draft at the computer): the error reaches the app and the card stays; when the
// node says Codex no longer asks it, the card is gone.
func TestCodexCardAnswerRefused(t *testing.T) {
	ft, n := newCardFleet(t, false)
	s := ft.s
	listCodex(s, proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour)})
	n.mu.Lock()
	n.err = &rpc.Error{Code: screen.InputDraft, Message: "draft"}
	n.mu.Unlock()
	err := s.resolve("cxq_1", proto.Resolution{Answers: map[string]string{"Which color?": "Red"}})
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != screen.InputDraft {
		t.Fatalf("err %v", err)
	}
	if pendingIDs(s)["cxq_1"] == "" {
		t.Fatal("the card did not come back after a refused answer")
	}
	n.mu.Lock()
	n.err = &rpc.Error{Code: screen.PromptChanged, Message: "gone"}
	n.mu.Unlock()
	_ = s.resolve("cxq_1", proto.Resolution{Answers: map[string]string{"Which color?": "Red"}})
	if len(pendingIDs(s)) != 0 {
		t.Fatalf("a card Codex no longer asks came back: %v", pendingIDs(s))
	}
}

// A refused answer leaves the card on the apps: the last thing the hub hears of it is the card again, not that it is
// over, and the bot's card for it stays open.
func TestCodexCardRefusedStaysOnTheApps(t *testing.T) {
	ft, n := newCardFleet(t, false)
	s := ft.s
	ended := endedCards(s)
	listCodex(s, proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour)})
	n.mu.Lock()
	n.err = &rpc.Error{Code: screen.InputDraft, Message: "draft"}
	n.mu.Unlock()
	if err := s.resolve("cxq_1", proto.Resolution{Answers: map[string]string{"Which color?": "Red"}}); err == nil {
		t.Fatal("a refused answer reported no error")
	}
	last := ""
	ft.f.mu.Lock()
	for _, tl := range ft.f.told {
		switch tl.event {
		case assistantwire.EventFleetInteraction:
			if strings.Contains(string(tl.params), `"id":"cxq_1"`) {
				last = "interaction"
			}
		case assistantwire.EventFleetResolved:
			if strings.Contains(string(tl.params), `"id":"cxq_1"`) {
				last = "interaction_resolved"
			}
		}
	}
	ft.f.mu.Unlock()
	if last != "interaction" {
		t.Fatalf("the hub last heard %q of a card that is still open", last)
	}
	if ended("cxq_1") {
		t.Fatal("the bot was told a card that is still open is over")
	}
}

// A node that reconnects lists its open cards again: they come back without ringing the phone a second time.
func TestCodexCardsRelistedQuietly(t *testing.T) {
	ft, _ := newCardFleet(t, false)
	s := ft.s
	q := proto.Interaction{ID: "cxq_1", SessionID: "s1", Kind: proto.IxQuestion, Agent: "codex", ExpiresAt: time.Now().Add(time.Hour)}
	listCodex(s, q)
	s.NodeDetached("mac") // the node went away
	if len(pendingIDs(s)) != 0 {
		t.Fatal("not cancelled with its node")
	}
	listCodex(s, q)
	if pendingIDs(s)["cxq_1"] == "" {
		t.Fatal("not listed again")
	}
	if got := ft.pushesOf(assistantwire.NotifyInput, 2, 200*time.Millisecond); len(got) != 1 {
		t.Fatalf("pushed %d times", len(got))
	}
}

// ---- plans answered in the terminal ----

func planItems(at time.Time) []proto.Item {
	return []proto.Item{
		{ID: "call#0", Seq: 10, TS: at.Add(-time.Millisecond), Role: "assistant", Kind: proto.KindToolUse,
			ToolName: "ExitPlanMode", ToolUseID: "plan-call"},
		{ID: "result#0", Seq: 11, TS: at.Add(time.Second), Role: "user", Kind: proto.KindToolResult,
			ToolUseID: "plan-call", Text: "The plan was answered in the terminal."},
	}
}

func TestPlanCallFinishedRequiresTheMatchingToolResult(t *testing.T) {
	at := time.Now()
	ix := proto.Interaction{CreatedAt: at}
	valid := planItems(at)
	for _, tc := range []struct {
		name  string
		items []proto.Item
		id    string
		want  bool
	}{
		{"id omitted by PermissionRequest", valid, "", true},
		{"exact tool id", valid, "plan-call", true},
		{"wrong tool id", valid, "other-call", false},
		{"still waiting, even if working", valid[:1], "", false},
		{"model says approved, not a result", []proto.Item{valid[0], {Role: "assistant", Kind: proto.KindText, Text: "User approved."}}, "", false},
		{"human says approved, not a result", []proto.Item{valid[0], {Role: "user", Kind: proto.KindText, Text: "Approved."}}, "", false},
		{"another tool finished", []proto.Item{valid[0], {Role: "user", Kind: proto.KindToolResult, ToolUseID: "bash", TS: at.Add(time.Second)}}, "", false},
		{"old plan", planItems(at.Add(-time.Minute)), "", false},
		{"plan created later", planItems(at.Add(time.Minute)), "", false},
		{"two candidate calls", append(append([]proto.Item{}, valid...), proto.Item{Role: "assistant", Kind: proto.KindToolUse,
			ToolName: "ExitPlanMode", ToolUseID: "other-plan", TS: at.Add(-2 * time.Millisecond)}), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ix.ToolUseID = tc.id
			if got := planCallFinished(ix, tc.items); got != tc.want {
				t.Fatalf("finished = %v, want %v", got, tc.want)
			}
		})
	}
	for _, mutate := range []func([]proto.Item){
		func(x []proto.Item) { x[0].TS = time.Time{} },
		func(x []proto.Item) { x[1].TS = time.Time{} },
		func(x []proto.Item) { x[1].TS = at.Add(-time.Second) },
		func(x []proto.Item) { x[1].Role = "assistant" },
	} {
		x := planItems(at)
		mutate(x)
		if planCallFinished(proto.Interaction{CreatedAt: at}, x) {
			t.Fatal("missing or earlier completion cannot answer a pending plan")
		}
	}
	valid[1].IsError = true
	if !planCallFinished(proto.Interaction{CreatedAt: at}, valid) {
		t.Fatal("an ended, rejected call must not leave an approval card")
	}
}

// planFleet is a manager whose machine's node reads its transcript pages with read.
func planFleet(t *testing.T, store bool, read func(int) ([]proto.Item, bool, error)) *fleetTest {
	t.Helper()
	ft := newFleetTestNoStore(t)
	if store {
		ft = newFleetTest(t)
	}
	ft.d.mu.Lock()
	ft.d.machines["mac"].call = func(method string, params json.RawMessage) (any, error) {
		if method != "session.items" {
			return nil, fmt.Errorf("unexpected %s", method)
		}
		var p struct {
			Before int `json:"before_seq"`
		}
		_ = json.Unmarshal(params, &p)
		items, more, err := read(p.Before)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items, "has_more": more}, nil
	}
	ft.d.mu.Unlock()
	return ft
}

func putPending(s *Service, p *pending) {
	s.fleet.mu.Lock()
	s.ixMapLocked()[p.ix.ID] = p
	s.fleet.mu.Unlock()
}

func TestTerminalDismissalHistoryNamesTerminal(t *testing.T) {
	ft := newFleetTest(t)
	at := time.Now()
	ix := proto.Interaction{ID: "terminal-plan", MachineID: "mac", SessionID: "session", Kind: proto.IxPlan,
		ToolName: "ExitPlanMode", CreatedAt: at, ExpiresAt: at.Add(time.Hour), Resumable: true}
	putPending(ft.s, &pending{ix: ix})
	ft.s.record(ix)
	ft.event("mac", "interaction.dismiss", map[string]any{"id": ix.ID})
	status, res, err := ft.st.InteractionOutcome(context.Background(), ix.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status != "dismissed" || res == nil || res.AnsweredBy != "terminal" {
		t.Fatalf("terminal dismissal history = %q %+v, want dismissed with answered_by terminal", status, res)
	}
}

func TestTerminalPlanResultClearsTheCardWithoutApprovingAnything(t *testing.T) {
	for _, trigger := range []string{"sessions", "revs"} {
		t.Run(trigger, func(t *testing.T) {
			at := time.Now()
			ft := planFleet(t, true, func(int) ([]proto.Item, bool, error) { return planItems(at), false, nil })
			s := ft.s
			ix := proto.Interaction{ID: "plan", MachineID: "mac", SessionID: "session", Kind: proto.IxPlan,
				ToolName: "ExitPlanMode", CreatedAt: at, ExpiresAt: at.Add(time.Hour), Resumable: true}
			p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
			putPending(s, p)
			s.record(ix)
			if trigger == "sessions" {
				ft.sessions("mac", proto.Session{SessionID: "session", Status: proto.StatusWorking})
			} else {
				ft.d.setSessions("mac", []proto.Session{{SessionID: "session", Status: proto.StatusWorking}})
				if !ft.event("mac", "sessions.revs", map[string]any{"revs": map[string]int{"session": 1234}}) {
					t.Fatal("the hub no longer gets the revs")
				}
			}
			ft.waitPending(0)
			select {
			case r := <-p.done:
				if !r.Dismiss || r.Approve != nil || r.Allow != nil || r.Mode != "" {
					t.Fatalf("external completion must only release the obsolete hook: %+v", r)
				}
			case <-time.After(time.Second):
				t.Fatal("the old hook stayed blocked")
			}
			if sess := s.decorated("mac")[0]; sess.PendingInteractions != 0 || sess.State == proto.StateNeedsYou {
				t.Fatalf("session still asks for approval: %+v", sess)
			}
			status, res, _ := ft.st.InteractionOutcome(context.Background(), ix.ID)
			if status != "dismissed" || res == nil || res.AnsweredBy != "terminal" || res.Approve != nil || res.Mode != "" {
				t.Fatalf("terminal decision must name its source without a made-up phone approval: %s %+v", status, res)
			}
			// finished tells the hub after the hook is released: wait for it rather than look once
			waitFor(t, "the hub to get interaction_resolved", func() bool { return len(ft.toldOf(assistantwire.EventFleetResolved)) > 0 })
		})
	}
}

func TestPlanReconcileCrossesAPageAndKeepsUnrelatedApprovals(t *testing.T) {
	at := time.Now()
	items := planItems(at)
	ft := planFleet(t, false, func(before int) ([]proto.Item, bool, error) {
		if before == 0 {
			return items[1:], true, nil
		}
		return items[:1], false, nil
	})
	s := ft.s
	putPending(s, &pending{ix: proto.Interaction{ID: "old-plan", MachineID: "mac", SessionID: "session", Kind: proto.IxPlan,
		ToolName: "ExitPlanMode", CreatedAt: at}, done: make(chan proto.Resolution, 1)})
	permission := &pending{ix: proto.Interaction{ID: "permission", MachineID: "mac", SessionID: "session", Kind: proto.IxPermission}}
	question := &pending{ix: proto.Interaction{ID: "question", MachineID: "mac", SessionID: "session", Kind: proto.IxQuestion}}
	putPending(s, permission)
	putPending(s, question)
	s.syncPlanInteractions("mac")
	ft.waitPending(2)
	s.fleet.mu.Lock()
	defer s.fleet.mu.Unlock()
	if s.fleet.interactions["permission"] != permission || s.fleet.interactions["question"] != question {
		t.Fatal("finishing a plan must not answer unrelated questions or permissions")
	}
}

// #3563: Claude Code does not end a PermissionRequest hook whose dialog was answered in the terminal: the tool's card
// stayed on the phone until the hook's timeout. Its tool call's result ends it, as a plan's does; a permission card
// that names no tool call is never guessed at, and one the phone just answered keeps its answer.
func TestTerminalPermissionResultClearsTheCard(t *testing.T) {
	at := time.Now()
	items := []proto.Item{
		{ID: "call#0", Seq: 10, TS: at.Add(-time.Millisecond), Role: "assistant", Kind: proto.KindToolUse, ToolName: "Bash", ToolUseID: "bash-call"},
		{ID: "result#0", Seq: 11, TS: at.Add(time.Second), Role: "user", Kind: proto.KindToolResult, ToolUseID: "bash-call", Text: "ok"},
	}
	ft := planFleet(t, true, func(int) ([]proto.Item, bool, error) { return items, false, nil })
	s := ft.s
	ix := proto.Interaction{ID: "perm", MachineID: "mac", SessionID: "session", Kind: proto.IxPermission, ToolName: "Bash",
		ToolUseID: "bash-call", CreatedAt: at, ExpiresAt: at.Add(time.Hour), Resumable: true}
	p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
	putPending(s, p)
	s.record(ix)
	noID := &pending{ix: proto.Interaction{ID: "perm-no-id", MachineID: "mac", SessionID: "session", Kind: proto.IxPermission,
		ToolName: "Bash", CreatedAt: at}, done: make(chan proto.Resolution, 1)}
	putPending(s, noID)
	ft.sessions("mac", proto.Session{SessionID: "session", Status: proto.StatusWorking})
	ft.waitPending(1)
	select {
	case r := <-p.done:
		if !r.Dismiss || r.Allow != nil || r.Always {
			t.Fatalf("a terminal answer must only release the hook: %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("the hook stayed blocked")
	}
	if status, res, _ := ft.st.InteractionOutcome(context.Background(), ix.ID); status != "dismissed" || res == nil ||
		res.AnsweredBy != "terminal" || res.Allow != nil || res.Always {
		t.Fatalf("terminal decision must name its source without a made-up phone answer: %s %+v", status, res)
	}
	s.fleet.mu.Lock()
	kept := s.fleet.interactions["perm-no-id"] == noID
	s.fleet.mu.Unlock()
	if !kept {
		t.Fatal("a permission card without its tool call's id was dismissed by another call's result")
	}
}

func TestPermissionCallIsNeverGuessed(t *testing.T) {
	at := time.Now()
	// A plan's call and result right before and after the card: a plan without an id takes them, a permission does not.
	if planCallFinished(proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", CreatedAt: at}, planItems(at)) {
		t.Fatal("a permission card without its tool call's id was ended by another call's result")
	}
	withExactInput := &pending{ix: proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash",
		ToolInput: json.RawMessage(`{"command":"synthetic"}`), Resumable: true}}
	if !terminalReconciled(withExactInput) {
		t.Fatal("a Claude Code permission with exact input was not eligible for reconciliation")
	}
	for _, p := range []*pending{
		{ix: proto.Interaction{Kind: proto.IxPermission, ToolUseID: "c", Resumable: true, Agent: "codex"}},
		{ix: proto.Interaction{Kind: proto.IxPermission, ToolUseID: "c"}},
		{ix: proto.Interaction{Kind: proto.IxPermission, Resumable: true}},
		{ix: proto.Interaction{Kind: proto.IxPermission, ToolUseID: "c", Resumable: true}, prepared: &preparedApproval{}},
	} {
		if terminalReconciled(p) {
			t.Fatalf("a permission without a safe Claude Code call identity was reconciled: %+v", p.ix)
		}
	}
}

func TestNonFinalApprovalResultsNeverFinishPermissionCall(t *testing.T) {
	at := time.Now()
	input := json.RawMessage(`{"command":"printf example"}`)
	for _, msg := range []string{
		"PreToolUse:Bash hook error: " + approvalops.WaitingText + " The command was not executed.",
		"PreToolUse:Bash hook error: " + approvalops.ClarifyingText,
	} {
		items := []proto.Item{
			{ID: "call#0", Seq: 10, TS: at.Add(-time.Millisecond), Role: "assistant", Kind: proto.KindToolUse,
				ToolName: "Bash", ToolUseID: "bash-call", ToolInput: input},
			{ID: "result#0", Seq: 11, TS: at.Add(time.Second), Role: "user", Kind: proto.KindToolResult,
				ToolUseID: "bash-call", IsError: true, Text: msg},
			{ID: "text#0", Seq: 12, TS: at.Add(2 * time.Second), Role: "assistant", Kind: proto.KindText,
				Text: "Please confirm the pending card."},
		}
		if planCallFinished(proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", ToolUseID: "bash-call", ToolInput: input, CreatedAt: at}, items) {
			t.Fatalf("non-final result finished call with ToolUseID: %q", msg)
		}
		if planCallFinished(proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", ToolInput: input, CreatedAt: at}, items) {
			t.Fatalf("non-final result finished call matched by ToolInput: %q", msg)
		}
		// checkPlanInteraction concatenates pages newest first: newer-page assistant text must not win before the older
		// page's non-final WaitingText / ClarifyingText result is inspected (#12918).
		newestFirst := []proto.Item{
			items[2],
			items[0],
			items[1],
		}
		if planCallFinished(proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", ToolInput: input, CreatedAt: at}, newestFirst) {
			t.Fatalf("newest-page-first assistant text bypassed older non-final result: %q", msg)
		}
	}
	// Genuine terminal command output that merely quotes WaitingText (for example grep over approval.go) still reconciles.
	grepItems := []proto.Item{
		{ID: "call#0", Seq: 10, TS: at.Add(-time.Millisecond), Role: "assistant", Kind: proto.KindToolUse,
			ToolName: "Bash", ToolUseID: "bash-call", ToolInput: input},
		{ID: "result#0", Seq: 11, TS: at.Add(time.Second), Role: "user", Kind: proto.KindToolResult,
			ToolUseID: "bash-call", Text: `server/internal/approval/approval.go:51:const WaitingText = "` + approvalops.WaitingText + `"`},
	}
	if !planCallFinished(proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", ToolUseID: "bash-call", ToolInput: input, CreatedAt: at}, grepItems) {
		t.Fatal("ordinary stdout quoting WaitingText was misclassified as a non-final hook release")
	}
}

func terminalPermissionItems(at time.Time, input json.RawMessage) []proto.Item {
	return []proto.Item{
		{ID: "call#0", Seq: 10, TS: at.Add(-time.Millisecond), Role: "assistant", Kind: proto.KindToolUse,
			ToolName: "Bash", ToolUseID: "bash-call", ToolInput: input},
		{ID: "result#0", Seq: 11, TS: at.Add(2 * time.Minute), Role: "user", Kind: proto.KindToolResult,
			ToolUseID: "bash-call", IsError: true, Text: "Permission for this command was denied by a built-in Claude Code safety check, not by the user."},
	}
}

func TestTerminalPermissionWithoutIDMatchesExactCall(t *testing.T) {
	at := time.Now()
	input := json.RawMessage(`{"command":"rm -f /tmp/persona-stale-permission-test/*","description":"Remove synthetic test output"}`)
	ix := proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", ToolInput: input, CreatedAt: at}
	for _, tc := range []struct {
		name  string
		input json.RawMessage
		items func() []proto.Item
		want  bool
	}{
		{"built-in denial", input, func() []proto.Item { return terminalPermissionItems(at, input) }, true},
		{"equivalent JSON", json.RawMessage(`{ "description": "Remove synthetic test output", "command": "rm -f /tmp/persona-stale-permission-test/*" }`),
			func() []proto.Item { return terminalPermissionItems(at, input) }, true},
		{"still waiting", input, func() []proto.Item { return terminalPermissionItems(at, input)[:1] }, false},
		{"different input", input, func() []proto.Item {
			return terminalPermissionItems(at, json.RawMessage(`{"command":"rm -f /tmp/another-test-directory/*"}`))
		}, false},
		{"two candidate calls", input, func() []proto.Item {
			items := terminalPermissionItems(at, input)
			return append(items, proto.Item{ID: "call#1", Seq: 9, TS: at.Add(-2 * time.Millisecond), Role: "assistant", Kind: proto.KindToolUse,
				ToolName: "Bash", ToolUseID: "other-bash-call", ToolInput: input})
		}, false},
		{"old call", input, func() []proto.Item {
			return terminalPermissionItems(at.Add(-time.Minute), input)
		}, false},
		{"different tool", input, func() []proto.Item {
			items := terminalPermissionItems(at, input)
			items[0].ToolName = "Write"
			return items
		}, false},
		{"unrelated activity without the call", input, func() []proto.Item {
			return []proto.Item{{ID: "text#0", Seq: 12, TS: at.Add(time.Second), Role: "assistant", Kind: proto.KindText, Text: "Continuing."}}
		}, false},
		{"unrelated parallel result", input, func() []proto.Item {
			items := terminalPermissionItems(at, input)[:1]
			return append(items, proto.Item{ID: "result#1", Seq: 11, TS: at.Add(time.Minute), Role: "user", Kind: proto.KindToolResult, ToolUseID: "other-call"})
		}, false},
		{"activity in the calling message", input, func() []proto.Item {
			items := terminalPermissionItems(at, input)[:1]
			return append(items, proto.Item{ID: "text#0", Seq: 11, TS: at.Add(-time.Millisecond), Role: "assistant", Kind: proto.KindText, Text: "Same message."})
		}, false},
		{"missing call timestamp", input, func() []proto.Item {
			items := terminalPermissionItems(at, input)
			items[0].TS = time.Time{}
			return items
		}, false},
		{"missing input", nil, func() []proto.Item { return terminalPermissionItems(at, input) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ix.ToolInput = tc.input
			if got := planCallFinished(ix, tc.items()); got != tc.want {
				t.Fatalf("finished = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTerminalPermissionWithoutIDClearsBuiltInDenial(t *testing.T) {
	at := time.Now()
	input := json.RawMessage(`{"command":"rm -f /tmp/persona-stale-permission-test/*"}`)
	items := terminalPermissionItems(at, input)
	ft := planFleet(t, false, func(int) ([]proto.Item, bool, error) { return items, false, nil })
	s := ft.s
	ix := proto.Interaction{ID: "builtin-denial", MachineID: "mac", SessionID: "session", Kind: proto.IxPermission,
		ToolName: "Bash", ToolInput: input, CreatedAt: at, ExpiresAt: at.Add(time.Hour), Resumable: true}
	p := &pending{ix: ix, done: make(chan proto.Resolution, 1), route: &askRoute{routedAt: at}}
	putPending(s, p)
	ft.d.setSessions("mac", []proto.Session{{MachineID: "mac", SessionID: "session", Status: proto.StatusWorking}})
	s.checkPlanInteraction("mac", p)
	if got := s.pendingInteractions(); len(got) != 0 {
		t.Fatalf("built-in denial left the permission card pending: %+v", got)
	}
	select {
	case r := <-p.done:
		if !r.Dismiss || r.Allow != nil || r.Always {
			t.Fatalf("runtime denial must only dismiss the obsolete card: %+v", r)
		}
	default:
		t.Fatal("the permission hook stayed blocked")
	}
	rows := s.decorated("mac")
	if len(rows) != 1 || rows[0].PendingInteractions != 0 || rows[0].State == proto.StateNeedsYou {
		t.Fatalf("the completed session still needs the owner: %+v", rows)
	}
	before := len(ft.toldOf(assistantwire.EventFleetInteraction))
	s.askTick(at.Add(askOwnerOnlyDeadline))
	if got := len(ft.toldOf(assistantwire.EventFleetInteraction)); got != before {
		t.Fatal("the obsolete card escalated after the runtime denied it")
	}
}

func TestPermissionContinuedPastCallClearsCard(t *testing.T) {
	for _, later := range []proto.Item{
		{ID: "text#0", Seq: 11, Role: "assistant", Kind: proto.KindText, Text: "Continuing without that removal."},
		{ID: "thinking#0", Seq: 11, Role: "assistant", Kind: proto.KindThinking, Text: "Trying another read-only approach."},
		{ID: "call#1", Seq: 11, Role: "assistant", Kind: proto.KindToolUse, ToolName: "Read", ToolUseID: "read-call"},
	} {
		t.Run(later.Kind, func(t *testing.T) {
			at := time.Now()
			input := json.RawMessage(`{"command":"rm -f /tmp/persona-stale-permission-test/*"}`)
			later.TS = at.Add(2 * time.Minute)
			items := append(terminalPermissionItems(at, input)[:1], later)
			if !planCallFinished(proto.Interaction{Kind: proto.IxPermission, ToolName: "Bash", ToolInput: input, CreatedAt: at}, items) {
				t.Fatal("later session activity did not finish the obsolete permission call")
			}
			ft := planFleet(t, false, func(int) ([]proto.Item, bool, error) { return items, false, nil })
			ix := proto.Interaction{ID: "continued-" + later.Kind, MachineID: "mac", SessionID: "session", Kind: proto.IxPermission,
				ToolName: "Bash", ToolInput: input, CreatedAt: at, ExpiresAt: at.Add(time.Hour), Resumable: true}
			p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
			putPending(ft.s, p)
			ft.s.checkPlanInteraction("mac", p)
			if got := ft.s.pendingInteractions(); len(got) != 0 {
				t.Fatalf("continued session left the permission card pending: %+v", got)
			}
			select {
			case r := <-p.done:
				if !r.Dismiss || r.Allow != nil || r.Always {
					t.Fatalf("continued activity must not invent an approval: %+v", r)
				}
			default:
				t.Fatal("the permission hook stayed blocked")
			}
		})
	}
}

func TestTerminalPermissionWithoutIDKeepsWaitingCard(t *testing.T) {
	at := time.Now()
	input := json.RawMessage(`{"command":"rm -f /tmp/persona-stale-permission-test/*"}`)
	items := terminalPermissionItems(at, input)[:1]
	ft := planFleet(t, false, func(int) ([]proto.Item, bool, error) { return items, false, nil })
	ix := proto.Interaction{ID: "really-waiting", MachineID: "mac", SessionID: "session", Kind: proto.IxPermission,
		ToolName: "Bash", ToolInput: input, CreatedAt: at, ExpiresAt: at.Add(time.Hour), Resumable: true}
	p := &pending{ix: ix, done: make(chan proto.Resolution, 1), route: &askRoute{routedAt: at}}
	putPending(ft.s, p)
	ft.s.checkPlanInteraction("mac", p)
	if _, ok := ft.s.pendingIx(ix.ID); !ok {
		t.Fatal("a permission call with no result or later activity was dismissed")
	}
	select {
	case r := <-p.done:
		t.Fatalf("a genuinely pending hook was released: %+v", r)
	default:
	}
	before := len(ft.toldOf(assistantwire.EventFleetInteraction))
	ft.s.askTick(at.Add(askOwnerOnlyDeadline))
	if got := len(ft.toldOf(assistantwire.EventFleetInteraction)); got <= before {
		t.Fatal("a genuinely waiting permission card did not escalate at its deadline")
	}
}

func TestTerminalReconcileKeepsAPhoneAnswer(t *testing.T) {
	at := time.Now()
	ft := planFleet(t, false, func(int) ([]proto.Item, bool, error) { return planItems(at), false, nil })
	s := ft.s
	p := &pending{ix: proto.Interaction{ID: "plan", MachineID: "mac", SessionID: "session", Kind: proto.IxPlan,
		ToolName: "ExitPlanMode", ToolUseID: "plan-call", CreatedAt: at}, done: make(chan proto.Resolution, 1), claimed: true}
	putPending(s, p)
	s.checkPlanInteraction("mac", p)
	s.fleet.mu.Lock()
	still := s.fleet.interactions["plan"] == p
	s.fleet.mu.Unlock()
	if !still {
		t.Fatal("a card the phone answered was dismissed under its answer")
	}
	select {
	case r := <-p.done:
		t.Fatalf("the hook got a dismiss instead of the phone's answer: %+v", r)
	default:
	}
}

func TestAnInFlightPlanReadDoesNotDismissANewerCard(t *testing.T) {
	at := time.Now()
	started, release := make(chan struct{}), make(chan struct{})
	ft := planFleet(t, false, func(int) ([]proto.Item, bool, error) {
		close(started)
		<-release
		return planItems(at), false, nil
	})
	s := ft.s
	old := &pending{ix: proto.Interaction{ID: "plan", Kind: proto.IxPlan, ToolName: "ExitPlanMode", CreatedAt: at}}
	putPending(s, old)
	done := make(chan struct{})
	go func() { s.checkPlanInteraction("mac", old); close(done) }()
	<-started
	newer := &pending{ix: proto.Interaction{ID: "plan", Kind: proto.IxPlan, ToolName: "ExitPlanMode", CreatedAt: at.Add(time.Minute)}}
	putPending(s, newer)
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("read did not finish")
	}
	s.fleet.mu.Lock()
	defer s.fleet.mu.Unlock()
	if s.fleet.interactions["plan"] != newer {
		t.Fatal("an old read removed a new pending plan")
	}
}

func TestPlanProbeFailureKeepsTheCardAndIsThrottled(t *testing.T) {
	var calls atomic.Int32
	ft := planFleet(t, false, func(int) ([]proto.Item, bool, error) {
		calls.Add(1)
		return nil, false, fmt.Errorf("node read failed")
	})
	s := ft.s
	p := &pending{ix: proto.Interaction{ID: "plan", MachineID: "mac", SessionID: "session", Kind: proto.IxPlan,
		ToolName: "ExitPlanMode", CreatedAt: time.Now()}, done: make(chan proto.Resolution, 1)}
	putPending(s, p)
	s.syncPlanInteractions("mac")
	deadline := time.Now().Add(time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 20; i++ {
		s.syncPlanInteractions("mac")
	}
	if calls.Load() != 1 {
		t.Fatalf("one pending plan read, got %d", calls.Load())
	}
	ft.waitPending(1)
	select {
	case r := <-p.done:
		t.Fatalf("a failed read must not release the hook: %+v", r)
	default:
	}
}

// A Codex approval answered from the phone and asked again later rings again (#4105); listed again at once (the
// screen lagging the answer) it stays quiet.
func TestCodexCardAnsweredOnThePhoneAskedAgainIsAnnounced(t *testing.T) {
	old := codexAskedAgain
	codexAskedAgain = 50 * time.Millisecond
	t.Cleanup(func() { codexAskedAgain = old })
	ft, _ := newCardFleet(t, false)
	s := ft.s
	a := proto.Interaction{ID: "cxa_1", SessionID: "s2", Kind: proto.IxPermission, Agent: "codex", Summary: "npm test",
		ExpiresAt: time.Now().Add(time.Hour)}
	yes := true
	listCodex(s, a)
	if got := ft.pushesOf(assistantwire.NotifyInput, 1, time.Second); len(got) != 1 {
		t.Fatalf("first: pushed %d times", len(got))
	}
	if err := s.resolve("cxa_1", proto.Resolution{Allow: &yes}); err != nil {
		t.Fatal(err)
	}
	listCodex(s)
	time.Sleep(2 * codexAskedAgain)
	listCodex(s, a)
	if got := ft.pushesOf(assistantwire.NotifyInput, 2, time.Second); len(got) != 2 {
		t.Fatalf("asked again later: pushed %d times", len(got))
	}
}

// codexEnded is pruned on its own, also for an id codexAnnounced no longer holds.
func TestCodexEndedPrunedOnItsOwn(t *testing.T) {
	ft, _ := newCardFleet(t, false)
	s := ft.s
	s.fleet.mu.Lock()
	s.fleet.codexEnded = map[string]time.Time{"cxa_old": time.Now().Add(-2 * codexAnnouncedFor), "cxa_new": time.Now()}
	s.fleet.mu.Unlock()
	listCodex(s)
	s.fleet.mu.Lock()
	_, old := s.fleet.codexEnded["cxa_old"]
	_, fresh := s.fleet.codexEnded["cxa_new"]
	s.fleet.mu.Unlock()
	if old || !fresh {
		t.Fatalf("pruned wrong: old kept %v, new kept %v", old, fresh)
	}
}

// shortScreenUpgrade has s's screen cards made without a dialog look again every 50 ms for a second
// (upgradeScreenCard). Call it before s gets a session list: each loop reads it as it starts.
func shortScreenUpgrade(s *Service) {
	s.fleet.upgradeEvery, s.fleet.upgradeFor = 50*time.Millisecond, time.Second
}

// Claude Code's Bypass Permissions confirmation on a fresh pane (blank rows under it down to the bottom) is a card
// with its two choices, answered with the keys that pick each, and nothing is pressed until the user picks one.
// Before, no dialog was read off that screen, so the card offered nothing to choose.
func TestScreenCardOffersTheBypassConfirmationsChoices(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "screen", "testdata", "claude-2.1.322-bypass-confirm-160.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ft, n := newCardFleet(t, false)
	n.setScreen(string(b))
	blockSession(ft, proto.Session{SessionID: "s1"})
	if got, _ := waitCards(ft.s, 1, 5*time.Second); len(got) != 1 {
		t.Fatalf("cards %v", got)
	}
	ix := ft.s.pendingInteractions()[0]
	if ix.Kind != proto.IxScreen || ix.Dialog == nil || len(ix.Dialog.Options) != 2 ||
		ix.Dialog.Options[0].Label != "No, exit" || ix.Dialog.Options[1].Label != "Yes, I accept" {
		t.Fatalf("card %+v dialog %+v", ix, ix.Dialog)
	}
	p, err := screenAnswer("s1", ix.Dialog, proto.Resolution{Option: ix.Dialog.Options[1].Key})
	if err != nil || !slices.Equal(p["keys"].([]string), []string{"down", "enter"}) {
		t.Fatalf("answer %v %v", p, err)
	}
	if !strings.HasPrefix(ix.ID, "scr_mac_s1_") || ix.Dialog.Rev == "" {
		t.Errorf("id %s rev %q", ix.ID, ix.Dialog.Rev)
	}
}
