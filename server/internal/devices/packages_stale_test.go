package devices

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/nodeproto"
	"github.com/sunfayao/persona/server/internal/update/updatetest"
)

// An unchanged file outside the recent-replacement window still reuses its scan instead of being read in full again.
func TestUnchangedLocalBuildReusesAStableVersionScan(t *testing.T) {
	const goos, arch = "windows", "amd64"
	const hub = "0.9.2-202611221500"
	current := updatetest.VersionedProgram(t, nodeproto.ProgramPersonad, goos, arch, hub)
	path := filepath.Join(t.TempDir(), "personad-windows-amd64.exe")
	copyFile(t, current, path)
	stamp := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	p := &nodePackages{}
	if carries, err := p.carriesVersion(path, hub); err != nil || !carries {
		t.Fatalf("first scan: %v %v", carries, err)
	}
	// Poison the remembered answer: a second call that really uses the cache returns it; a full rescan finds hub and
	// returns true instead. This distinguishes reuse from merely getting the same answer twice.
	p.mu.Lock()
	scan := p.scans[path]
	scan.carries = false
	p.scans[path] = scan
	p.mu.Unlock()
	if carries, err := p.carriesVersion(path, hub); err != nil || carries {
		t.Fatalf("an unchanged stable file was scanned again: %v %v", carries, err)
	}
}

// A timestamp carried from a machine whose clock is ahead must not disable caching forever (#13326 review).
func TestFutureLocalBuildStillUsesCachedPackage(t *testing.T) {
	const goos, arch = "windows", "amd64"
	const hub = "0.9.2-202611221500"
	current := updatetest.VersionedProgram(t, nodeproto.ProgramPersonad, goos, arch, hub)
	d, dir, _ := packageDevices(t, hub, "")
	local := filepath.Join(dir, "personad-windows-amd64.exe")
	copyFile(t, current, local)
	stamp := time.Now().Add(time.Hour)
	if err := os.Chtimes(local, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if pkg, err := d.nodePackageFor(context.Background(), goos, arch, nodeproto.ProgramPersonad); err != nil || pkg.path != local {
		t.Fatalf("first future-stamp lookup: %+v %v", pkg, err)
	}
	r, ok := d.cachedPackage(d.packageKey(goos, arch, nodeproto.ProgramPersonad))
	if !ok || r.err != nil || r.pkg.path != local {
		t.Fatalf("an unchanged future-stamp build missed the result cache: %+v %v, ok=%v", r.pkg, r.err, ok)
	}
}

func TestFileContainsAcrossChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "build")
	needle := "0.9.2-202611221500"
	body := make([]byte, 3<<20)
	at := 1<<20 - 5 // straddles the first chunk's end
	copy(body[at:], needle)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := fileContains(path, []byte(needle)); err != nil || !ok {
		t.Fatalf("across a chunk boundary: %v %v", ok, err)
	}
	if ok, err := fileContains(path, []byte("0.9.1-202611190505")); err != nil || ok {
		t.Fatalf("absent: %v %v", ok, err)
	}
}
