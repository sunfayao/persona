package pathutil

import (
	"path/filepath"
)

// realPath is p with its symlinks resolved as far as it exists: its longest existing ancestor (a file about to be
// written, a checkout to be made, need not exist yet), so two spellings of one place compare — a path written through a
// link (~/.claude/..., macOS's /var for /private/var) and a resolved one. p as it is when nothing of it resolves.
func Real(p string) string {
	rest := ""
	for q := p; ; {
		if real, err := filepath.EvalSymlinks(q); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(q)
		if parent == q {
			return p
		}
		rest = filepath.Join(filepath.Base(q), rest)
		q = parent
	}
}
