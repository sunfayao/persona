package ownerfence

import (
	"context"
	"errors"
	"strings"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/lazyre"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/textfmt"
)

type observerKey struct{}
type actionKey struct{}

func WithObserver(ctx context.Context, supervisor Ref) context.Context {
	return context.WithValue(ctx, observerKey{}, supervisor)
}

func ObserverOf(ctx context.Context) (Ref, bool) {
	supervisor, ok := ctx.Value(observerKey{}).(Ref)
	return supervisor, ok
}

func WithAction(ctx context.Context, a Action) context.Context {
	return context.WithValue(ctx, actionKey{}, a)
}

func ActionOf(ctx context.Context) (Action, bool) {
	action, ok := ctx.Value(actionKey{}).(Action)
	return action, ok
}

func (b *Book) CheckContext(ctx context.Context) error {
	if a, ok := ActionOf(ctx); ok {
		if first := b.Check(a); first.Seq != 0 {
			return Refusal(first)
		}
	}
	return nil
}

// ShownMax is how many characters of an owner input agents_list and agent_read show.
const ShownMax = 200

// ShowObserved shows the latest input the owner gave target themselves (typed there, or an answer on its card), clipped.
func (b *Book) ShowObserved(ctx context.Context, target Ref) string {
	observer, ok := ObserverOf(ctx)
	if !ok || observer.Session == "" {
		return ""
	}
	input := b.Latest(target)
	if input.Seq == 0 {
		return ""
	}
	b.Observe(observer, target, input) // a relayed input still counts as read: it is no news to the reader
	text := shown(input.Text)
	if input.RelayOf != 0 || text == "" { // the main conversation's own words, relayed, are not what the owner said here
		return ""
	}
	return "\n" + prompts.RenderOwnerInputFence(prompts.OwnerInputFence{Text: text, Owner: input.ByOwner}) + "\n"
}

var (
	contextBlock = lazyre.New(`(?is)<\s*(?:jarvis|persona)-context\b[^>]*>.*?<\s*/\s*(?:jarvis|persona)-context\s*>`)
	contextOpen  = lazyre.New(`(?i)<(\s*(?:jarvis|persona)-context\b)`)
)

// shown is an owner input as the tools show it: the context blocks a client wrapped round the words are dropped (they
// are not the owner's), an opening tag left over is written with &lt; so none stays unclosed, and the rest is clipped.
// The template's ctx removes the closing tags.
func shown(text string) string {
	text = strings.TrimSpace(contextBlock.ReplaceAllString(text, ""))
	return textfmt.Clip(contextOpen.ReplaceAllString(text, "&lt;$1"), ShownMax)
}

func Refusal(input Input) error {
	return errors.New(prompts.RenderOwnerInputFence(prompts.OwnerInputFence{Text: input.Text, Owner: input.ByOwner, Blocked: true}))
}

// MatchesOwnerInput checks the latest user item against the authenticated main input.
func MatchesOwnerInput(items []proto.Item, input Input) bool {
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.Role != "user" || item.Kind != proto.KindText {
			continue
		}
		if !item.Queued && item.From == "" && item.Injected == nil && prompts.ItemSource(item) == nil &&
			(input.ClientMsgID != "" && item.ClientMsgID == input.ClientMsgID ||
				input.ClientMsgID == "" && !item.TS.Before(input.At) && textfmt.Clip(item.Text, 1200) == input.Text) {
			return true
		}
		break
	}
	return false
}

// WriteTarget identifies session mutations that can deliver a supervisory instruction.
func WriteTarget(method string, params any) (session, text string) {
	p, ok := params.(map[string]any)
	if !ok {
		return "", ""
	}
	switch method {
	case assistantwire.DeviceSessionSend, assistantwire.DeviceSessionSendOnce:
		session, _ = p["session_id"].(string)
		text, _ = p["text"].(string)
	case assistantwire.DeviceSessionStart:
		session, _ = p["resume_session_id"].(string)
		text, _ = p["prompt"].(string)
	case "session.interrupt":
		session, _ = p["session_id"].(string)
	default:
		return "", ""
	}
	return session, text
}

func WorkerQuestionError(isLeader, waitingOnOwner, waitingOnLeader bool) error {
	switch {
	case isLeader && waitingOnOwner:
		return errors.New(prompts.RenderWorkerQuestionAuthority(true))
	case !isLeader && !waitingOnOwner && waitingOnLeader:
		return errors.New(prompts.RenderWorkerQuestionAuthority(false))
	}
	return nil
}
