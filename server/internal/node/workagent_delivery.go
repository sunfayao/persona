package node

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/sunfayao/persona/server/internal/node/harness/agy"
	"github.com/sunfayao/persona/server/internal/proto"
)

// herdr's agent.prompt acknowledges terminal input, not an agy turn. A follow-up is delivered only when agy appends
// that input to the bound conversation. Otherwise the caller gets delivered=unknown and no automatic retype (#14623).
var (
	agySendWait = 10 * time.Second
	agySendPoll = 100 * time.Millisecond
)

const agySendUnconfirmed = "消息已写进 agy 的终端，但没有在 agy 会话记录里看到它，不能确认已送达；先在电脑上看一眼再决定要不要重发。"

// agySendWatch reads complete transcript lines appended after a prompt was typed. A missing initial transcript may
// appear on the first turn, but then its input needs a timestamp at or after the send so an older identical request
// cannot acknowledge it. Replacing or shrinking the transcript makes delivery unknowable rather than confirmed.
type agySendWatch struct {
	reader                  agyContent
	ref                     agy.Ref
	before                  *agy.Projection
	want                    []rune
	sent                    string
	after                   time.Time
	requireTime, lost, seen bool
}

func (n *Node) watchAgySend(id, pane, text string, typedAt time.Time) *agySendWatch {
	ref := n.agyContentRef(id, pane)
	if ref.Conversation == "" {
		return nil
	}
	reader := n.agyReader()
	h, err := reader.Read(ref)
	if err != nil {
		return nil
	}
	return &agySendWatch{reader: reader, ref: ref, before: h, want: echoWords(text), sent: text, after: typedAt,
		requireTime: h.Kind() == agy.Missing}
}

// took reports whether a complete new explicit input matches the send, without parsing the transcript again.
func (w *agySendWatch) took() bool {
	if w == nil || w.lost {
		return false
	}
	if w.seen {
		return true
	}
	h, err := w.reader.Read(w.ref)
	if err != nil || h == w.before {
		return false
	}
	var since int
	if w.before != nil {
		if !h.Follows(w.before) {
			w.lost = true
			return false
		}
		since = w.before.Seq()
	}
	items := h.Items()
	for _, it := range items[agy.AfterIndex(items, since):] {
		if it.Role != "user" || w.requireTime && it.TS.Before(w.after.Add(-time.Second)) {
			continue
		}
		if len(w.want) == 0 && strings.TrimSpace(w.sent) != "" && (it.Kind == proto.KindImage || strings.Contains(it.Text, "<attached_images>")) ||
			it.Kind == proto.KindText && agySendTextMatches(agyNativeInputText(it.Text, w.sent), w.want) {
			w.seen = true
			return true
		}
	}
	w.before = h
	return false
}

func agySendTextMatches(text string, want []rune) bool {
	if len(want) == 0 {
		return false
	}
	got := echoWords(text)
	k := min(len(want), promptWords)
	return len(got) >= k && string(got[:k]) == string(want[:k])
}

func agyNativeInputText(text, sent string) string {
	text = agy.UserText(text)
	if strings.HasPrefix(strings.TrimLeftFunc(sent, unicode.IsSpace), "/") {
		return text
	}
	if i := strings.IndexFunc(text, unicode.IsSpace); i > 0 && strings.HasPrefix(text, "/") {
		return strings.TrimLeftFunc(text[i:], unicode.IsSpace)
	}
	return text
}

func (n *Node) confirmAgySend(ctx context.Context, id, pane, text string, typedAt time.Time, watch *agySendWatch, beforeQueued int) error {
	ticker := time.NewTicker(agySendPoll)
	defer ticker.Stop()
	timer := time.NewTimer(agySendWait)
	defer timer.Stop()
	for {
		if watch == nil {
			// A follow-up can be the input that makes agy create and report its first conversation. Refresh host state so
			// herdr's new agent_session report is visible without waiting on an unrelated session list poll, then bind it
			// late and scan the new file from its start; only timestamped input at or after this send can count.
			_ = n.refreshHosts(ctx)
			watch = n.watchAgySend(id, pane, text, typedAt)
			if watch != nil {
				watch.before = nil
				watch.requireTime = true
			}
		}
		if watch.took() {
			return nil
		}
		if watch != nil && watch.lost {
			return uncertain(errors.New(agySendUnconfirmed))
		}
		// While agy is busy, it buffers follow-ups in TUI memory and shows `<preview>... (alt+m to send immediately)`
		// instead of appending USER_INPUT to the full transcript immediately (#14871).
		if screen, err := n.readPane(ctx, pane, "visible", 80); err == nil {
			if lines := parseAgyQueuedLines(screen); agyScreenShowsQueued(lines, text, beforeQueued) {
				n.recordAgyQueued(id, pane, text, watch, lines)
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return uncertain(fmt.Errorf("%s: %w", agySendUnconfirmed, ctx.Err()))
		case <-timer.C:
			return uncertain(errors.New(agySendUnconfirmed))
		case <-ticker.C:
		}
	}
}
