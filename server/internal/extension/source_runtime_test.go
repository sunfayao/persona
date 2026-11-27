package extension

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/extension/sourcevalidate"
)

// CheckSelfContained is CheckSelfContainedContext without a deadline of the caller's.
func CheckSelfContained(b []byte) error {
	return CheckSelfContainedContext(context.Background(), b)
}

func TestSourceBundleAllowsHarmlessImportWords(t *testing.T) {
	for _, source := range []string{`export default {text:"Data from a service",imported:true}`, "// import('./x')\nexport default 'import and from'", "export default `text from a service`", `const pattern=/import|from|"/; export default pattern`, `const from=1;export default from`} {
		if err := CheckSelfContained([]byte(source)); err != nil {
			if errors.Is(err, sourcevalidate.ErrUnavailable) {
				t.Skip("UNAVAILABLE: independent production validator resources; module-policy process fixtures are separate")
			}
			t.Fatalf("harmless module rejected: %s: %v", source, err)
		}
	}
	if err := CheckSelfContained([]byte(`export default (`)); err == nil {
		t.Fatal("invalid module syntax accepted")
	}
}

func TestSourceRuntimeManifestMode(t *testing.T) {
	r := &Runtime{Mode: RuntimeDeno, Source: &SourceRuntime{Entry: "src/main.ts", Bundle: "bundle/main.mjs", SHA256: strings.Repeat("0", 64)}}
	if errs := r.validate(); len(errs) != 0 {
		t.Fatal(errs)
	}
	for _, mutate := range []func(*Runtime){func(r *Runtime) { r.Command.Path = "bin/x" }, func(r *Runtime) { r.Env = map[string]string{"X": "y"} }, func(r *Runtime) { r.RPC = "none" }, func(r *Runtime) { r.HTTP = true }, func(r *Runtime) { r.Mode = "process" }, func(r *Runtime) { r.Source = nil }} {
		copy := *r
		mutate(&copy)
		if len(copy.validate()) == 0 {
			t.Fatal("accepted incompatible source runtime")
		}
	}
}
