package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/proto"
)


func TestChannelUnboundWhileQuestionIsBeingSent(t *testing.T) {
	s, f, _, _ := newTextAskService(t)
	ctx := context.Background()
	// Unbind inside the send, before askInChannel records its successful delivery. No sleeps or timing assumptions.
	f.answerApp(AppChannels, ChannelsSend, func(json.RawMessage) (any, error) {
		return map[string]any{}, s.ChannelUnbound(ctx, ChannelUnbound{Channel: "examplechat"})
	})
	s.askInChannel(ctx, "examplechat", proto.Interaction{ID: "old-permission", Kind: proto.IxPermission})
	if w := s.channelWaiting("examplechat"); w.Asks != 0 {
		t.Fatalf("an in-flight question restored the old binding: %+v", w)
	}
	// A genuinely new question after rebinding is still recorded normally.
	f.answerApp(AppChannels, ChannelsSend, func(json.RawMessage) (any, error) { return map[string]any{}, nil })
	s.askInChannel(ctx, "examplechat", proto.Interaction{ID: "new-permission", Kind: proto.IxPermission})
	if w := s.channelWaiting("examplechat"); w.Asks != 1 {
		t.Fatalf("new binding cannot receive questions: %+v", w)
	}
}

func TestChannelUnboundReportsStorageFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("read", func(t *testing.T) {
		st := &flakyScheduleStore{data: map[string][]byte{}}
		s := newServiceOn(t, newFakeHub().withMachine(), st)
		st.failReads(true)
		if err := s.ChannelUnbound(ctx, ChannelUnbound{Channel: "examplechat"}); err == nil {
			t.Fatal("unread state reported cleared")
		}
		if n := st.writes(); n != 0 {
			t.Fatalf("overwrote unread state with %d writes", n)
		}
	})
	t.Run("write", func(t *testing.T) {
		st := &scheduleWriteStore{Store: &memStore{}}
		s := newServiceOn(t, newFakeHub().withMachine(), st)
		st.failWrites(channelsStateKey)
		if err := s.ChannelUnbound(ctx, ChannelUnbound{Channel: "examplechat"}); err == nil {
			t.Fatal("failed state write reported cleared")
		}
		st.failWrites("")
		if err := s.ChannelUnbound(ctx, ChannelUnbound{Channel: "examplechat"}); err != nil {
			t.Fatalf("cleanup cannot be retried: %v", err)
		}
	})
}
