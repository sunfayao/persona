package codex

import "testing"

func TestPermissionModeReadsFullAccessFromTheTurnPolicy(t *testing.T) {
	for _, c := range []struct{ approval, sandbox, want string }{
		{"never", "danger-full-access", "bypassPermissions"},
		{"on-request", "workspace-write", "default"},
		{"untrusted", "read-only", "default"},
		{"on-request", "danger-full-access", "default"},
		{"never", "workspace-write", "default"},
		{"", "workspace-write", "default"},
		{"", "", ""},
	} {
		if got := PermissionMode(c.approval, c.sandbox); got != c.want {
			t.Errorf("PermissionMode(%q, %q) = %q, want %q", c.approval, c.sandbox, got, c.want)
		}
	}
}
