package assistant

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/prompts"
)

func TestGoldenStandingAuthorizationDispatch(t *testing.T) {
	golden(t, "standing_authorizations.task", prompts.RenderStandingAuthorizations(prompts.StandingAuthorizations{
		Text: "Fix the login crash and run the affected tests.",
		Grants: []prompts.StandingAuthorization{{
			ID: "auth_example", MachineID: "box", Directory: "/code/app", Actions: []string{"merge", "deploy-test"},
			Conditions: []string{"CI is green", "No merge conflicts"},
			Quote:      "Merge this repository once its checks pass; no further review confirmation is needed.",
			GrantedAt:  "2026-11-11T07:36:00Z", Source: "Main conversation", Reversible: true,
		}},
	}))
}
