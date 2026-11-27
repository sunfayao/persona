package assistant

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestEntriesCardAnswerStructuredAndDefaultsExcluded(t *testing.T) {
	database := todoTestDB(t)
	e := newTodoEnv(t, database)
	ctx := context.Background()
	yes := true
	ix := proto.Interaction{ID: "card-example", MachineID: "box", SessionID: "main-example", Kind: proto.IxQuestion, ToolName: "ask_user", Reversible: &yes,
		Questions: []proto.Question{{Question: "Which release path?", Options: []proto.QuestionOption{{Label: "Stage"}, {Label: "Release"}}}}}
	answer := proto.Resolution{Answers: map[string]string{"Which release path?": "Stage"}}
	if err := e.s.ledgerCardDecisions(ctx, ix, answer, "resolved"); err != nil {
		t.Fatal(err)
	}
	out, err := database.ListAssistantEntries(ctx, proto.AssistantEntriesParams{By: "user", Type: "decision"}, true)
	if err != nil || len(out.Entries) != 1 {
		t.Fatalf("card result: %+v %v", out, err)
	}
	got := out.Entries[0]
	if got.Source.Card != ix.ID || got.Source.Machine != "box" || got.Quote != "Stage" || got.Decision == nil || !reflect.DeepEqual(got.Decision.Rejected, []string{"Release"}) || got.Decision.Reversible == nil || !*got.Decision.Reversible {
		t.Fatalf("structured card: %+v", got)
	}
	if err := e.s.ledgerCardDecisions(ctx, ix, answer, "resolved"); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{proto.AnsweredByAssistant, proto.AnsweredByDefault, proto.AnsweredByTerminal} {
		ix.ID = "card-" + actor
		answer.AnsweredBy = actor
		if err := e.s.ledgerCardDecisions(ctx, ix, answer, "resolved"); err != nil {
			t.Fatal(err)
		}
	}
	out, err = database.ListAssistantEntries(ctx, proto.AssistantEntriesParams{}, true)
	if err != nil || len(out.Entries) != 1 {
		t.Fatalf("automatic answers entered as user: %+v %v", out, err)
	}
}

func TestEntriesAskUserAnswerIsQueryableAndModelQuotesStayInferred(t *testing.T) {
	database := todoTestDB(t)
	e := newAskTodoEnv(t, database)
	ctx := context.Background()
	ix, _ := e.askLater(askLaterArgs)
	e.mustEnd(ix.ID, `{"answers":{"Merge the release PR now?":"Wait"}}`)
	raw, err := e.s.toolEntries(ctx, "server", "c1x1x1", "decisions_query", json.RawMessage(`{"by":"user"}`))
	var out proto.AssistantEntriesResult
	if err != nil || json.Unmarshal([]byte(raw), &out) != nil || len(out.Entries) != 1 || out.Entries[0].Source.Card != ix.ID {
		t.Fatalf("query after ask_user: %s %v", raw, err)
	}
	input := json.RawMessage(`{"entry":{"text":"Stage the example release.","decision":{"options":["Stage","Release"],"selected":["Stage"],"rejected":["Release"],"reason":"Retain rollback.","reversible":true}},"user_quotes":[{"quote":"Use the example workflow.","source":{"machine":"server","session":"c1x1x1","msg_uuid":"message-example"},"trust":"user_verbatim"}]}`)
	for range 2 {
		if _, err := e.s.toolEntries(ctx, "server", "c1x1x1", "decision_record", input); err != nil {
			t.Fatal(err)
		}
	}
	quotes, err := database.ListAssistantEntries(ctx, proto.AssistantEntriesParams{Trust: "inferred"}, true)
	if err != nil || len(quotes.Entries) != 1 || quotes.Entries[0].By != "user" || quotes.Entries[0].Source.Message != "message-example" {
		t.Fatalf("unverified user quote: %+v %v", quotes, err)
	}
	agent, err := database.ListAssistantEntries(ctx, proto.AssistantEntriesParams{By: "agent", Type: "decision"}, true)
	if err != nil || len(agent.Entries) != 1 {
		t.Fatalf("agent decision replay: %+v %v", agent, err)
	}
}

func TestEntriesCollectOwnerCardAnswersAcrossSourceSpaces(t *testing.T) {
	database := todoTestDB(t)
	e := newTodoEnv(t, database)
	ctx := context.Background()
	e.f.setSessions("server", proto.Session{SessionID: "health-example", Assistant: true, Space: testSpace})
	ix := proto.Interaction{ID: "card-health-example", MachineID: "server", SessionID: "health-example", Kind: proto.IxQuestion, Questions: []proto.Question{{Question: "Keep the example reminder?", Options: []proto.QuestionOption{{Label: "Keep"}, {Label: "Skip"}}}}}
	if err := e.s.ledgerCardDecisions(ctx, ix, proto.Resolution{Answers: map[string]string{"Keep the example reminder?": "Keep"}}, "resolved"); err != nil {
		t.Fatal(err)
	}
	out, err := database.ListAssistantEntries(ctx, proto.AssistantEntriesParams{By: "user"}, true)
	if err != nil || len(out.Entries) != 1 || out.Entries[0].OriginSpace != testSpace || len(out.Entries[0].Category) != 0 {
		t.Fatalf("source/category: %+v %v", out, err)
	}
}
