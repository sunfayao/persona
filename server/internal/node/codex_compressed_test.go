package node

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestCodexCompressedRolloutIsVisibleThroughTheSessionReader(t *testing.T) {
	n := remoteTestNode(t)
	dir := filepath.Join(n.codexHome(), "archived_sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-01-01T00-00-00-"+threadMain+".jsonl.zst")
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	contents := fmt.Sprintf(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":%q,"cwd":"/work/example"}}
{"timestamp":"2026-01-01T00:00:01Z","type":"event_msg","payload":{"type":"item_completed","item":{"id":"m1","type":"AgentMessage","content":[{"type":"text","text":"The compressed history is readable."}]}}}
`, threadMain)
	if err := os.WriteFile(path, enc.EncodeAll([]byte(contents), nil), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := n.file(threadMain)
	if err != nil {
		t.Fatal(err)
	}
	items, err := f.Refresh()
	if err != nil || len(items) != 1 || items[0].Text != "The compressed history is readable." {
		t.Fatalf("archive items = %+v, %v", items, err)
	}
	if f.Changed() {
		t.Fatal("an unchanged compressed archive was treated as a new history")
	}
	if f.Meta().Cwd != "/work/example" {
		t.Fatalf("archive metadata = %+v", f.Meta())
	}
}
