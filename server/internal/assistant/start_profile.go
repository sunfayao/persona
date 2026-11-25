package assistant

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/compat"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// signedInProfile is the Claude profile (config_dir) the hub's own conversations start in.
const signedInProfile = "legacy"

// startSignedIn starts p in signedInProfile, or as it is where there is no profile to start in: on a node too old to be
// told one (the hub refuses the profile before anything starts, devices/machine.go), and on a launcher that runs
// another agent than Claude Code, which alone has profiles. startOn leaves the profile out for a launcher it knows runs
// another agent; one it cannot tell (the machine's launcher list did not come, …) the node refuses it for before
// anything starts (#6884). Either way the second start is the only one.
func (s *Service) startSignedIn(ctx context.Context, p StartParams) (proto.Session, error) {
	p.ConfigDir = signedInProfile
	sess, err := s.start(ctx, p)
	if nodeOutdated(err) || profileRefused(err) {
		p.ConfigDir = ""
		return s.start(ctx, p)
	}
	return sess, err
}

// nodeOutdated: the hub refused a call for something the node is too old for.
func nodeOutdated(err error) bool {
	var re *rpc.Error
	return errors.As(err, &re) && re.Code == proto.CodeNodeOutdated
}

// profileRefused: the node refused a config_dir because the launcher runs another agent than Claude Code, before
// starting anything (assistantwire.ConfigDirClaudeOnly; its own words, also from a node older than the constant).
func profileRefused(err error) bool {
	return err != nil && strings.Contains(err.Error(), assistantwire.ConfigDirClaudeOnly)
}

// unknownMethod: a node answered that it does not know the method (an older node).
func unknownMethod(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unknown method")
}

// runtimeSource: a launcher a runtime adds to a machine's list (DSH, Paseo, agy…), not one of the machine's own shell
// or PATH launchers, a provider added in Persona or plain claude.
func runtimeSource(raw string) bool {
	source := compat.Source(raw)
	return source != "" && source != "persona" && source != "builtin" && !machineSource(source)
}

// promptSourceUser is a hook event's prompt_source for the user's own words (node/hooks.go promptSource).
const promptSourceUser = "user"

// expandHome turns a leading "~/" into this machine's home.
func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return p
}

// clipBytes cuts s to at most n bytes without splitting a character.
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
