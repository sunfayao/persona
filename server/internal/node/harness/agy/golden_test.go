package agy

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/turnmodel"
)

var update = flag.Bool("update", false, "regenerate Antigravity projections")

func TestGoldenConversations(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("testdata/agy-*.jsonl")
	if err != nil || len(files) == 0 {
		t.Fatalf("find corpus: %v (%v)", err, files)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			clock := testClock{}
			ref := Ref{t.TempDir(), agyTestID}
			path := agyTestFile(t, ref.Root, ref.Conversation, string(raw))
			if err := os.Chtimes(path, clock.Now(), clock.Now()); err != nil {
				t.Fatal(err)
			}
			h := readFixture(t, New(Dependencies{Clock: clock}), ref)
			row := proto.Session{SessionID: ref.Conversation, Agent: "agy"}
			h.Apply(&row, true)
			for _, item := range h.items {
				if item.Kind == proto.KindToolUse && (item.Facets == nil || item.Facets.Tool == nil || item.Facets.Tool.Kind != turnmodel.KindOf(item.ToolName)) {
					t.Fatalf("tool %s has no canonical kind", item.ID)
				}
				if strings.Contains(item.Text, "Created At:") || strings.Contains(item.Text, "<truncated") {
					t.Fatalf("item %s leaked an export header or marker", item.ID)
				}
			}
			snapshot := struct {
				History string        `json:"history"`
				ToSeq   int           `json:"to_seq"`
				Items   []proto.Item  `json:"items"`
				Row     proto.Session `json:"row"`
			}{h.token, h.seq, h.items, row}
			got, err := json.MarshalIndent(snapshot, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			wantPath := strings.TrimSuffix(file, ".jsonl") + ".golden.json"
			if *update || os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(wantPath, got, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(wantPath)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("%s differs; regenerate with go test ./internal/node/harness/agy -run Golden -update: %v", wantPath, err)
			}
		})
	}
}
