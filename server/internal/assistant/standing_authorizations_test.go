package assistant

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/prompts"
)

func standingRegistryGrant() StandingAuthorization {
	return StandingAuthorization{MachineID: "box", Directory: "/code/example", Actions: []string{"merge"}, Conditions: []string{"ci_green", "no_conflicts"}, Quote: "Merge the example project when checks pass and there are no conflicts.", Source: "main conversation, user message", Reversible: true}
}
func standingJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestStandingAuthorizationRejectsBroadOrMutatedGrants(t *testing.T) {
	for _, dir := range []string{"/", "/code", "~/code/example", "relative/path", "/code/*", "/code/app/../other", "/code/example/"} {
		grant := standingRegistryGrant()
		grant.Directory = dir
		if _, err := prepareStandingChange(StandingAuthorizations{}, StandingAuthorizations{Authorizations: []StandingAuthorization{grant}}, time.Now()); err == nil {
			t.Errorf("broad/ambiguous: %q", dir)
		}
	}
	for _, field := range []string{"machine", "actions", "quote", "source"} {
		grant := standingRegistryGrant()
		switch field {
		case "machine":
			grant.MachineID = ""
		case "actions":
			grant.Actions = []string{"all"}
		case "quote":
			grant.Quote = ""
		case "source":
			grant.Source = ""
		}
		if err := checkStandingGrant(grant); err == nil {
			t.Errorf("invalid %s", field)
		}
	}
	grant := standingRegistryGrant()
	grant.ID = "sa_existing"
	grant.GrantedAt = time.Now()
	before := StandingAuthorizations{Revision: 1, Authorizations: []StandingAuthorization{grant}}
	grant.Actions = []string{"merge", "deploy-production"}
	if _, err := prepareStandingChange(before, StandingAuthorizations{Revision: 1, Authorizations: []StandingAuthorization{grant}}, time.Now()); err == nil {
		t.Fatal("existing grant silently expanded")
	}
}
func TestGoldenStandingAuthorizationSettingsDescription(t *testing.T) {
	golden(t, "standing_authorizations.settings_description", prompts.Tool("persona_settings").Description)
}
