package node

import (
	"testing"

	repository "github.com/sunfayao/persona/server/internal/repositorywire"
)

func TestBackendWorkspaceProviderIdentityIsRechecked(t *testing.T) {
	valid := repository.SourceSnapshot{Identity: repository.Identity{Provider: "ext:example/source", InstanceURL: "https://code.example.com", Path: "example/web", CanonicalURL: "https://code.example.com/example/web"}, CloneURL: "https://code.example.com/example/web.git", ProviderName: "Example source"}
	for _, tc := range []struct {
		name   string
		change func(*repository.SourceSnapshot)
		want   bool
	}{
		{"valid", func(*repository.SourceSnapshot) {}, true},
		{"canonical mismatch", func(s *repository.SourceSnapshot) { s.Identity.CanonicalURL += "-other" }, false},
		{"path traversal", func(s *repository.SourceSnapshot) {
			s.Identity.Path = "../web"
			s.Identity.CanonicalURL = s.Identity.InstanceURL + "/" + s.Identity.Path
		}, false},
		{"instance credentials", func(s *repository.SourceSnapshot) {
			s.Identity.InstanceURL = "https://example:example@code.example.com"
			s.Identity.CanonicalURL = s.Identity.InstanceURL + "/" + s.Identity.Path
		}, false},
		{"noncanonical port", func(s *repository.SourceSnapshot) {
			s.Identity.InstanceURL = "https://code.example.com:443"
			s.Identity.CanonicalURL = s.Identity.InstanceURL + "/" + s.Identity.Path
		}, false},
		{"clone credentials", func(s *repository.SourceSnapshot) {
			s.CloneURL = "https://example:example@code.example.com/example/web.git"
		}, false},
		{"empty host", func(s *repository.SourceSnapshot) {
			s.Identity.InstanceURL = "https://"
			s.Identity.CanonicalURL = s.Identity.InstanceURL + "/" + s.Identity.Path
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := valid
			tc.change(&s)
			if got := (&Node{}).workspaceSourceAllowed(repository.PreparationRepository{SourceSnapshot: s}); got != tc.want {
				t.Fatalf("source allowed=%v, want %v", got, tc.want)
			}
		})
	}
}
