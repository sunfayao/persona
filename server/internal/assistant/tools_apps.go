package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/ownerfence"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/lazyre"
	"github.com/sunfayao/persona/server/internal/proto"
)

// The tools on what the hub keeps: its
// interaction queue (interaction_answer) and its inbox (inbox_draft), apps that take and return data (app.call
// interactions / inbox). The arguments, the rules and what the model reads are here.

// ---- interaction_answer (prompts/mcp/agents) ----

// toolInteractionAnswer answers another agent's question for the user. Only a question may be
// answered here: a permission, a plan and a terminal dialog are the user's to decide, and an assistant conversation
// (this one among them) is never answered by the assistant.
func (s *Service) toolInteractionAnswer(ctx context.Context, self string, args json.RawMessage) (string, error) {
	var a struct {
		Machine       string          `json:"machine"`
		SessionID     string          `json:"session_id"`
		InteractionID string          `json:"interaction_id"`
		Answers       json.RawMessage `json:"answers"`
		Reason        string          `json:"reason"`
	}
	_ = json.Unmarshal(args, &a)
	observer, observes := ownerfence.ObserverOf(ctx)
	space := s.agentCallerSpace(ctx, observer.Machine, self)
	am, c, err := s.findAgentInSpace(ctx, a.Machine, a.SessionID, &space)
	if err != nil {
		return "", err
	}
	if c.Assistant || c.SessionID == self {
		return "", errors.New("that interaction belongs to an assistant conversation; only the user answers it")
	}
	id := strings.TrimSpace(a.InteractionID)
	var got struct {
		Interaction proto.Interaction `json:"interaction"`
		OK          bool              `json:"ok"`
		ownerfence.QuestionEnd
	}
	if err := s.appCall(ctx, AppInteractions, assistantwire.InteractionsGet, map[string]any{
		"interaction_id": id, "machine_id": am.ID, "session_id": c.SessionID,
	}, &got); err != nil {
		return "", err
	}
	if !got.OK {
		return "", got.ToolError(id)
	}
	ix := got.Interaction
	if ix.Kind == proto.IxScreen {
		// A terminal dialog (Claude's Bypass Permissions confirmation, a safety check): its card offers the user its
		// choices; it is never answered for them.
		return "", fmt.Errorf("interaction %s is a screen (a terminal dialog), only the user can answer it in the app, where its card offers its choices", ix.ID)
	}
	if ix.Kind != proto.IxQuestion {
		return "", fmt.Errorf("interaction %s is a %s (approval), only the user can answer it in the app", ix.ID, ix.Kind)
	}
	if ix.MachineID != am.ID || ix.SessionID != c.SessionID {
		return "", fmt.Errorf("interaction %s belongs to another agent: machine=%s session_id=%s", ix.ID, ix.MachineID, ix.SessionID)
	}
	if time.Now().After(ix.ExpiresAt) {
		return "", fmt.Errorf("interaction %s expired", ix.ID)
	}
	if observes {
		ctx, err = s.supervisoryAction(ctx, observer.Machine, observer.Session, am.ID, c.SessionID, ix.CreatedAt)
		if err != nil {
			return "", err
		}
	}
	answers, err := ownerfence.InteractionAnswers(ix, a.Answers)
	if err != nil {
		return "", err
	}
	// A card handed to this conversation before the user (ask_route.go): its answer is the task's decision.
	todo, routed := s.routedTo(ix.ID, self, s.isMain(ctx, self))
	var res ownerfence.QuestionEnd
	if err := s.appCall(ctx, AppInteractions, assistantwire.InteractionsAnswer, map[string]any{"machine_id": ix.MachineID,
		"session_id": ix.SessionID, "interaction_id": ix.ID, "answers": answers, "reason": clip(oneLine(a.Reason), 120)}, &res); err != nil {
		return "", err
	}
	if !res.OK {
		return "", res.ToolError(ix.ID)
	}
	if routed {
		s.askAnswered(ctx, todo, self, ix, answers, a.Reason)
	}
	var b strings.Builder
	b.WriteString("Answered for the user:\n")
	for _, q := range ix.Questions {
		fmt.Fprintf(&b, "- %s → %s\n", q.Question, answers[q.Question])
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// ---- inbox_draft (prompts/mcp/inbox) ----

// placeholderRE: a blank a draft leaves for the user to fill in — text in full-width lenticular brackets, {{…}}.
var placeholderRE = lazyre.New(`【[^】\n]*】|\{\{[^}\n]*\}\}`)

// draftPlaceholder is the first blank in a draft left to fill in; empty when it has none.
func draftPlaceholder(text string) string {
	return placeholderRE.FindString(text)
}

// toolInboxDraft gives the inbox the draft of the AI twin working in conversation self (inboxDraft):
// it waits on the item for the user to send.
func (s *Service) toolInboxDraft(ctx context.Context, self string, args json.RawMessage) (string, error) {
	var a struct {
		Text, Note     string
		Changes        []DocChange
		ProcessingTurn string `json:"processing_turn"`
	}
	if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Text) == "" {
		return "", errors.New("text is required")
	}
	// The user sends it with one tap: a blank left to fill in (a bracketed "by what time") would go out as it is.
	if p := draftPlaceholder(a.Text); p != "" {
		return "", fmt.Errorf("the draft leaves %s to fill in, and it goes out as it is: leave out what you could not find (say so in note) and submit again", p)
	}
	itemID, refused, err := s.inboxDraft(ctx, self, strings.TrimSpace(a.Text), clip(strings.TrimSpace(a.Note), 600), a.Changes, a.ProcessingTurn)
	if err != nil { // not kept: the event is back where the turn found it, so the draft can be handed in again
		return "", err
	}
	if itemID == "" {
		return "", errors.New("this conversation was not started for an inbox item")
	}
	// Only moves the item offers (a row it lists, one of the column's values).
	if refused != nil {
		return "", fmt.Errorf("change %+v is not one the item lists (ref, field and one of its values)", *refused)
	}
	return "The draft is in the user's inbox; they will send it. You're done.", nil
}
