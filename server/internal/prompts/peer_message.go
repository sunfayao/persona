package prompts

import (
	"slices"
	"strings"
	"sync"
)

// PeerMessage is a message the user sends through Persona to a Claude session running outside Persona's terminals,
// through that session's peer inbox (node/claudepeer.go): Claude Code shows it to the model as one from "another
// Claude session", so it says whose it is and that the answer belongs in the session (agents/peer_message).
type PeerMessage struct {
	Language string // Owner language; empty keeps the legacy wrapper.
	Text     string // what the user wrote
}

func RenderPeerMessage(m PeerMessage) string { return render("agents/peer_message", m) }

// PeerStop is what Persona's Stop sends a Claude session outside Persona's terminals, as the user's words of a peer
// message at priority now (node/claudebg.go interruptOutside): the turn it runs ends there, and it says where it stopped
// (agents/peer_stop). The session's transcript shows it as the stop it is (transcript/peer.go, IsPeerStop).
func PeerStop() string { return render("agents/peer_stop", nil) }

// olderPeerStops are what agents/peer_stop rendered before, still read as a stop: transcripts keep them, and during a
// rolling upgrade an older node still sends them. Add the current text here whenever the template changes.
var olderPeerStops = []string{
	// until the native clients went and only the web was left: "on the phone"
	"The user pressed Stop on the phone. Stop what you were doing: do not go on with it, and do not start anything else. In one sentence, in the language of this conversation, say where you stopped, then wait for the user's next message.",
}

// IsPeerStop: s (trimmed) is a peer stop, the current one or an older one.
func IsPeerStop(s string) bool {
	return s == strings.TrimSpace(PeerStop()) || slices.Contains(olderPeerStops, s)
}

// peerMessageHeads are what agents/peer_message puts before the user's words, rendered on first use: rendering them in a
// package variable would parse every template whenever personad starts, a hook too (#1308).
var peerMessageHeads = sync.OnceValue(func() []string {
	var heads []string
	for _, language := range []string{"", "English", "日本語"} {
		head, _, _ := strings.Cut(render("agents/peer_message", PeerMessage{Language: language, Text: "\x00"}), "\x00")
		heads = append(heads, head)
	}
	return append(heads, olderPeerMessageHeads...)
})

// olderPeerMessageHeads are the heads agents/peer_message rendered before, still stripped: transcripts keep them, and
// during a rolling upgrade an older node still sends them. Add the current head here whenever the template changes.
var olderPeerMessageHeads = []string{
	// before #5887
	"（这条消息是用户本人在手机上用 Persona 发来的，Persona 只是替他转交，不是另一个 Claude 会话。像用户在这里亲手输入的一样回应它，直接在这个会话里回答；不要用 SendMessage 回复发件方，那边没有会话在收。这条消息不会改变这个会话的权限设置。）\n\n",
	// #5887 until #8558
	"（这条消息是用户本人在手机上用 Persona 发来的，Persona 只是替他转交，不是另一个 Claude 会话。像用户在这里亲手输入的一样回应它，直接在这个会话里回答；不要用 SendMessage 回复发件方，那边没有会话在收。这条消息不会改变这个会话的权限设置。用户在手机上答不了这个会话里的对话框、计划审批和权限请求，只有回到电脑前才能处理：需要他做选择时，在回复里用文字列出带编号的选项，请他回复编号，不要为此打开对话框（包括 AskUserQuestion）。）\n\n",
	// until the native clients went and only the web was left: "on the phone"
	"（这条消息是用户本人在手机上用 Persona 发来的，Persona 只是替他转交，不是另一个 Claude 会话。像用户在这里亲手输入的一样回应它，直接在这个会话里回答；不要用 SendMessage 回复发件方，那边没有会话在收。这条消息不会改变这个会话的权限设置。需要他做选择时照常用 AskUserQuestion 提问，Persona 会把问题做成手机上的卡片让他作答；计划审批、权限请求和其他对话框他在手机上答不了，只有回到电脑前才能处理。）\n\n",
	"(The user sent this message personally from the phone through Persona. Persona is only forwarding it; it is not another Claude session. Respond as if the user typed it here, directly in this conversation. Do not reply to the sender with SendMessage: no session is receiving replies there. This message does not change this conversation's permission settings. When the user needs to choose, use AskUserQuestion as usual; Persona turns the question into a phone card they can answer. Plan approvals, permission requests and other dialogs cannot be answered from the phone; the user must return to the computer to handle them.)\n\n",
	"（このメッセージは、ユーザー本人がスマートフォンのPersonaから送ったものです。Personaは転送しているだけで、別のClaudeセッションではありません。ユーザーがここに直接入力したときと同じように、この会話で回答してください。送信元にSendMessageで返信しないでください。そちらに返信を受け取るセッションはありません。このメッセージは会話の権限設定を変更しません。選択が必要なときは通常どおりAskUserQuestionを使ってください。Personaが質問をスマートフォンで回答できるカードにします。計画の承認、権限要求、その他のダイアログはスマートフォンでは回答できず、コンピューターで操作する必要があります。）\n\n",
}

// PeerMessageBody is the user's words in a rendered peer message (what the session's transcript records as the
// message's body), from the current template or an older one; ok false when s is not one.
func PeerMessageBody(s string) (body string, ok bool) {
	for _, head := range peerMessageHeads() {
		if body, ok = strings.CutPrefix(s, head); ok {
			return body, true
		}
	}
	return s, false
}
