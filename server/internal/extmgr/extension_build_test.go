package extmgr

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The package carries the manifest under both names and without its tags: hubs from before the rename read only
// persona-plugin.json, and hubs from before tags refuse a manifest with a field they don't know.
func TestPackWritesTheManifestForOlderHubs(t *testing.T) {
	dir := t.TempDir()
	src := `{"manifest_version": 1, "id": "tagged", "name": "Tagged", "version": "1.0.0", "tags": ["示例", "开发"],
 "contributes": {"prompts": [{"id": "hi", "kind": "quick", "title": "Hi", "text": "Say hi."}]}}`
	if err := os.WriteFile(filepath.Join(dir, "persona-extension.json"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if err := packExtension(context.Background(), dir, nil, &out, &errs); err != nil {
		t.Fatalf("pack: %v\n%s", err, errs.String())
	}
	files := tarFiles(t, filepath.Join(dir, "dist", "tagged-1.0.0.tar.gz"))
	extensionJSON, pluginJSON := files["tagged-1.0.0/persona-extension.json"], files["tagged-1.0.0/persona-plugin.json"]
	if extensionJSON == "" || extensionJSON != pluginJSON {
		t.Fatalf("the two manifests: %q and %q", extensionJSON, pluginJSON)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(extensionJSON), &fields); err != nil || fields["tags"] != nil || fields["id"] != "tagged" {
		t.Fatalf("packaged manifest %v: %v", fields, err)
	}
	if src, _ := os.ReadFile(filepath.Join(dir, "persona-extension.json")); !strings.Contains(string(src), `"tags"`) {
		t.Fatal("pack changed the source manifest")
	}
}

func tarFiles(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		files[h.Name] = string(b)
	}
	return files
}

func TestNewRefusesBadInput(t *testing.T) {
	var out bytes.Buffer
	for _, c := range [][3]string{{"Bad ID", "go", "x"}, {"ok", "rust", "x"}, {"ok", "go", `a"b`}} {
		if err := newExtension(c[0], c[1], c[2], filepath.Join(t.TempDir(), "x"), &out); err == nil {
			t.Errorf("new %v: no error", c)
		}
	}
	// The refusals read in the terminal's language (#5520).
	for _, tt := range []struct{ lang, template, want string }{
		{"en", "rust", `The templates are go and ts; there is no "rust"`},
		{"ja", "rust", `テンプレートは go と ts だけです（"rust" はありません）`},
		{"zh-Hans", "rust", `模板只有 go 和 ts，没有 "rust"`},
	} {
		t.Setenv("PERSONA_LANG", tt.lang)
		if err := newExtension("ok", tt.template, "x", filepath.Join(t.TempDir(), "x"), &out); err == nil || err.Error() != tt.want {
			t.Errorf("%s: %v, want %q", tt.lang, err, tt.want)
		}
	}
}

// A new extension's next steps, in the terminal's language, with the commands as they are typed.
func TestNewPrintsNextStepsInTheTerminalLanguage(t *testing.T) {
	t.Setenv("PERSONA_LANG", "en")
	dir := filepath.Join(t.TempDir(), "demo")
	var out bytes.Buffer
	if err := newExtension("demo-steps", "go", "Demo", dir, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Wrote the extension demo-steps in " + dir + " (go template). Next:",
		"  go test ./...                 unit tests and conformance checks\n",
		"personad extension dev", "try it on this machine’s hub", "personad extension pack"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in:\n%s", want, out.String())
		}
	}
	if _, err := openProject(filepath.Join(t.TempDir(), "nothing")); err == nil || !strings.HasPrefix(err.Error(), "There is no persona-extension.json in ") {
		t.Errorf("no manifest: %v", err)
	}
}

// An extension of an extension repository (extensions/<id>/, one go.mod at the repository's root) is built as Go; a
// TypeScript one there is still TypeScript, and the search for a go.mod stops at the repository.
func TestOpenProjectInARepository(t *testing.T) {
	repo := t.TempDir()
	write := func(path, s string) {
		p := filepath.Join(repo, path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := func(id string) string {
		return `{"manifest_version": 1, "id": "` + id + `", "name": "` + id + `", "version": "1.0.0", "engines": {"extension_protocol": 1},
 "runtime": {"command": "bin/` + id + `"}, "contributes": {"ui": [{"id": "` + id + `", "title": "x", "placement": "nav_page"}]}}`
	}
	write("go.mod", "module example.com/exts\n")
	write("extensions/gox/persona-extension.json", manifest("gox"))
	write("extensions/tsx/persona-extension.json", manifest("tsx"))
	write("extensions/tsx/src/main.ts", "")
	for id, kind := range map[string]string{"gox": "go", "tsx": "ts"} {
		p, err := openProject(filepath.Join(repo, "extensions", id))
		if err != nil || p.kind != kind {
			t.Fatalf("%s: %+v %v", id, p, err)
		}
	}
	write("extensions/decl/persona-extension.json", `{"manifest_version": 1, "id": "decl", "name": "Decl", "version": "1.0.0",
 "contributes": {"providers": [{"id": "decl", "name": "Decl", "base_url": "https://example.com"}]}}`)
	if p, err := openProject(filepath.Join(repo, "extensions", "decl")); err != nil || p.kind != "" {
		t.Fatalf("a declarative extension in a Go repository: %+v %v", p, err)
	}
	// a repository of its own (.git) without a go.mod: not Go, even if a directory above has one
	write("inner/.git/HEAD", "ref: refs/heads/main\n")
	write("inner/extensions/x/persona-extension.json", manifest("x"))
	if _, err := openProject(filepath.Join(repo, "inner", "extensions", "x")); err == nil {
		t.Fatal("a go.mod above the repository is not the extension's")
	}
}
