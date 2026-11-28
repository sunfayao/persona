package assistant

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestManagerContentUsesTurnModel(t *testing.T) {
	files := []string{
		"agy_unlinked.go", "agent_tasks.go", "channels_runs.go", "done.go", "feishu_steps.go", "fleet.go",
		"agents/a2a_delivery.go", "../assistantwire/handler.go", "../assistantwire/wire.go",
	}
	for _, path := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			if isLegacyTurnContent(value) {
				t.Errorf("%s: move the legacy transcript marker %q into turnmodel", fset.Position(lit.Pos()), value)
			}
			return true
		})
	}
}

func isLegacyTurnContent(value string) bool {
	for _, marker := range []string{"agy#0", "API Error:", "已中断", "（正在重试）", "上下文已压缩", "Please run /login"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
