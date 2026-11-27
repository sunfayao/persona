package extension

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/i18n"
)

// halfThenWait serves data's first half, then the rest once release is closed; stopped says a request ended
// before that (its client went away).
type halfThenWait struct {
	srv      *httptest.Server
	requests atomic.Int32
	stopped  atomic.Int32
	release  chan struct{}
}

func newHalfThenWait(t *testing.T, data []byte) *halfThenWait {
	h := &halfThenWait{release: make(chan struct{})}
	h.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.requests.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data[:len(data)/2])
		w.(http.Flusher).Flush()
		select {
		case <-h.release:
			_, _ = w.Write(data[len(data)/2:])
		case <-r.Context().Done():
			h.stopped.Add(1)
		}
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *halfThenWait) market(t *testing.T, cache string) *Market {
	t.Helper()
	spec, err := ParseMarket(h.srv.URL + "/index.json")
	if err != nil {
		t.Fatal(err)
	}
	return &Market{Spec: spec, Client: h.srv.Client(), Cache: cache}
}

func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !ok(); {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		<-time.After(5 * time.Millisecond)
	}
}

// downloadSeen is ctx whose download progress (bytes, and whether from the cache) is kept.
func downloadSeen(ctx context.Context) (context.Context, func() StepEvent) {
	var mu sync.Mutex
	var last StepEvent
	return WithProgress(ctx, func(step string, ev StepEvent) {
			if step == StepDownload {
				mu.Lock()
				last = ev
				mu.Unlock()
			}
		}), func() StepEvent {
			mu.Lock()
			defer mu.Unlock()
			return last
		}
}

// A phone that gave up on a slow preview took the download with it, and asking again downloaded the package a
// second time beside the first (#3692). The download now outlives a caller who went away, the next caller joins it
// where it is (one request), and the one after takes the package from the cache.
func TestSharedDownloadOutlivesItsCallerAndIsJoined(t *testing.T) {
	data := bytes.Repeat([]byte("package "), 8<<10)
	h := newHalfThenWait(t, data)
	cache := t.TempDir()
	m := h.market(t, cache)
	pkg := &MarketPackage{URL: h.srv.URL + "/pkg.tar.gz", SHA256: ociSum(data), Size: int64(len(data))}

	first, leave := context.WithCancel(t.Context())
	first, seenFirst := downloadSeen(first)
	errc := make(chan error, 1)
	go func() { _, _, err := m.Download(first, pkg, t.TempDir()); errc <- err }()
	waitUntil(t, "data to arrive", func() bool { return seenFirst().Bytes > 0 })
	leave() // the phone's request timed out
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("the caller that left: %v", err)
	}

	second, seenSecond := downloadSeen(t.Context())
	type result struct {
		file string
		err  error
	}
	done := make(chan result, 1)
	go func() { file, _, err := m.Download(second, pkg, t.TempDir()); done <- result{file, err} }()
	waitUntil(t, "the second caller to join where it is", func() bool { return seenSecond().Bytes > 0 })
	close(h.release)
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	if b, _ := os.ReadFile(r.file); !bytes.Equal(b, data) {
		t.Fatalf("got %d bytes", len(b))
	}
	if n := h.requests.Load(); n != 1 || h.stopped.Load() != 0 {
		t.Fatalf("requests %d, stopped %d: the download started again", n, h.stopped.Load())
	}
	third, seenThird := downloadSeen(t.Context())
	if _, _, err := m.Download(third, pkg, t.TempDir()); err != nil || !seenThird().Cached || h.requests.Load() != 1 {
		t.Fatalf("not from the cache: %v %+v %d", err, seenThird(), h.requests.Load())
	}
	waitUntil(t, "its directory to go", func() bool { dirs, _ := filepath.Glob(filepath.Join(cache, ".fetch-*")); return len(dirs) == 0 })
}

