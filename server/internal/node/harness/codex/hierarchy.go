package codex

import (
	"github.com/nwverse/persona/server/internal/proto"
)

// Public IDs isolate Codex homes. Missing parents remain explicit; cycles terminate.
func ResolveHierarchy(sessions []proto.Session) {
	byID := make(map[string]int, len(sessions))
	for i, s := range sessions {
		byID[s.SessionID] = i
	}
	for i := range sessions {
		s := &sessions[i]
		if !s.IsSubagent {
			continue
		}
		visited := map[string]bool{s.SessionID: true}
		parent := s.ParentSessionID
		rootIndex := -1
		metadataRoot := s.RootSessionID
		for parent != "" && !visited[parent] {
			visited[parent] = true
			s.RootSessionID = parent
			index, ok := byID[parent]
			if !ok {
				if metadataRoot != "" {
					s.RootSessionID = metadataRoot
					if idx, exists := byID[metadataRoot]; exists {
						rootIndex = idx
					}
				}
				parent = ""
				break
			}
			rootIndex = index
			parent = sessions[index].ParentSessionID
		}
		if parent != "" && visited[parent] {
			s.RootSessionID = ""
		}
		if rootIndex >= 0 {
			root := sessions[rootIndex]
			s.RepoRoot, s.SpaceID, s.SpaceKind, s.SpaceName, s.SpaceIndex = root.RepoRoot, root.SpaceID, root.SpaceKind, root.SpaceName, root.SpaceIndex
		}
	}
}
