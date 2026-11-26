package assistant

import (
	"context"
	"errors"
	"net/http"

	"github.com/nwverse/persona/server/internal/apierr"
	"github.com/nwverse/persona/server/internal/channel"
	"github.com/nwverse/persona/server/internal/i18n"
)

// ---- the app's interface (relayed by the hub, assistantwire.Routes) ----

func (s *Service) channelRoutes() {
	handle := func(pattern string, fn http.HandlerFunc) {
		s.handle(pattern, func(w http.ResponseWriter, r *http.Request) { fn(w, withRequestLang(r)) })
	}
	handle("GET /api/v1/channels", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.channelList(r.Context())
		if err != nil {
			apierr.Write(w, http.StatusBadGateway, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"channels": list})
	})
}

// updateChannel applies the app's changes: only what is given.
func (s *Service) updateChannel(ctx context.Context, v channel.View, enabled *bool, target *channel.Target, features map[string]bool) error {
	l := s.botLang(ctx)
	if target != nil {
		if err := target.Check(); err != nil {
			return err
		}
		if target.Kind == channel.TargetConversation && !s.isAssistantConversation(ctx, target.MachineID, target.SessionID) {
			return errors.New(i18n.T(l, "assistant-channels.assistant_chats_only"))
		}
	}
	known := map[string]bool{}
	for _, f := range v.Features {
		known[f.ID] = true
	}
	for id := range features {
		if !known[id] {
			return errors.New(i18n.T(l, "assistant-channels.no_feature", i18n.Args{"name": v.Name, "feature": id}))
		}
	}
	if v.ID == channel.Feishu {
		return s.updateFeishuChannel(ctx, enabled, target, features)
	}
	if v.Source != channel.SourcePlugin {
		return errors.New(i18n.T(l, "assistant-channels.not_set_here", i18n.Args{"name": v.Name}))
	}
	if enabled != nil {
		if err := s.appCall(ctx, AppChannels, ChannelsEnable, map[string]any{"channel": v.ID, "on": *enabled}, nil); err != nil {
			return err
		}
	}
	c := &s.chans
	c.mu.Lock()
	defer c.mu.Unlock()
	s.loadChannelsLocked(ctx)
	p := s.channelPrefLocked(v.ID)
	if target != nil {
		t := *target
		t.Title = ""
		p.Target = &t
	}
	for id, on := range features {
		p.Features[id] = on
		if id == channel.FeatureAllAsks && on {
			p.Features[channel.FeatureAsks] = true
		}
	}
	s.saveChannelsLocked()
	return nil
}

type cannotSendError struct {
	msg string
}

func (e cannotSendError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return errCannotSend.Error()
}

func (e cannotSendError) Unwrap() error { return errCannotSend }
