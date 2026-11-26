package codex

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// ReadThreadTitles reads the latest name and preceding names of each thread in the session index.
func ReadThreadTitles(path string) (map[string]string, map[string][]string) {
	titles, former := map[string]string{}, map[string][]string{}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var e struct {
				ID   string `json:"id"`
				Name string `json:"thread_name"`
			}
			if json.Unmarshal(sc.Bytes(), &e) == nil && e.ID != "" && strings.TrimSpace(e.Name) != "" {
				name := strings.TrimSpace(e.Name)
				if last, ok := titles[e.ID]; ok && last != name {
					former[e.ID] = append(former[e.ID], last)
				}
				titles[e.ID] = name
			}
		}
		f.Close()
	}
	return titles, former
}
