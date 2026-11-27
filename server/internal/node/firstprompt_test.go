package node

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// promptLands makes the transcript of the session claude was started with (--session-id) get the user's message
// after the try-th agent.prompt (1 when omitted), as Claude writes it when it takes a message; earlier ones are dropped.
func promptLands(t *testing.T, f *fakeHerdr, home, cwd string, try ...int) {
	t.Helper()
	at := 1
	if len(try) > 0 {
		at = try[0]
	}
	var mu sync.Mutex
	prompts, id := 0, ""
	f.after(func(c herdrCall) {
		mu.Lock()
		defer mu.Unlock()
		switch c.Method {
		case "pane.send_input":
			if m := sessionIDArg.FindStringSubmatch(str(c.Params["text"])); m != nil {
				id = m[1]
			}
		case "agent.prompt":
			if prompts++; prompts == at {
				writeUserLine(t, home, cwd, id, str(c.Params["text"]))
			}
		}
	})
}

var sessionIDArg = regexp.MustCompile(`--session-id (\S+)`)

// startedID is the session id the start typed (--session-id).
func startedID(f *fakeHerdr) string {
	for _, c := range f.calls() {
		if m := sessionIDArg.FindStringSubmatch(str(c.Params["text"])); c.Method == "pane.send_input" && m != nil {
			return m[1]
		}
	}
	return ""
}

func writeUserLine(t *testing.T, home, cwd, id, text string) {
	writeTranscript(t, home, cwd, id, time.Now(),
		`{"type":"user","timestamp":"`+time.Now().UTC().Format(time.RFC3339Nano)+`","cwd":"`+cwd+`","message":{"role":"user","content":"`+text+`"}}`)
}

// startPane makes herdr open pane w1:p2 for a start into workspace w1, with a claude that is idle there.
func startPane(t *testing.T, f *fakeHerdr, plain, ansi string) {
	f.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
	startHerdr(f, "w1:p2", proto.StatusIdle)
	f.setScreens("w1:p2", plain, ansi)
}

// A first message that stays in the box (typed, not submitted) gets one Enter, and is not typed a second time.
func TestStartEntersAFirstMessageLeftInTheBox(t *testing.T) {
	withSubmitGrace(t, 100*time.Millisecond)
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	startPane(t, f, screenFixture(t, "claude-2.1.312-empty-162.txt"), screenFixture(t, "claude-2.1.312-empty-162.ansi"))
	typedTxt, typedANSI := screenFixture(t, "claude-2.1.312-typed-162.txt"), screenFixture(t, "claude-2.1.312-typed-162.ansi")
	f.after(func(c herdrCall) {
		switch {
		case c.Method == "agent.prompt": // the box keeps showing it ("fix the flaky test")
			f.setScreens("w1:p2", typedTxt, typedANSI)
		case isKeys("enter")(c):
			writeUserLine(t, home, cwd, startedID(f), "fix the flaky test")
		}
	})
	if _, err := request(t, n, "session.start", map[string]any{"cwd": cwd, "workspace_id": "w1", "prompt": "fix the flaky test"}); err != nil {
		t.Fatal(err)
	}
	n.firstWait.Wait()
	assertTrace(t, f.trace("agent.prompt", "pane.send_keys"), []string{
		call("agent.prompt", map[string]any{"target": "w1:p2", "text": "fix the flaky test"}),
		sendKeys("w1:p2", "enter"),
	})
	if _, ok := n.lostFirst.get(startedID(f)); ok {
		t.Error("marked as lost")
	}
}

func TestFirstWatchAttachesLateWithoutCountingOldImagePrompt(t *testing.T) {
	n, _, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	text := "fix the flaky test"
	watch := &promptWatch{want: echoWords(text), after: time.Now()}
	if n.firstWatchTaken(startID, watch) {
		t.Fatal("a nonexistent transcript confirmed delivery")
	}
	var old map[string]any
	if err := json.Unmarshal([]byte(promptLine(cwd, text, 2<<20)), &old); err != nil {
		t.Fatal(err)
	}
	old["timestamp"] = watch.after.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	line, _ := json.Marshal(old)
	appendTranscript(t, home, cwd, startID, string(line))
	if n.firstWatchTaken(startID, watch) {
		t.Fatal("an older matching image prompt confirmed this send")
	}
	appendTranscript(t, home, cwd, startID, promptLine(cwd, text, 2<<20))
	if !n.firstWatchTaken(startID, watch) {
		t.Fatal("a full image-bearing prompt was missed on late attach")
	}
}

func TestFirstDraftPinsResumedPasteIDs(t *testing.T) {
	prompt := "A resumed first prompt\nwith multiple\nlines"
	draft := &firstDraft{} // a resumed session already used earlier chip IDs
	if !draft.matches("[Image #5][Pasted text #6 +26 lines]", prompt) {
		t.Fatal("did not recognize the resumed paste")
	}
	for _, other := range []string{
		"[Image #5][Pasted text #7 +26 lines]",
		"[Image #5][Pasted text #6 +26 lines] unrelated words",
		"A resumed first prompt with unrelated changes",
	} {
		if draft.matches(other, prompt) {
			t.Fatalf("matched changed resumed draft %q", other)
		}
	}
}

