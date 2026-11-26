package extmgr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A declarative kit: its quick prompt is listed for the apps, its skill and instructions go
// to the assistant; switched off one by one, each leaves.
func TestDeclarativeKit(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	dir := declarativePackage(t, `{"manifest_version": 1, "id": "kit", "name": "Kit", "version": "1.0.0", "contributes": {
		"skills": [{"id": "review", "dir": "skills/review"}],
		"prompts": [{"id": "tone", "kind": "instructions", "file": "assistant/tone.md"},
			{"id": "today", "kind": "quick", "title": "今天", "text": "今天做了什么？"}]}}`)
	_ = os.MkdirAll(filepath.Join(dir, "skills", "review"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "skills", "review", "SKILL.md"), []byte("# review\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "assistant"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "assistant", "tone.md"), []byte("Answer in short sentences.\n"), 0o644)
	installForTest(t, h, dir)

	_, out := call(t, mux, "GET", "/api/v1/extensions/-/prompts", master, "")
	if ps := out["prompts"].([]any); len(ps) != 1 || ps[0].(map[string]any)["title"] != "今天" || ps[0].(map[string]any)["extension_name"] != "Kit" {
		t.Fatalf("%v", out)
	}
	skills := h.extensions.extensionSkills()
	if len(skills) != 1 || skills[0].Name != "review" || string(skills[0].Files["SKILL.md"]) != "# review\n" {
		t.Fatalf("%+v", skills)
	}
	if got := h.extensions.extensionInstructions(context.Background(), ""); !strings.Contains(got, "Answer in short sentences.") {
		t.Fatal(got)
	}

	for _, c := range []string{"skills/review", "prompts/tone", "prompts/today"} {
		if code, out := call(t, mux, "PUT", "/api/v1/extensions/kit/-/contributions/"+c, master, `{"enabled": false}`); code != 200 {
			t.Fatalf("%s: %d %v", c, code, out)
		}
	}
	if len(h.extensions.extensionSkills()) != 0 || strings.Contains(h.extensions.extensionInstructions(context.Background(), ""), "short sentences") {
		t.Fatal("switched off: not the assistant's any more")
	}
	if _, out := call(t, mux, "GET", "/api/v1/extensions/-/prompts", master, ""); len(out["prompts"].([]any)) != 0 {
		t.Fatal(out)
	}

}
