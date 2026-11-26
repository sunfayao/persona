package filemeta

// The assistant on an agent runtime (docs/integrations/runtimes.md "Assistant"; DSH first): a runtime that declares
// the assistant capability runs the conversation, driven with the runtime protocol's own session calls. What is the
// assistant's stays here, as for Claude and Codex: the conversation's state (launcher, directory), the live snapshot
// each message starts with, the permission mode, the shared memory, and the gated skill commands Feishu goes through
// (larkgate.go; their credentials never leave the node). Nothing in this file is any one runtime's.

import (
	"os"
)

func Exists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
