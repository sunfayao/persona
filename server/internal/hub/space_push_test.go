package hub

import (
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

// A space conversation's card says on the lock screen only that there is news, not what it is about nor the
// conversation's title (docs/assistant/tracks.md "Outbound"); other cards are as before.
func TestSpaceCardPushHidesContent(t *testing.T) {
	alert := func(ix proto.Interaction) map[string]any {
		return interactionPayload(ix, "server")["aps"].(map[string]any)["alert"].(map[string]any)
	}
	a := alert(proto.Interaction{Kind: proto.IxPermission, ToolName: "mail_send", SessionTitle: "血压", Cwd: "/home/me/.persona/tracks/qa-private",
		Summary: "发给 doctor@example.com：最近一周血压 150/95"})
	if body, sub := a["body"].(string), a["subtitle"].(string); strings.Contains(body, "150") || strings.Contains(sub, "血压") || !strings.HasPrefix(sub, "Persona") {
		t.Fatalf("space card: %v", a)
	}
	q := alert(proto.Interaction{Kind: proto.IxQuestion, ToolName: "ask_user", SessionTitle: "睡眠", Cwd: "/home/me/.persona/tracks/qa-private",
		Questions: []proto.Question{{Question: "昨晚几点睡的？"}}})
	if strings.Contains(q["body"].(string), "几点") {
		t.Fatalf("space question: %v", q)
	}
	if o := alert(proto.Interaction{Kind: proto.IxPermission, SessionTitle: "Persona", Cwd: "/home/me/.persona/assistant", Summary: "发周报"}); o["body"] != "发周报" {
		t.Fatalf("assistant's own card: %v", o)
	}
	// A card from a folder of a space (a hook after the model's `cd`, #2368) is the space's too: no content,
	// no answer buttons. A directory that only starts the same, or a folder named like it elsewhere, is not.
	ask := func(cwd string) proto.Interaction {
		return proto.Interaction{Kind: proto.IxQuestion, ToolName: "AskUserQuestion", SessionTitle: "报告", Cwd: cwd,
			Questions: []proto.Question{{Question: "血糖偏高要记下吗？", Options: []proto.QuestionOption{{Label: "记"}, {Label: "不记"}}}}}
	}
	for cwd, hidden := range map[string]bool{
		"/home/me/.persona/tracks/qa-private/reports": true,
		"/home/me/.persona/health-bin":                false,
		"/tmp/qa-private":                             false,
	} {
		payload := interactionPayload(ask(cwd), "server")
		body := payload["aps"].(map[string]any)["alert"].(map[string]any)["body"].(string)
		_, buttons := payload["ask"]
		if hidden != !strings.Contains(body, "血糖") || hidden == buttons {
			t.Errorf("%s: body %q, answer buttons %v; want hidden %v", cwd, body, buttons, hidden)
		}
	}
}
