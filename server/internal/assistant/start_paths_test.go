package assistant

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Every way Persona starts an Agent or opens a conversation is supervised by default (#4669): it either gives the
// work a supervision ledger (a task the main conversation or a leader watches, or a spawned conversation the hub
// follows until it reports back), or it is on the opt-out list below with the reason. The test finds the start paths
// in the source itself: every function that calls a start primitive (session.start on a node, or one of the
// functions that open an assistant conversation) must be listed, so a new entry point cannot skip supervision
// silently. A wrapper (a function that only passes a start on) is followed to its own callers instead.

// startPrimitives open an assistant conversation; a call with DeviceSessionStart starts an Agent on a node.
var startPrimitives = []string{"startOn", "start", "startConversationOn", "startConversation", "Start", "startMain", "startSignedIn"}

// startWrappers pass a start on: their callers are the entry points.
var startWrappers = map[string]string{
	"startTaskAgent": "a planned task's agent start (tasks_placement.go), called by runPlanTaskStart",
	// The same supervised start into a prepared backend workspace (#11687), called only by startTaskAgent.
	"startBackendTask": "a planned task's prepared-backend agent start (tasks_backend_launch.go), called by startTaskAgent",
}

// pkgFunc is a function of the package as parsed: its file, the names it calls (the Service's methods, s.x; a local
// function value called start is no start), and every identifier in it.
type pkgFunc struct {
	file                   string
	calls                  map[string]bool
	idents                 map[string]bool
	hasCall                bool
	startOutsideComparison bool
}

func (f pkgFunc) callsAny(names []string) bool {
	return slices.ContainsFunc(names, func(n string) bool { return f.calls[n] })
}

func (f pkgFunc) mentions(ident string) bool { return f.idents[ident] }

// parsePackageFuncs reads the package's own files (no tests); a method and a function of one name are merged.
func parsePackageFuncs(t *testing.T, dir string) map[string]pkgFunc {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[string]pkgFunc{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			f, ok := out[fd.Name.Name]
			if !ok {
				f = pkgFunc{file: path, calls: map[string]bool{}, idents: map[string]bool{}}
			}
			ast.PreorderStack(fd.Body, nil, func(n ast.Node, parents []ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					f.hasCall = true
					switch fn := x.Fun.(type) {
					case *ast.SelectorExpr: // the Service's own methods only (s.start, not a timer's or a process's)
						if recv, ok := fn.X.(*ast.Ident); ok && recv.Name == "s" {
							f.calls[fn.Sel.Name] = true
						}
					}
				case *ast.Ident:
					f.idents[x.Name] = true
					if x.Name == "DeviceSessionStart" {
						expr, comparison := parents[len(parents)-1].(*ast.BinaryExpr)
						if !comparison || (expr.Op != token.EQL && expr.Op != token.NEQ) {
							f.startOutsideComparison = true
						}
					}
				}
				return true
			})
			out[fd.Name.Name] = f
		}
	}
	return out
}

// startEntries are the functions that start an Agent (a call with DeviceSessionStart) or call a start primitive, the
// wrappers followed to their callers; the primitives themselves, and the wrappers, are not entries.
func startEntries(funcs map[string]pkgFunc) []string {
	starts := slices.Clone(startPrimitives)
	for w := range startWrappers {
		starts = append(starts, w)
	}
	seen := map[string]bool{}
	for name, f := range funcs {
		if slices.Contains(startPrimitives, name) || startWrappers[name] != "" {
			continue // a primitive or a wrapper is no entry of its own: its callers are
		}
		// A pure method-name comparison (tracksNodeWrite) sends nothing. Keep a
		// comparison followed by a dynamic call and every other use of the method.
		if (f.mentions("DeviceSessionStart") && (f.hasCall || f.startOutsideComparison)) || f.callsAny(starts) {
			seen[name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// A method-name classifier is not a start. Passing, forwarding, or checking a
// method before invoking it must still expose that entry to the supervision guard.
func TestStartPathsDistinguishMethodChecksFromWrites(t *testing.T) {
	dir := t.TempDir()
	const src = `package example
func classifies(m string) bool { return m == DeviceSessionStart || m != DeviceSessionSend }
func direct() { call(DeviceSessionStart) }
func forwarded() string { return DeviceSessionStart }
func checked(m string) { if m == DeviceSessionStart { call(m) } }
func countsAsAResume(s *Service) { s.start() }
`
	if err := os.WriteFile(filepath.Join(dir, "starts.go"), []byte(src), 0600); err != nil {
		t.Fatal(err)
	}
	got := startEntries(parsePackageFuncs(t, dir))
	if want := []string{"checked", "countsAsAResume", "direct", "forwarded"}; !slices.Equal(got, want) {
		t.Errorf("start entries = %v, want %v", got, want)
	}
}