// Nobody waiting any more: the download still finishes into the cache.
func TestSharedDownloadFinishesIntoTheCacheWithNobodyWaiting(t *testing.T) {
	data := bytes.Repeat([]byte("package "), 4<<10)
	h := newHalfThenWait(t, data)
	cache := t.TempDir()
	m := h.market(t, cache)
	pkg := &MarketPackage{URL: h.srv.URL + "/pkg.tar.gz", SHA256: ociSum(data), Size: int64(len(data))}
	ctx, leave := context.WithCancel(t.Context())
	ctx, seen := downloadSeen(ctx)
	errc := make(chan error, 1)
	go func() { _, _, err := m.Download(ctx, pkg, t.TempDir()); errc <- err }()
	waitUntil(t, "data to arrive", func() bool { return seen().Bytes > 0 })
	leave()
	<-errc
	close(h.release)
	waitUntil(t, "the package in the cache", func() bool { from, _ := cached(cache, pkg.SHA256); return from != "" })
	waitUntil(t, "its directory to go", func() bool { dirs, _ := filepath.Glob(filepath.Join(cache, ".fetch-*")); return len(dirs) == 0 })
	if h.stopped.Load() != 0 {
		t.Fatal("the download stopped with its caller")
	}
}

// What a run that ended mid-way left in the cache goes; a download going on stays.
func TestSharedDownloadDropsStaleDirectories(t *testing.T) {
	cache := t.TempDir()
	old, live := filepath.Join(cache, ".fetch-old"), filepath.Join(cache, ".fetch-live")
	for _, dir := range []string{old, live} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "download"), []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	then := time.Now().Add(-time.Hour)
	for _, p := range []string{old, filepath.Join(old, "download")} {
		if err := os.Chtimes(p, then, then); err != nil {
			t.Fatal(err)
		}
	}
	dropStaleFetches(cache)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the stale one stays")
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatal("the live one went")
	}
}

// Each caller's declared size must be checked even when the digest matches another in-flight request.
func TestSharedDownloadValidatesEachDeclaredSize(t *testing.T) {
	for _, invalidFirst := range []bool{false, true} {
		name := "valid-first"
		if invalidFirst {
			name = "invalid-first"
		}
		t.Run(name, func(t *testing.T) {
			data := bytes.Repeat([]byte("package "), 8<<10)
			h := newHalfThenWait(t, data)
			m := h.market(t, t.TempDir())
			valid := &MarketPackage{URL: h.srv.URL + "/pkg.tar.gz", SHA256: ociSum(data), Size: int64(len(data))}
			invalid := &MarketPackage{URL: valid.URL, SHA256: valid.SHA256, Size: int64(len(data) + 1)}
			first, second := valid, invalid
			if invalidFirst {
				first, second = invalid, valid
			}
			firstDir, secondDir := t.TempDir(), t.TempDir()
			ctxFirst, seenFirst := downloadSeen(t.Context())
			ctxSecond, seenSecond := downloadSeen(t.Context())
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() { _, _, err := m.Download(ctxFirst, first, firstDir); firstDone <- err }()
			waitUntil(t, "first download", func() bool { return seenFirst().Bytes > 0 })
			go func() { _, _, err := m.Download(ctxSecond, second, secondDir); secondDone <- err }()
			waitUntil(t, "second download", func() bool { return seenSecond().Bytes > 0 })
			close(h.release)
			firstErr, secondErr := <-firstDone, <-secondDone
			if invalidFirst {
				firstErr, secondErr = secondErr, firstErr
			}
			if firstErr != nil {
				t.Fatalf("valid package failed: %v", firstErr)
			}
			if secondErr == nil || secondErr.Error() != i18n.Err("extension-market_security.checksum").Error() {
				t.Fatalf("mismatched size: got %v, want checksum/size error", secondErr)
			}
			validDir := firstDir
			if invalidFirst {
				validDir = secondDir
			}
			if b, err := os.ReadFile(filepath.Join(validDir, "download")); err != nil || !bytes.Equal(b, data) {
				t.Fatalf("valid package: got %d bytes, error %v", len(b), err)
			}
		})
	}
}
