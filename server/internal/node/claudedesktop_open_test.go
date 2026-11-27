//go:build !windows

package node

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

const desktopLocal = "eeeeeeee-0000-4000-8000-000000000001" // the app's own id of sidA's session: local_<this>

// recordDesktopLinks replaces the opener: each link it is asked to open is sent on the channel returned, then `then`
// runs (the app starting a claude, say) and fail is returned.
func recordDesktopLinks(t *testing.T, fail error, then func()) <-chan string {
	t.Helper()
	links := make(chan string, 4)
	old := openDesktopLink
	openDesktopLink = func(_ context.Context, link string) error {
		links <- link
		if then != nil {
			go then()
		}
		return fail
	}
	t.Cleanup(func() { openDesktopLink = old })
	return links
}

// sendWithin sends text to id on another goroutine; the result comes on the channel.
func sendWithin(ctx context.Context, n *Node, id, text string) <-chan error {
	done := make(chan error, 1)
	b, _ := json.Marshal(map[string]any{"session_id": id, "text": text, "client_msg_id": newUUID()})
	go func() {
		_, err := n.handle(ctx, "session.send", b)
		done <- err
	}()
	return done
}

func waitSend(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("the send did not end")
		return nil
	}
}

// Another claude has the session open (a terminal's without an inbox, a program's): refused as before, the app not
// asked to start a second one.
func TestADesktopSessionAnotherClaudeHasIsNotOpened(t *testing.T) {
	for _, entrypoint := range []string{"cli", "sdk-ts"} {
		t.Run(entrypoint, func(t *testing.T) {
			n, f, home := herdrNode(t)
			noPaneEnv(t)
			first, _ := useDesktopDirs(t)
			bgTranscript(t, home, home, sidA, "plan")
			writeDesktopEntry(t, first, desktopLocal, sidA, false, nil, "")
			writeOutsideClaude(t, home, sidA, entrypoint, "idle", nil, nil)
			links := recordDesktopLinks(t, nil, nil)
			f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
			err := waitSend(t, sendWithin(context.Background(), n, sidA, "go on"))
			if refusalCode(err) != openElsewhere {
				t.Fatalf("send: %v", err)
			}
			if len(links) != 0 {
				t.Fatalf("the app was asked to open it: %s", <-links)
			}
		})
	}
}

// Where the app does not open its links in the background (Windows: claudeDesktopOpens false), its index is read for
// the list alone: a send to a session of its sidebar opens nothing and is handled as before the index was read (a
// stopped agent's refusal), and the app's launcher is not offered.
func TestWithoutABackgroundOpenTheIndexIsForTheListAlone(t *testing.T) {
	n, f, home := herdrNode(t)
	noPaneEnv(t)
	first, _ := useDesktopDirs(t)
	claudeDesktopOpens = false
	fakeClaudeOnPath(t)
	shellLaunchers(t)
	bgTranscript(t, home, home, sidA, "plan")
	writeDesktopEntry(t, first, desktopLocal, sidA, false, nil, "")
	if readClaudeDesktopIndex(home).about[sidA].local == "" {
		t.Fatal("the index was not read")
	}
	links := recordDesktopLinks(t, nil, nil)
	f.reply("session.snapshot", snapshot(nil, nil, nil, nil))
	f.reset()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := waitSend(t, sendWithin(ctx, n, sidA, "go on"))
	if len(links) != 0 {
		t.Fatalf("%d links opened", len(links))
	}
	if code, _ := refusalOf(t, err); code != agentStopped {
		t.Fatalf("send: %v", err)
	}
	if _, added := n.withDesktopLauncher(nil); added {
		t.Fatal("the app's launcher offered")
	}
}

// The index keeps an unarchived session's local id, only in the app's own form (it goes into a link).
func TestTheIndexKeepsTheAppsOwnID(t *testing.T) {
	first, third := useDesktopDirs(t)
	home := t.TempDir()
	writeDesktopEntry(t, first, desktopLocal, sidA, false, nil, "")
	writeDesktopEntry(t, third, "ffffffff-0000-4000-8000-000000000002", sidB, false, nil, `,"sessionId":"local_x y"`)
	x := readClaudeDesktopIndex(home)
	if got := x.about[sidA].local; got != "local_"+desktopLocal {
		t.Errorf("local id %q", got)
	}
	if got := x.about[sidB].local; got != "" {
		t.Errorf("a malformed id kept: %q", got)
	}
}
