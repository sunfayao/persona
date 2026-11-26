package node

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/host"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/screen"
)

func traeQuestionRevision(id, pane string, q *screen.TraeQuestion) string {
	return screen.TraeQuestionRev + shortHash(id+"\x00"+pane+"\x00"+q.Dialog.Rev)
}

func (n *Node) traeQuestionDialog(id, text string) *screen.Dialog {
	a, ok := n.agent(id)
	if !ok || a.Agent != traexAgent {
		return nil
	}
	if q := screen.ReadTraeQuestion(text); q != nil {
		d := q.Dialog
		d.Rev = traeQuestionRevision(id, a.PaneID, q)
		if q.FreeText != "" || host.ParsePane(a.PaneID).Kind != host.Herdr {
			d.Options = nil // typed text and a host without guarded key readback stay in the terminal
		}
		return &d
	}
	return nil
}

// answerTraeQuestion resolves only the active, explicitly chosen native page. TRAE's digits are also text
// when its free-text row has focus. Read back each arrow and text paste before pressing Enter exactly once.
func (n *Node) answerTraeQuestion(ctx context.Context, id, revision, option, text string) error {
	defer n.typing.lock(id)()
	unconfirmed := func() error {
		return uncertain(errors.New(i18n.T(i18n.From(ctx), "rpc-errors.traex_question_unconfirmed")))
	}
	changed := func() error {
		return refusal(screen.PromptChanged, i18n.T(i18n.From(ctx), "rpc-errors.traex_question_changed"), nil)
	}
	terminal := func() error {
		return refusal(screen.DialogOpen, i18n.T(i18n.From(ctx), "rpc-errors.traex_question_terminal"), nil)
	}
	a, ok := n.agent(id)
	if !ok || a.Agent != traexAgent || host.ParsePane(a.PaneID).Kind != host.Herdr {
		return changed()
	}
	if err := n.switchingRefusal(id); err != nil {
		return err
	}
	ownsPane := func(ctx context.Context) bool {
		return n.traeForeground(ctx, a.PaneID) && n.traexLiveThread(a.PaneID, id) == id
	}
	read := func(ctx context.Context) (*screen.TraeQuestion, string, error) {
		visible, err := n.dialogScreen(ctx, a.PaneID)
		return screen.ReadTraeQuestion(visible), visible, err
	}
	same := func(q *screen.TraeQuestion) bool {
		return q != nil && traeQuestionRevision(id, a.PaneID, q) == revision
	}
	ctx, cancel := context.WithTimeout(ctx, codexConfirm)
	defer cancel()
	if !ownsPane(ctx) {
		return changed()
	}
	before, _, err := read(ctx)
	if err != nil || !same(before) {
		return changed()
	}
	pick := before.Dialog.Option(option)
	text = strings.TrimSpace(text)
	if pick == nil || before.FreeText != "" || (pick.Kind == screen.KindTypeSomething) != (text != "") ||
		strings.ContainsAny(text, "\r\n\t") || inertControls(text) != text {
		return terminal()
	}
	defer n.notifySessionsSoon()
	cursor, _ := strconv.Atoi(before.Dialog.Cursor)
	target, _ := strconv.Atoi(pick.Key)
	wait := func(matches func(*screen.TraeQuestion, string) bool) bool {
		for {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(codexStep):
			}
			after, visible, err := read(ctx)
			if err == nil && matches(after, visible) {
				return true
			}
		}
	}
	for cursor != target {
		step, key := 1, "down"
		if cursor > target {
			step, key = -1, "up"
		}
		if !ownsPane(ctx) || n.sendKeys(ctx, a.PaneID, key) != nil ||
			!wait(func(q *screen.TraeQuestion, _ string) bool {
				return same(q) && q.FreeText == "" && q.Dialog.Cursor == strconv.Itoa(cursor+step)
			}) {
			return unconfirmed()
		}
		cursor += step
	}
	if text != "" {
		if !ownsPane(ctx) || n.sendText(ctx, a.PaneID, pasteStart+text+pasteEnd) != nil ||
			!wait(func(q *screen.TraeQuestion, _ string) bool {
				return same(q) && q.Dialog.Cursor == option && q.FreeText == text
			}) {
			return unconfirmed()
		}
	}
	// Re-read immediately before Enter. A navigation event from the terminal itself may have changed the page,
	// cursor or text since our last readback.
	if !ownsPane(ctx) {
		return unconfirmed()
	}
	q, _, err := read(ctx)
	if err != nil || !same(q) || q.Dialog.Cursor != option || q.FreeText != text || n.sendKeys(ctx, a.PaneID, "enter") != nil {
		return unconfirmed()
	}
	if !wait(func(q *screen.TraeQuestion, visible string) bool {
		if before.SubmitsAll {
			box := parseTraeComposer(visible)
			return q == nil && box.open && !box.dialog
		}
		return q != nil && q.Total == before.Total && q.Index == before.Index+1 && q.Left == before.Left-1
	}) || !ownsPane(ctx) {
		return unconfirmed()
	}
	return nil
}
