package assistant

import (
	"testing"

	"github.com/sunfayao/persona/server/internal/prompts"
)

func TestGoldenViewTools(t *testing.T) {
	for _, name := range []string{"view"} {
		tool := prompts.Tool(name)
		golden(t, name, tool.Description+"\n\n"+string(tool.InputSchema)+"\n")
	}
}
