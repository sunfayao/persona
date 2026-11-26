package files

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestReadFileStreamDescribesBinaryFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	n := New(testConfig(dir))
	video := bytes.Repeat([]byte{0, 1, 2, 3}, maxFileBytes) // bigger than a page
	os.WriteFile(filepath.Join(dir, "clip.mp4"), video, 0600)
	os.WriteFile(filepath.Join(dir, "blob"), []byte{0, 1, 2, 0xff}, 0600)
	os.WriteFile(filepath.Join(dir, "a.csv"), []byte("a,b\n1,2\n"), 0600)
	fc, err := n.Read("~/clip.mp4", "", 0, true)
	if err != nil || !fc.Stream || fc.Data != "" || fc.Mime != "video/mp4" || fc.Size != int64(len(video)) || fc.Revision == "" || fc.Truncated {
		t.Fatalf("video = %+v, %v", fc, err)
	}
	chunk, err := n.ReadChunk(proto.FileChunkRequest{Path: "~/clip.mp4"})
	if err != nil || chunk.Revision != fc.Revision {
		t.Fatalf("download revision %+v differs from %q", chunk, fc.Revision)
	}
	fc, err = n.Read("~/blob", "", 0, true) // not known by name: read, found binary, still described only
	if err != nil || !fc.Stream || fc.Data != "" || fc.Mime != "application/octet-stream" {
		t.Fatalf("blob = %+v, %v", fc, err)
	}
	fc, err = n.Read("~/a.csv", "", 0, true) // text stays text
	if err != nil || fc.Stream || fc.Text != "a,b\n1,2\n" || fc.Mime != "text/csv" {
		t.Fatalf("csv = %+v, %v", fc, err)
	}
	fc, err = n.Read("~/clip.mp4", "", 0, false) // older apps: base64 as before
	if err != nil || fc.Stream || fc.Data == "" || !fc.Truncated {
		t.Fatalf("legacy = stream %v data %d truncated %v, %v", fc.Stream, len(fc.Data), fc.Truncated, err)
	}
}

func TestDownloadSniffsWhatTheNameDoesNotSay(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	n := New(testConfig(dir))
	os.WriteFile(filepath.Join(dir, "scan"), []byte("%PDF-1.4\n%rest"), 0600)
	info, err := n.ReadChunk(proto.FileChunkRequest{Path: "~/scan"})
	if err != nil || info.Mime != "application/pdf" {
		t.Fatalf("%+v, %v", info, err)
	}
}

// A .ts file is TypeScript or an MPEG transport stream, and only its bytes tell (#7974): every way a client gets at a
// file (the viewer's file.read with and without stream, the download's type, the Info sheet) says the same.
func TestTypeScriptAndTransportStreamByTheirBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	n := New(testConfig(dir))
	fixtures := filepath.Join("..", "..", "..", "..", "spec", "fixtures", "file-types")
	script, err := os.ReadFile(filepath.Join(fixtures, "typescript.ts"))
	if err != nil {
		t.Fatal(err)
	}
	segment, err := os.ReadFile(filepath.Join(fixtures, "segment.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"app.ts": script, "esm.mts": script, "cjs.cts": script, "clip.ts": segment,
		"camera.mts": segment, "empty.ts": nil} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"app.ts", "esm.mts", "cjs.cts", "empty.ts"} {
		for _, stream := range []bool{true, false} {
			fc, err := n.Read("~/"+name, "", 0, stream)
			if err != nil || fc.Stream || fc.Text != string(script[:len(fc.Text)]) || fc.Mime != proto.MimeTypeScript {
				t.Fatalf("%s (stream %v) = %+v, %v", name, stream, fc, err)
			}
		}
	}
	for _, name := range []string{"clip.ts", "camera.mts"} {
		fc, err := n.Read("~/"+name, "", 0, true)
		if err != nil || !fc.Stream || fc.Text != "" || fc.Data != "" || fc.Mime != proto.MimeTransportStream || fc.Revision == "" {
			t.Fatalf("%s = %+v, %v", name, fc, err)
		}
	}
	for name, want := range map[string]string{"app.ts": proto.MimeTypeScript, "clip.ts": proto.MimeTransportStream} {
		chunk, err := n.ReadChunk(proto.FileChunkRequest{Path: "~/" + name})
		if err != nil || chunk.Mime != want {
			t.Fatalf("download %s = %+v, %v", name, chunk, err)
		}
		if info := n.statDetail("~/"+name, ""); info.Kind != "file" || info.Mime != want {
			t.Fatalf("Info %s = %+v", name, info)
		}
	}
	if info := n.statDetail("~", ""); info.Kind != "dir" || info.Mime != "" {
		t.Fatalf("a folder has no type: %+v", info)
	}
}