// A first message with a control character goes in inert, and the box showing it so is that message left there: it gets
// its Enter.
func TestStartEntersAnInertFirstMessageLeftInTheBox(t *testing.T) {
	withSubmitGrace(t, 100*time.Millisecond)
	n, f, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	startPane(t, f, screenFixture(t, "claude-2.1.312-empty-162.txt"), screenFixture(t, "claude-2.1.312-empty-162.ansi"))
	inert := func(s string) string {
		return strings.Replace(s, "fix the flaky test", "<U+0007>fix the flaky test", 1)
	}
	typedTxt, typedANSI := inert(screenFixture(t, "claude-2.1.312-typed-162.txt")), inert(screenFixture(t, "claude-2.1.312-typed-162.ansi"))
	f.after(func(c herdrCall) {
		switch {
		case c.Method == "agent.prompt":
			f.setScreens("w1:p2", typedTxt, typedANSI)
		case isKeys("enter")(c):
			writeUserLine(t, home, cwd, startedID(f), "<U+0007>fix the flaky test")
		}
	})
	if _, err := request(t, n, "session.start", map[string]any{"cwd": cwd, "workspace_id": "w1", "prompt": "\afix the flaky test"}); err != nil {
		t.Fatal(err)
	}
	n.firstWait.Wait()
	assertTrace(t, f.trace("agent.prompt", "pane.send_keys"), []string{
		call("agent.prompt", map[string]any{"target": "w1:p2", "text": "<U+0007>fix the flaky test"}),
		sendKeys("w1:p2", "enter"),
	})
}

// A start carries image bytes to the target machine, not a path on the sender's. The node saves those exact bytes and
// gives Claude the same attachment and path lines as an ordinary image message (#11084).
func TestStartDecodesImageAttachments(t *testing.T) {
	n, f, home := herdrNode(t)
	withSubmitGrace(t, 60*time.Millisecond)
	withFirstPromptWaits(t, 500*time.Millisecond, 3*time.Second)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	empty, emptyANSI := screenFixture(t, "claude-2.1.312-empty-162.txt"), screenFixture(t, "claude-2.1.312-empty-162.ansi")
	startPane(t, f, empty, emptyANSI)
	raw := []byte("synthetic image bytes")
	encoded := base64.StdEncoding.EncodeToString(raw)
	var sent string
	f.after(func(c herdrCall) {
		if c.Method != "agent.prompt" {
			return
		}
		sent = str(c.Params["text"])
		line, err := json.Marshal(map[string]any{
			"type": "user", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "cwd": cwd,
			"message": map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": encoded}},
				map[string]any{"type": "text", "text": sent},
			}},
		})
		if err != nil {
			t.Error(err)
			return
		}
		appendTranscript(t, home, cwd, startedID(f), string(line))
	})
	if _, err := request(t, n, "session.start", map[string]any{
		"cwd": cwd, "workspace_id": "w1", "prompt": "Inspect this screenshot",
		"images": []map[string]any{{"mime": "image/png", "data": encoded}},
	}); err != nil {
		t.Fatal(err)
	}
	n.firstWait.Wait()
	if !strings.Contains(sent, "Inspect this screenshot\n\n<attached_images>\n") {
		t.Fatalf("start prompt has no attachment block: %q", sent)
	}
	rest := strings.SplitN(sent, "<attached_images>\n", 2)[1]
	path := strings.SplitN(rest, "\n", 2)[0]
	if !filepath.IsAbs(path) {
		t.Fatalf("attachment is not a target-side absolute path: %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("saved image = %q, %v; want the transmitted bytes", got, err)
	}
	if !strings.Contains(sent, "path:"+path) {
		t.Fatalf("Claude's prompt lacks the image's path line: %q", sent)
	}
}

// The transcript getting a message typed at the computer clears it too.
func TestALostFirstMessageClearsOnceTheTranscriptHasOne(t *testing.T) {
	n, _, home := herdrNode(t)
	cwd := mkdir(t, filepath.Join(home, "proj"))
	n.lostFirst.set(startID, lostDialog, time.Now())
	if why := n.lostFirstPrompt(startID, proto.StatusIdle); why != lostDialog {
		t.Fatalf("why %q", why)
	}
	writeUserLine(t, home, cwd, startID, "typed at the computer")
	if why := n.lostFirstPrompt(startID, proto.StatusIdle); why != "" {
		t.Errorf("still lost: %q", why)
	}
	n.lostFirst.set(startID, lostDialog, time.Now().Add(time.Minute))
	if why := n.lostFirstPrompt(startID, proto.StatusWorking); why != "" {
		t.Errorf("a working agent still lost: %q", why)
	}
}
