package codex

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
)

// NewestStateDB chooses the highest numbered Codex state database in paths.
func NewestStateDB(paths []string) string {
	best, bestN := "", -1
	for _, p := range paths {
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "state_"), ".sqlite"))
		if err == nil && n > bestN {
			best, bestN = p, n
		}
	}
	return best
}

// DecodeThreads decodes the SQLite JSON rows and projects titles that contain a complete opening tag.
func DecodeThreads(out []byte, titleOf func(string) string) (map[string]AppThread, error) {
	byID := map[string]AppThread{}
	if len(bytes.TrimSpace(out)) == 0 { // no row: none of the sidebar's chats is in the database
		return byID, nil
	}
	var rows []struct {
		AppThread
		ContextPrompt string `json:"contextPrompt"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.ContextPrompt != "" {
			if caption := titleOf(r.ContextPrompt); caption != "" {
				r.Title = caption
			}
		}
		byID[r.ID] = r.AppThread
	}
	return byID, nil
}
