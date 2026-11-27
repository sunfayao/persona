package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	repository "github.com/sunfayao/persona/server/internal/repositorywire"
	"github.com/sunfayao/persona/server/internal/taskworkspace"
)

func TestBackendWorkspaceMissingReceiptWithPhysicalRemains(t *testing.T) {
	for _, existing := range []string{"checkout", "private Git", "unexpected control file", "nonempty hooks"} {
		t.Run(existing, func(t *testing.T) {
			home := workspaceTemp(t)
			n := &Node{home: home, dir: filepath.Join(home, ".persona")}
			req := taskworkspace.Request{WorkspaceID: "ws_ambiguous", OperationID: "op_ambiguous", Plan: workspacePlan(t, home, repository.LayoutRoot, workspaceRemote(t, "main"))}
			storage := filepath.Join(n.dir, "task-workspaces", taskworkspace.Hash(req.WorkspaceID))
			path := map[string]string{
				"checkout": req.Plan.RootPath, "private Git": filepath.Join(storage, "git"),
				"unexpected control file": storage, "nonempty hooks": filepath.Join(storage, "hooks"),
			}[existing]
			if err := os.MkdirAll(path, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(path, "existing-data")
			if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{"cancel", "cleanup"} {
				r := workspaceCall(t, n, method, req)
				if r.Error == nil || r.Error.Code != "operation_unknown" || r.State == "removed" || r.State == "cancelled" {
					t.Fatal(method, r)
				}
			}
			if _, err := os.Lstat(filepath.Join(storage, "receipt.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invented ownership", err)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
				t.Fatal("existing resource changed", string(data), err)
			}
		})
	}
}
