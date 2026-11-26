package hub

import (
	"path/filepath"

	"github.com/sunfayao/persona/server/internal/identity"
)

// testSpaceDir is the directory of the track qa-private in home's state directory: a space whose information is
// private, as the tests need one.
func testSpaceDir(home string) string {
	return filepath.Join(identity.DataDir(home), "tracks", "qa-private")
}
