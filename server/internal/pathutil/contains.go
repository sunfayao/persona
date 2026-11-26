package pathutil

import (
	"path/filepath"
	"strings"
)

// Contains returns true if sub is equal to base or is a descendant of base.
func Contains(sub, base string) bool {
	if sub == base {
		return true
	}
	rel, err := filepath.Rel(base, sub)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}
