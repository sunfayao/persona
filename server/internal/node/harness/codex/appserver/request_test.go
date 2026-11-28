package appserver

import (
	"encoding/json"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestRequestCardRetainsQuestionAndFileFields(t *testing.T) {
	t.Parallel()
	raw := `{"grantRoot":"fallback","questions":[{"id":"test","header":"Mode","question":"Which mode?","options":[{"label":"Quick","description":"Run the small checks"}]}]}`
	var request Request
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		t.Fatal(err)
	}
	card := proto.Interaction{ID: "stable"}
	request.PopulateCard("item/tool/requestUserInput", &card, nil)
	if card.ID != "stable" || !card.CanCancel || card.Kind != proto.IxQuestion || card.ToolName != "request_user_input" ||
		len(card.Questions) != 1 || len(card.Questions[0].Options) != 1 || card.Questions[0].Options[0].Label != "Quick" {
		t.Fatalf("question card = %+v", card)
	}
	var change proto.Interaction
	request.PopulateCard("item/fileChange/requestApproval", &change, []string{"", "example.txt"})
	if change.Kind != proto.IxPermission || change.ToolName != "Edit" || string(change.ToolInput) != `{"file_path":"example.txt"}` {
		t.Fatalf("file card = %+v", change)
	}
}

func TestPermissionGrantContainsOnlyRequestedScopes(t *testing.T) {
	t.Parallel()
	r := Request{Permissions: json.RawMessage(`{"filesystem":{"read":["example.txt"]},"network":null}`)}
	grant, err := json.Marshal(r.PermissionGrant())
	if err != nil || string(grant) != `{"filesystem":{"read":["example.txt"]}}` {
		t.Fatalf("grant = %s, %v", grant, err)
	}
	if grant := (Request{Permissions: json.RawMessage("null")}).PermissionGrant(); grant == nil || len(grant) != 0 {
		t.Fatalf("null grant = %+v", grant)
	}
}
