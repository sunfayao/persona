package assistant

import (
	"context"
	"net/url"
)

// The kinds of companion the workbench treats on its own: the Inbox's (its button replaces the /inbox one) and the
// any other kind appears as a link.
const (
	CompanionInbox  = "inbox"
	CompanionNotify = "notify"
	CompanionOther  = "other"
)

// CompanionBot is another Lark bot the workbench links to.
type CompanionBot struct {
	Kind  string `json:"kind"`   // CompanionInbox, CompanionNotify or CompanionOther
	Name  string `json:"name"`   // on its button
	Icon  string `json:"icon"`   // an emoji before the name
	AppID string `json:"app_id"` // its Lark app: the button opens the chat with it
}

// link opens the chat with the bot in the Lark app (applink; the web client may not follow it).
func (b CompanionBot) link() string {
	return "https://applink.feishu.cn/client/bot/open?appId=" + url.QueryEscape(b.AppID)
}

// companionBots are the registered companions.
func (s *Service) companionBots(ctx context.Context) []CompanionBot {
	b := &s.fbot
	b.mu.Lock()
	defer b.mu.Unlock()
	s.loadBotLocked(ctx)
	return append([]CompanionBot(nil), b.st.Companions...)
}
