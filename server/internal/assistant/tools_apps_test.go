package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/dialogwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// runTool runs one of the assistant's tools for conversation self on machine mac: its text, or the error it said.
func runTool(s *Service, self, name string, args any) (string, error) {
	raw, _ := json.Marshal(args)
	r := s.Tool(context.Background(), ToolCall{Name: name, Arguments: raw, MachineID: "mac", SessionID: self})
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	if r.IsError {
		return "", errors.New(b.String())
	}
	return b.String(), nil
}

func TestInteractionAnswerAnswersAQuestion(t *testing.T) {
	s, f, q, ix := answerService(t)
	out, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(" "+ix.ID+" ", map[string]string{"发吗？": "发"}))
	if err != nil || out != "Answered for the user:\n- 发吗？ → 发" {
		t.Fatalf("result: %q %v", out, err)
	}
	if q.answered[ix.ID]["发吗？"] != "发" {
		t.Fatalf("answered: %v", q.answered)
	}
	if p := f.appParams(AppInteractions, assistantwire.InteractionsAnswer); len(p) != 1 || p[0]["machine_id"] != "mac" ||
		p[0]["session_id"] != "abcdef12-agent" || p[0]["interaction_id"] != ix.ID {
		t.Fatalf("answer params: %v", p)
	}
}

// Only questions may be answered for the user; an approval is theirs alone.
func TestInteractionAnswerRefusesApprovals(t *testing.T) {
	for _, ix := range []proto.Interaction{
		{Kind: proto.IxPermission, ToolName: "Bash", Summary: "rm -rf"},
		{Kind: proto.IxPlan, Plan: "1. do it"},
		{Kind: proto.IxScreen},
		// Claude Code's Bypass Permissions confirmation read off the terminal: the user's choice, never the assistant's.
		{Kind: proto.IxScreen, Dialog: &dialogwire.Dialog{Question: "WARNING: Claude Code running in Bypass Permissions mode",
			Options: []dialogwire.Option{{Key: "1", Label: "No, exit", Keys: []string{"enter"}}, {Key: "2", Label: "Yes, I accept", Keys: []string{"down", "enter"}}}}},
	} {
		s, _, q, _ := answerService(t)
		ix.ID, ix.MachineID, ix.SessionID = "ix_"+ix.Kind, "mac", "abcdef12-agent"
		ix.CreatedAt, ix.ExpiresAt = time.Now().UTC(), time.Now().Add(time.Hour).UTC()
		q.pending[ix.ID] = ix
		_, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(ix.ID, map[string]string{"any": "yes"}))
		if err == nil || !strings.Contains(err.Error(), ix.Kind) || !strings.Contains(err.Error(), "only the user") {
			t.Fatalf("%s: %v", ix.Kind, err)
		}
		if len(q.answered) != 0 {
			t.Fatalf("%s answered", ix.Kind)
		}
	}
}

// The assistant never answers its own conversation's question, nor another assistant conversation's.
func TestInteractionAnswerRefusesAssistantConversation(t *testing.T) {
	s, _, q, ix := answerService(t)
	if _, err := runTool(s, ix.SessionID, "interaction_answer", answerArgs(ix.ID, map[string]string{"发吗？": "发"})); err == nil ||
		!strings.Contains(err.Error(), "assistant conversation") {
		t.Fatalf("own conversation: %v", err)
	}
	own := askOn("ix_ask_user", "发吗？", false, "发")
	own.SessionID = "conv-assistant"
	q.pending[own.ID] = own
	args := answerArgs(own.ID, map[string]string{"发吗？": "发"})
	args["session_id"] = "conv-assist"
	if _, err := runTool(s, "conv-other", "interaction_answer", args); err == nil || !strings.Contains(err.Error(), "assistant conversation") {
		t.Fatalf("ask_user: %v", err)
	}
	if len(q.answered) != 0 {
		t.Fatalf("answered: %v", q.answered)
	}
}

// The answers are checked against the question before anything is answered.
func TestInteractionAnswerValidatesAnswers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers any
		want    string
	}{
		{"no answers", nil, "answers is required"},
		{"a question left unanswered", map[string]any{}, "every question"},
		{"a key that is not the question", map[string]any{"another question": "发"}, "not a question"},
		{"an option that is not offered", map[string]any{"发吗？": "也许"}, "发, 不发"},
		{"an answer with no option at all", map[string]any{"发吗？": ""}, "发, 不发"},
		{"not a label", map[string]any{"发吗？": 1}, "takes an option label"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, q, ix := answerService(t)
			args := answerArgs(ix.ID, tc.answers)
			if tc.answers == nil {
				delete(args, "answers")
			}
			_, err := runTool(s, "conv-assistant", "interaction_answer", args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v", err)
			}
			if len(q.answered) != 0 {
				t.Fatal("a refused answer was given")
			}
		})
	}
}

