package node

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nwverse/persona/server/internal/node/harness/codex"
)

// readCodexProjectSelection is a read-only snapshot seam for selection tests.
var readCodexProjectSelection = codexDesktopProjectSnapshot

// parseCodexDesktopProjectID resolves a catalog source id to the current home's
// native API id. Migrated projects keep their legacy catalog identity, so a
// verified native-to-legacy alias must be reversed before using projectId.
func parseCodexDesktopProjectID(home, sourceID string) (string, error) {
	if sourceID == "" {
		return "", nil
	}
	canonicalHome := codex.CanonicalHome(home, "")
	if canonicalHome == "" {
		return "", errors.New("invalid Codex home for project selection")
	}
	id, ok := strings.CutPrefix(sourceID, codexDesktopProjectID(canonicalHome, ""))
	if !ok && home != canonicalHome {
		id, ok = strings.CutPrefix(sourceID, codexDesktopProjectID(home, ""))
	}
	if !ok || strings.TrimSpace(id) == "" {
		return "", errors.New("the selected Codex project belongs to a different profile or has an invalid identifier")
	}
	snapshot, err := readCodexProjectSelection(canonicalHome)
	if err != nil {
		return "", fmt.Errorf("read selected Codex project: %w", err)
	}
	// A card created by Persona may still hold the native id after the desktop
	// migrates its catalog row to the equivalent legacy id.
	if _, exists := snapshot.NativeProjectIDs[id]; exists {
		return id, nil
	}
	if _, exists := snapshot.Projects[id]; !exists {
		return "", errors.New("the selected Codex project no longer exists")
	}
	nativeID := ""
	for native, canonical := range snapshot.NativeProjectIDs {
		if canonical != id {
			continue
		}
		if nativeID != "" && nativeID != native {
			return "", errors.New("the selected Codex project has ambiguous native project mappings")
		}
		nativeID = native
	}
	if nativeID == "" {
		return "", errors.New("the selected legacy Codex project is not available through the desktop project API yet; open or update it in Codex first")
	}
	return nativeID, nil
}
