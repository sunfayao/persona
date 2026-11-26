package shellcmd

import (
	"path/filepath"
	"strings"
)

// IsShellProgram reports whether the program name, including a login shell name, is a shell.
func IsShellProgram(name string) bool {
	switch strings.TrimPrefix(filepath.Base(name), "-") {
	case "sh", "bash", "zsh", "fish", "dash", "ksh", "tcsh", "csh", "nu", "pwsh", "elvish", "xonsh":
		return true
	}
	return false
}
