package devices

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/update"
	"github.com/sunfayao/persona/server/internal/update/updatetest"
)

// releaseFiles is release m as a source with no dl/<version>/ serves it: personad-<plat>.gz holding build, and
// latest.json.
func releaseFiles(t *testing.T, m update.Manifest, plat string, build []byte) map[string][]byte {
	t.Helper()
	var packed bytes.Buffer
	zw := gzip.NewWriter(&packed)
	_, _ = zw.Write(build)
	_ = zw.Close()
	outer, inner := sha256.Sum256(packed.Bytes()), sha256.Sum256(build)
	name := "personad-" + plat + ".gz"
	m.Files = map[string]update.File{plat: {Name: name, Size: int64(packed.Len()), SHA256: hex.EncodeToString(outer[:]), BinarySHA256: hex.EncodeToString(inner[:])}}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{"latest.json": data, name: packed.Bytes()}
}

// GitHub's moving release base must not be asked for the legacy <version>/<file> path first: its redirect turns
// that into releases/download/v<version>/<version>/<file>, which is not a release asset.
func TestNodePackageGitHubDownloadUsesReleaseTag(t *testing.T) {
	plat := otherOS + "-" + otherArch
	name := "personad-" + plat + ".gz"
	full := updatetest.Program(t, "personad", otherOS, otherArch)
	build, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	src := updatetest.NewReleases(t)
	src.Publish("v0.9.1", releaseFiles(t, overlapping("0.9.1", 202611190000), plat, build), true)
	d, _, _ := packageDevices(t, "0.9.1", src.GitHub())
	d.packages.httpClient = func(*url.URL, time.Duration) *http.Client { return src.Client() }
	pkg, err := d.nodePackageFor(context.Background(), otherOS, otherArch, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.fetchPackage(context.Background(), pkg); err != nil {
		t.Fatal(err)
	}
	want := updatetest.TagPath("v0.9.1", name)
	if got := src.Fetched(name); len(got) != 1 || got[0] != want {
		t.Fatalf("fetched %v, want only %q", got, want)
	}
}
