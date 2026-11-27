package assistant

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/prompts"
)

func TestGoldenConfirmationBriefFacts(t *testing.T) {
	golden(t, "routine_facts.confirmations", prompts.RenderBriefFacts(prompts.BriefFacts{Hours: 12,
		ConfirmationSummary: "Today: 3 confirmation cards sent to you · 2 automatically authorized",
		Confirmations:       []string{"Build the app · question: 2 sent to you · 1 automatically authorized", "Review the app · plan: 1 sent to you · 1 automatically authorized"}}))
}