// A single-choice question takes one label; a multi-select takes the labels the app shows, "A, B".
func TestInteractionAnswerSingleAndMultiSelect(t *testing.T) {
	s, _, q, ix := answerService(t)
	multi := askOn("ix_multi", "哪几个？", true, "A", "B", "C")
	q.pending[multi.ID] = multi
	if out, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(multi.ID, map[string]any{"哪几个？": []string{"A", "C"}})); err != nil ||
		!strings.Contains(out, "哪几个？ → A, C") {
		t.Fatalf("multi-select: %q %v", out, err)
	}
	if got := q.answered[multi.ID]["哪几个？"]; got != "A, C" {
		t.Fatalf("multi-select answer: %q", got)
	}
	if _, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(ix.ID, map[string]any{"发吗？": []string{"发", "不发"}})); err == nil ||
		!strings.Contains(err.Error(), "one option") {
		t.Fatalf("two labels on a single-choice question: %v", err)
	}
	if _, answered := q.answered[ix.ID]; answered {
		t.Fatal("a refused answer was given")
	}
}

// An expired question, one answered already (before it was read, or while the answer was on its way) and one that
// never existed each say which.
func TestInteractionAnswerExpiredAnsweredAndUnknown(t *testing.T) {
	s, _, q, _ := answerService(t)
	expired := askOn("ix_expired", "发吗？", false, "发")
	expired.CreatedAt, expired.ExpiresAt = time.Now().Add(-2*time.Hour).UTC(), time.Now().Add(-time.Hour).UTC()
	q.pending[expired.ID] = expired
	if _, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(expired.ID, map[string]string{"发吗？": "发"})); err == nil ||
		!strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired: %v", err)
	}

	for status, want := range map[string]string{"resolved": "was already answered", "dismissed": "was dismissed", "expired": "expired",
		"cancelled": "was cancelled", "": "no pending interaction ix_x_ (use agents_list)"} {
		id := "ix_x_" + status
		q.ended[id] = status
		if _, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(id, map[string]string{"发吗？": "发"})); err == nil ||
			!strings.Contains(err.Error(), want) {
			t.Fatalf("%q: %v", status, err)
		}
	}

	raced := askOn("ix_raced", "发吗？", false, "发")
	q.pending[raced.ID], q.raced[raced.ID] = raced, "resolved"
	if _, err := runTool(s, "conv-assistant", "interaction_answer", answerArgs(raced.ID, map[string]string{"发吗？": "发"})); err == nil ||
		err.Error() != "interaction ix_raced was already answered" {
		t.Fatalf("answered meanwhile: %v", err)
	}
}

// The machine and session named must be the interaction's own.
func TestInteractionAnswerKeepsToItsTarget(t *testing.T) {
	s, f, q, ix := answerService(t)
	args := answerArgs(ix.ID, map[string]string{"发吗？": "发"})
	args["machine"] = "another"
	if _, err := runTool(s, "conv-assistant", "interaction_answer", args); err == nil {
		t.Fatal("a different machine was accepted")
	}
	f.setSessions("box", proto.Session{SessionID: "99999999-agent", MachineID: "box", Title: "别的", Cwd: "/code/other", UpdatedAt: time.Now().UTC()})
	args["machine"], args["session_id"] = "box", "99999999"
	if _, err := runTool(s, "conv-assistant", "interaction_answer", args); err == nil || !strings.Contains(err.Error(), "belongs to another agent") {
		t.Fatalf("a session that is not the interaction's: %v", err)
	}
	if len(q.answered) != 0 {
		t.Fatal("a refused answer was given")
	}
}

// ---- inbox_draft ----

