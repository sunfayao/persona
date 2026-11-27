package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/update"
	"github.com/sunfayao/persona/server/internal/update/updatetest"
)

// A newer release published between the hub's check and its update (#9263): the update installs the release it
// checked, from that release's own tag on GitHub, or says a newer one is out and offers that one from a source that is
// not GitHub's; never "the download does not match the release's manifest" for a download that was fine.

// pinNodePlatform is a platform the releases here publish a node build for besides the hub's own.
const pinNodePlatform = "plan9-arm"

// pinScript is release version's personad (and persona-hub) for this platform: a script answering version -v as the
// release's binary does, with pad in it, so two releases' downloads differ in size as well as content.
func pinScript(version string, build int64, pad string) []byte {
	return []byte(fmt.Sprintf("#!/bin/sh\n%secho %s\necho 'channel release, build %d'\n", pad, version, build))
}

// pinRelease is release version as a source serves it: personad and persona-hub for this platform, a node build for
// pinNodePlatform, and latest.json.
func pinRelease(t *testing.T, version string, build int64, pad string) map[string][]byte {
	t.Helper()
	own := runtime.GOOS + "-" + runtime.GOARCH
	files := map[string][]byte{}
	file := func(name string, body []byte) update.File {
		packed := gz(body)
		outer, inner := sha256.Sum256(packed), sha256.Sum256(body)
		files[name] = packed
		return update.File{Name: name, Size: int64(len(packed)), SHA256: hex.EncodeToString(outer[:]), BinarySHA256: hex.EncodeToString(inner[:])}
	}
	script := pinScript(version, build, pad)
	m := update.Manifest{Version: version, Build: build, Protocol: proto.Protocol, Released: time.Now().UTC().Truncate(time.Second),
		Files: map[string]update.File{
			own:             file("personad-"+own+".gz", script),
			pinNodePlatform: file("personad-"+pinNodePlatform+".gz", []byte("node build of "+version+"\n"+pad)),
		},
		Programs: map[string]map[string]update.File{"persona-hub": {own: file("persona-hub-"+own+".gz", script)}}}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	files["latest.json"] = data
	return files
}

// releasesHub is updatingHub with base, a source of src, as hub.json's update.url, and node updates on.
func releasesHub(t *testing.T, src *updatetest.Releases, base string) (*Hub, string, func() []string) {
	t.Helper()
	h, _, target, execs := updatingHub(t, newPublisher(t, "unused", 202611021500), "release")
	h.cfg.Update.URL = base
	h.cfg.AutoUpdateNodes = true
	h.upd.client = src.Client()
	return h, target, execs
}

// A download that is broken while the source still has the release checked fails with the mismatch, as before, and
// the hub keeps offering that release.
func TestUpdateBrokenDownloadStillMismatches(t *testing.T) {
	src := updatetest.NewReleases(t)
	files := pinRelease(t, "0.6.1", 202611160928, "")
	own := "personad-" + runtime.GOOS + "-" + runtime.GOARCH + ".gz"
	files[own] = append(bytes.Clone(files[own]), "garbage"...)
	src.Publish("v0.6.1", files, true)
	h, target, execs := releasesHub(t, src, src.DL())
	ctx := context.Background()
	if _, err := h.upd.check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.upd.begin(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the failure", func() bool { return h.upd.view().State == "failed" })
	if b, _ := os.ReadFile(target); string(b) != "#!/bin/sh\necho old\n" || len(execs()) != 0 {
		t.Fatal("replaced the binary")
	}
	if v := h.upd.view(); !strings.Contains(v.Error, "does not match the release's manifest") || v.Latest == nil || v.Latest.Version != "0.6.1" {
		t.Fatalf("error %q, offers %+v", v.Error, v.Latest)
	}
	if got := src.Fetched(own); len(got) != 1 {
		t.Fatalf("fetched %v", got)
	}
}
