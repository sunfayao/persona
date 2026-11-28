package ownerfence

import (
	"context"
	"errors"

	"github.com/sunfayao/persona/server/internal/assistantwire"
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

func (b *Book) ShowObserved(ctx context.Context, target Ref) string {
	observer, ok := ObserverOf(ctx)
	if !ok || observer.Session == "" {
		return ""
	}
	input := b.Latest(target)
	if input.Seq == 0 {
		return ""
	}
	b.Observe(observer, target, input)
	return "\n" + prompts.RenderOwnerInputFence(prompts.OwnerInputFence{Text: input.Text, Owner: input.ByOwner}) + "\n"
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
