package sessionlist

import "testing"

func TestDirectoryAliasesUseOnlyVerifiedEquivalences(t *testing.T) {
	t.Parallel()
	a := NewDirectoryAliases("/home/example", "/data00/home/example")
	a.Observe("/links/app", "/work/app")
	a.Observe("/links/other", "/work/other")
	a.Observe("/links/other", "/elsewhere/other")
	for _, tc := range []struct{ cwd, verified, want string }{
		{"~/code", "", "/data00/home/example/code"},
		{"/home/example/code", "", "/data00/home/example/code"},
		{"/home/example-other/code", "", "/home/example-other/code"},
		{"/home/example/code", "/vol/other", "/vol/other"},
		{"/links/app", "", "/work/app"},
		{"/links/other", "", "/links/other"},
		{"relative", "", ""},
	} {
		if got := a.Path(tc.cwd, tc.verified); got != tc.want {
			t.Errorf("Path(%q, %q) = %q, want %q", tc.cwd, tc.verified, got, tc.want)
		}
	}
}