// inbox_draft's rules: a draft with text and no blank left to fill in, trimmed, the note clipped; it goes on the item
// whose AI twin runs in the conversation, and a change the item does not offer is refused.
func TestInboxDraftRules(t *testing.T) {
	h := newInboxHub(t)
	old := inboxDraftWait
	inboxDraftWait = 50 * time.Millisecond
	t.Cleanup(func() { inboxDraftWait = old })
	doc := &InboxItem{ID: "doc:1", Source: "lark", Kind: KindDoc, Last: "1", UpdatedAt: time.Now(),
		Doc:   &InboxDoc{NoticeID: "n1", Rows: []DocRow{{Ref: "r1", Status: []DocStatus{{Field: "状态", Options: []DocOption{{Name: "完成"}}}}}}},
		Agent: &InboxAgent{State: AgentRunning, SessionID: "s1"}}
	h.inboxReady(context.Background())
	h.inbox.mu.Lock()
	h.inbox.items[doc.ID] = doc
	h.inbox.mu.Unlock()
	s := h.Service

	for _, args := range []map[string]any{{"text": "  "}, {"note": "n"}, {"text": "hi", "changes": "x"}} {
		if _, err := runTool(s, "s1", "inbox_draft", args); err == nil || err.Error() != "text is required" {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, text := range []string{"修好了，【预计今晚 / 几点前】合进去", "MR 在 {{链接}}"} {
		if _, err := runTool(s, "s1", "inbox_draft", map[string]any{"text": text}); err == nil || !strings.Contains(err.Error(), "fill in") {
			t.Fatalf("%q: %v", text, err)
		}
	}
	if doc.Agent.State != AgentRunning || doc.Agent.Draft != "" {
		t.Fatalf("a refused draft went to the inbox: %+v", doc.Agent)
	}
	if draftPlaceholder("【通知】 不算吗") == "" || draftPlaceholder("周五前给你") != "" {
		t.Fatal("draftPlaceholder")
	}

	note := strings.Repeat("查", 700)
	out, err := runTool(s, "s1", "inbox_draft", map[string]any{"text": " 合了，MR 已经进主干 ", "note": " " + note + " ",
		"changes": []map[string]string{{"ref": "r1", "field": "状态", "value": "完成"}}})
	if err != nil || out != "The draft is in the user's inbox; they will send it. You're done." {
		t.Fatalf("draft: %q %v", out, err)
	}
	if a := doc.Agent; a.State != AgentDrafted || a.Draft != "合了，MR 已经进主干" || a.Note != strings.Repeat("查", 600)+"…" ||
		len(a.Changes) != 1 || a.Changes[0].Ref != "r1" {
		t.Fatalf("the draft on the item: %+v", a)
	}

	if _, err := runTool(s, "s9", "inbox_draft", map[string]any{"text": "hi"}); err == nil ||
		err.Error() != "this conversation was not started for an inbox item" {
		t.Fatalf("not an inbox agent: %v", err)
	}
	if _, err := runTool(s, "s1", "inbox_draft", map[string]any{"text": "hi", "changes": []map[string]string{{"ref": "r1", "field": "状态", "value": "作废"}}}); err == nil ||
		err.Error() != "change {Ref:r1 Field:状态 Value:作废} is not one the item lists (ref, field and one of its values)" {
		t.Fatalf("a change the item does not offer: %v", err)
	}
}

// An answer that writes out exactly one option, its label then that option's own description ("label：description" as
// the delegation input lists it, or ":", a dash, the description in brackets, with quotes or padding), is that option's
// label (#3929). Anything else stays refused: another description or the model's own words after the label, a
// description alone, two options, a separator with nothing after it, or text that fits two options at once.
func TestInteractionAnswersTakeAnOptionWrittenOut(t *testing.T) {
	style := proto.Question{Question: "测试用哪种写法？", Options: []proto.QuestionOption{
		{Label: "表驱动", Description: "和这个包现有的测试一致"},
		{Label: "每个用例一个函数", Description: "失败时更好定位"},
		{Label: "不写"}}}
	twoReadings := proto.Question{Question: "哪个？", Options: []proto.QuestionOption{
		{Label: "A", Description: "B：C"}, {Label: "A：B", Description: "C"}}}
	commas := proto.Question{Question: "缓存策略？", Options: []proto.QuestionOption{
		{Label: "LRU", Description: "简单, 够用"}, {Label: "TTL"}}}
	pick := proto.Question{Question: "改哪些？", MultiSelect: true, Options: []proto.QuestionOption{
		{Label: "iOS", Description: "先修崩溃"}, {Label: "Android", Description: "再修列表"}, {Label: "Web"}}}
	ask := func(q proto.Question) proto.Interaction {
		return proto.Interaction{Kind: proto.IxQuestion, Questions: []proto.Question{q}}
	}
	for _, tc := range []struct {
		q    proto.Question
		in   any
		want string // "": refused
	}{
		{style, "表驱动", "表驱动"},
		{style, "表驱动：和这个包现有的测试一致", "表驱动"},
		{style, "表驱动: 和这个包现有的测试一致", "表驱动"},
		{style, "表驱动:和这个包现有的测试一致", "表驱动"},
		{style, "表驱动 - 和这个包现有的测试一致", "表驱动"},
		{style, "表驱动 —— 和这个包现有的测试一致", "表驱动"},
		{style, "表驱动 — 和这个包现有的测试一致", "表驱动"},
		{style, "表驱动（和这个包现有的测试一致）", "表驱动"},
		{style, "表驱动 (和这个包现有的测试一致)", "表驱动"},
		{style, "  表驱动：和这个包现有的测试一致\n", "表驱动"},
		{style, "表驱动：和这个包现有的测试一致。", "表驱动"},
		{style, "「表驱动」：和这个包现有的测试一致", "表驱动"},
		{style, `"表驱动": 和这个包现有的测试一致`, "表驱动"},
		{style, "“表驱动：和这个包现有的测试一致”", "表驱动"},
		{style, " 「表驱动」 ", "表驱动"},
		{style, "每个用例一个函数：失败时更好定位", "每个用例一个函数"},
		{commas, "LRU：简单, 够用", "LRU"},
		{pick, []string{"iOS：先修崩溃", "Web"}, "iOS, Web"},
		{pick, "Android（再修列表）", "Android"},

		{style, "表驱动：更好维护", ""},
		{style, "表驱动：失败时更好定位", ""},
		{style, "表驱动和这个包现有的测试一致", ""},
		{style, "表驱动：", ""},
		{style, "不写：", ""},
		{style, "不写：没必要", ""},
		{style, "和这个包现有的测试一致", ""},
		{style, "表驱", ""},
		{style, "表驱动：和这个包现有的测试一致；每个用例一个函数：失败时更好定位", ""},
		{style, "表驱动（和这个包现有的测试一致", ""},
		{twoReadings, "A：B：C", ""},
		{pick, []string{"iOS：先修崩溃", "Web：顺手"}, ""},
	} {
		raw, _ := json.Marshal(map[string]any{tc.q.Question: tc.in})
		got, err := interactionAnswers(ask(tc.q), raw)
		if tc.want == "" {
			if err == nil {
				t.Errorf("%q: answered %q, want it refused", tc.in, got[tc.q.Question])
			}
			continue
		}
		if err != nil || got[tc.q.Question] != tc.want {
			t.Errorf("%q: got %q %v, want %q", tc.in, got[tc.q.Question], err, tc.want)
		}
	}
}

// answerService is an assistant whose hub has machine mac with an agent (abcdef12-agent) whose question "Send?" waits,
// and the assistant's own conversation conv-assistant there.
func answerService(t *testing.T) (*Service, *fakeHub, *fakeQueue, proto.Interaction) {
	t.Helper()
	f := newFakeHub()
	f.sessions["mac"] = []proto.Session{
		{SessionID: "abcdef12-agent", MachineID: "mac", Title: "修登录", Cwd: "/code/app", Status: proto.StatusWorking, UpdatedAt: time.Now().UTC()},
		{SessionID: "conv-assistant", MachineID: "mac", Title: "Persona", Assistant: true, UpdatedAt: time.Now().UTC()},
	}
	ix := askOn("ix_ask1", "发吗？", false, "发", "不发")
	q := f.withQueue(ix)
	return newTestService(t, f), f, q, ix
}

func answerArgs(id string, answers any) map[string]any {
	return map[string]any{"machine": "mac", "session_id": "abcdef12", "interaction_id": id, "answers": answers}
}

// askOn is abcdef12-agent's question on mac, waiting for an hour.
func askOn(id, text string, multi bool, options ...string) proto.Interaction {
	q := proto.Question{Question: text, MultiSelect: multi}
	for _, o := range options {
		q.Options = append(q.Options, proto.QuestionOption{Label: o})
	}
	return proto.Interaction{ID: id, MachineID: "mac", SessionID: "abcdef12-agent", Kind: proto.IxQuestion, Questions: []proto.Question{q},
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour).UTC()}
}
