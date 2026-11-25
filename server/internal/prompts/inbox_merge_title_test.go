package prompts

import (
	"strings"
	"testing"
)

// The merge title prompt asks for one JSON title and shows every merged row, in the order given (#7229).
func TestInboxMergeTitle(t *testing.T) {
	sys := RenderInboxMergeTitle(InboxMergeTitle{Owner: "Alex", Language: "English"})
	for _, want := range []string{"Alex", "in English", `{"title": "…"}`} {
		if !strings.Contains(sys, want) {
			t.Errorf("the system prompt lacks %q:\n%s", want, sys)
		}
	}
	if plain := RenderInboxMergeTitle(InboxMergeTitle{}); !strings.Contains(plain, "in Simplified Chinese") {
		t.Errorf("without a language the title is asked for in Simplified Chinese:\n%s", plain)
	}
	in := RenderInboxMergeTitleInput(InboxMergeTitleInput{Items: []InboxFolderItem{
		{Kind: "dm", Source: "Lark", Title: "Budget deadline", Sender: "Ana", Summary: "Ana asks for the numbers"},
		{Kind: "mail", Title: "Budget numbers", Preview: "Hi, attached"},
	}})
	i, j := strings.Index(in, "Budget deadline"), strings.Index(in, "Budget numbers")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("the rows are not shown in order:\n%s", in)
	}
	for _, want := range []string{"From: Ana", "Summary: Ana asks for the numbers", "Content: Hi, attached", "Type: dm (Lark)"} {
		if !strings.Contains(in, want) {
			t.Errorf("the input lacks %q:\n%s", want, in)
		}
	}
}
