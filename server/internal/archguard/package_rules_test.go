package archguard

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	maxPackageFiles = 80
	maxPackageLines = 15000
	packageBaseline = "package_rules_baseline.json"
	lowerBaseline   = "run UPDATE_GOLDEN=1 go test ./internal/archguard -run '^TestPackageRules$' from server"
)

type packageSize struct {
	Files int `json:"files"`
	Lines int `json:"lines"`
}

type packageRules struct {
	Imports map[string]string      `json:"imports"`
	Sizes   map[string]packageSize `json:"sizes"`
}

// TestPackageRules reads source directly so build-tagged code and platform-specific
// files count even when the current go toolchain would exclude them.
func TestPackageRules(t *testing.T) {
	data, err := os.ReadFile(packageBaseline)
	if err != nil {
		t.Fatal(err)
	}
	var baseline packageRules
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}

	sizes, imports := scanInternalPackages(t, "..")
	next := packageRules{Imports: map[string]string{}, Sizes: map[string]packageSize{}}
	for edge := range imports {
		if reason := baseline.Imports[edge]; strings.TrimSpace(reason) != "" {
			next.Imports[edge] = reason
		} else {
			t.Errorf("domain import %s crosses a package boundary; use a contract or inject the dependency", edge)
		}
	}
	for pkg, size := range sizes {
		limit, recorded := baseline.Sizes[pkg]
		if !recorded {
			limit = packageSize{Files: maxPackageFiles, Lines: maxPackageLines}
		}
		if size.Files > limit.Files || size.Lines > limit.Lines {
			t.Errorf("%s has %d non-test Go files and %d lines (limit %d files and %d lines); move code out of the package. After a reduction, %s",
				pkg, size.Files, size.Lines, limit.Files, limit.Lines, lowerBaseline)
		}
		if size.Files > maxPackageFiles || size.Lines > maxPackageLines {
			next.Sizes[pkg] = size
		}
	}
	if t.Failed() {
		return
	}
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		data, err := json.MarshalIndent(next, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(packageBaseline, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	for edge := range baseline.Imports {
		if _, stillUsed := next.Imports[edge]; !stillUsed {
			t.Errorf("obsolete import exception %s; %s", edge, lowerBaseline)
		}
	}
}

func scanInternalPackages(t *testing.T, dir string) (map[string]packageSize, map[string]bool) {
	t.Helper()
	sizes := map[string]packageSize{}
	imports := map[string]bool{}
	if err := fs.WalkDir(os.DirFS(dir), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			base := entry.Name()
			if name != "." && (base == "testdata" || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_")) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
			return nil
		}
		pkg := path.Join("internal", path.Dir(name))
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if !strings.HasSuffix(name, "_test.go") {
			size := sizes[pkg]
			size.Files++
			size.Lines += bytes.Count(data, []byte{'\n'})
			if len(data) != 0 && data[len(data)-1] != '\n' {
				size.Lines++
			}
			sizes[pkg] = size
		}
		if domainRoot(pkg) == "" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, data, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			quoted, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if dep, ok := strings.CutPrefix(quoted, modulePath); ok && crossesDomain(pkg, dep) {
				imports[pkg+" -> "+dep] = true
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return sizes, imports
}

func domainRoot(pkg string) string {
	for _, root := range []string{"internal/assistant", "internal/node", "internal/hub", "internal/devices", "internal/extmgr"} {
		if strings.HasPrefix(pkg, root+"/") {
			return root
		}
	}
	return ""
}

func crossesDomain(pkg, dep string) bool {
	root := domainRoot(pkg)
	return dep == root || (under(dep, root) && !under(pkg, dep) && !under(dep, pkg))
}
