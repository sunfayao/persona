package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/sunfayao/persona/server/internal/compat"
	"github.com/sunfayao/persona/server/internal/pathutil"
	"github.com/sunfayao/persona/server/internal/proto"
)

// canonicalCodexHome is home absolute, with its links resolved as far as it exists: a home that does not exist yet
// (Codex not used yet when the node starts) canonicalizes the same as it will once it does, so LegacyHome, fixed on
// the first configure, still equals that home in Homes later under a linked ancestor (macOS's /var, a linked /home).
func CanonicalHome(home, base string) string {
	p := proto.ExpandHome(home, base)
	if !filepath.IsAbs(p) {
		return ""
	}
	return pathutil.Real(filepath.Clean(p))
}

func SourceID(home, raw, legacy string) string {
	if home == legacy {
		return raw
	}
	h := sha256.Sum256([]byte(compat.CodexHashPrefix() + home + "\x00" + raw))
	// The reserved shape cannot collide with a raw UUID and still satisfies validID.
	return "c0de-" + hex.EncodeToString(h[:16])
}
