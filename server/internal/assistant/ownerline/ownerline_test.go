package ownerline

import "testing"

func TestActionLineFindsAnExplicitRequestForTheOwnerToAct(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"All done.\nYou need to approve the deploy.", "You need to approve the deploy."},
		{"Would you like me to merge it?", "Would you like me to merge it?"},
		{"Everything is merged.\nNothing else.", ""},
		{"You do not need to do anything.", ""},
		{"需要你本人操作：点一下确认", "需要你本人操作：点一下确认"},
		{"不需要你本人操作", ""},
	} {
		if got := ActionLine(c.text); got != c.want {
			t.Errorf("%q: %q, want %q", c.text, got, c.want)
		}
	}
}
