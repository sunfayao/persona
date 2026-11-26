package filemeta

import (
	"os"
	"sort"
	"time"
)

// newestFirst orders the copies of a transcript by modification time, newest first.
func NewestFirst(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	mod := map[string]time.Time{}
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil {
			mod[p] = st.ModTime()
		}
	}
	sort.SliceStable(paths, func(i, j int) bool { return mod[paths[i]].After(mod[paths[j]]) })
	return paths
}
