package codex

import (
	"encoding/json"
	"errors"
	"os"
)

type DesktopSidebar struct {
	Unread      map[string]bool
	Projectless []string
	Assignments map[string]string
}

// ReadDesktopSidebar reads the App's legacy sidebar and the read markers for local threads.
func ReadDesktopSidebar(path string) (DesktopSidebar, bool) {
	var raw struct {
		ReadState struct {
			UnreadByIdentity map[string]map[string][]string `json:"unreadByIdentity"`
		} `json:"electron-thread-read-state-v1"`
		Projectless []string `json:"projectless-thread-ids"`
		Assignments map[string]struct {
			ProjectID string `json:"projectId"`
		} `json:"thread-project-assignments"`
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return DesktopSidebar{}, false
	}
	if err == nil && json.Unmarshal(b, &raw) != nil {
		return DesktopSidebar{}, false
	}
	unread := map[string]bool{}
	for _, hosts := range raw.ReadState.UnreadByIdentity {
		for host, ids := range hosts {
			if len(host) < 6 || host[:6] != "local:" {
				continue
			}
			for _, id := range ids {
				unread[id] = true
			}
		}
	}
	assignments := make(map[string]string, len(raw.Assignments))
	for id, assignment := range raw.Assignments {
		assignments[id] = assignment.ProjectID
	}
	return DesktopSidebar{Unread: unread, Projectless: raw.Projectless, Assignments: assignments}, true
}
