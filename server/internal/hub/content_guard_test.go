package hub

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const hubContentUpdate = "UPDATE_HUB_CONTENT_BASELINE"

func TestContentGuardOnlyLosesCLISentinels(t *testing.T) {
	const baselinePath = "testdata/content_guard_baseline.json"
	actual := map[string]int{}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "testdata" || entry.Name() == "webdist") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		count, err := countHubContentSentinels(path, src)
		if count > 0 {
			actual[filepath.ToSlash(path)] = count
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	var baseline map[string]int
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}

	grew, stale := false, false
	paths := map[string]bool{}
	for path := range baseline {
		paths[path] = true
	}
	for path := range actual {
		paths[path] = true
	}
	sorted := make([]string, 0, len(paths))
	for path := range paths {
		sorted = append(sorted, path)
	}
	sort.Strings(sorted)
	for _, path := range sorted {
		count, present := actual[path]
		old, known := baseline[path]
		if count > old {
			t.Errorf("%s: %d -> %d CLI sentinels; move this logic to the node-side adapter or a capability flag", path, old, count)
			grew = true
		} else if known && (!present || count < old) {
			stale = true
			if os.Getenv(hubContentUpdate) != "1" {
				t.Errorf("%s: %d -> %d CLI sentinels; run with %s=1 to lower the baseline", path, old, count, hubContentUpdate)
			}
		}
	}
	if !grew && stale && os.Getenv(hubContentUpdate) == "1" {
		writeHubContentBaseline(t, baselinePath, actual)
	}
}

func writeHubContentBaseline(t *testing.T, path string, counts map[string]int) {
	t.Helper()
	data, err := json.MarshalIndent(counts, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func countHubContentSentinels(path string, src []byte) (int, error) {
	itemPath := false
	switch filepath.ToSlash(path) {
	case "transcript_text.go", "injected.go", "card_compat.go", "ttft.go":
		itemPath = true
	}
	if !itemPath && !bytes.Contains(src, []byte("Item")) {
		return 0, nil
	}
	tree, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
	if err != nil {
		return 0, err
	}
	cli := regexp.MustCompile(`(?i:codex|agy|antigravity|traex|paseo)|Omp|(?:^|[^a-zA-Z])omp(?:[^a-zA-Z]|$)`)
	count := 0
	ast.Inspect(tree, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.BasicLit:
			if n.Kind == token.STRING {
				value, _ := strconv.Unquote(n.Value)
				if cli.MatchString(value) {
					count++
				}
			}
		case *ast.Ident:
			if cli.MatchString(n.Name) {
				count++
			}
		case *ast.SelectorExpr:
			id, ok := n.X.(*ast.Ident)
			if ok && id.Name == "proto" && n.Sel.Name == "Item" {
				itemPath = true
			}
			if ok && id.Name == "transcript" && strings.HasPrefix(n.Sel.Name, "Runtime") && !cli.MatchString(n.Sel.Name) {
				// Codex, Paseo, Omp and RuntimeAgy constants are already counted by their identifiers.
				count++
			}
		}
		return true
	})
	if !itemPath {
		return 0, nil
	}
	return count, nil
}

func TestContentGuardCountsOnlyCodeOnItemPaths(t *testing.T) {
	src := []byte(`package hub
// codex
func f(item proto.Item) string {
	return "codex" + codexName + transcript.RuntimeError + transcript.PaseoScheduleNotice
}
`)
	if count, err := countHubContentSentinels("elsewhere.go", src); err != nil || count != 4 {
		t.Fatalf("count = %d, error = %v; want 4 sentinels", count, err)
	}
	src = []byte(`package hub; const launcher = "codex"`)
	if count, err := countHubContentSentinels("launcher.go", src); err != nil || count != 0 {
		t.Fatalf("count = %d, error = %v; want no sentinels outside an item path", count, err)
	}
}
