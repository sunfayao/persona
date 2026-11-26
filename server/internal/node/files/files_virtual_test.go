package files

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/filemeta"
)

// The underlying file is real; its reported size models a virtual filesystem's stable, overstated size.
func overreportedFileNode(t *testing.T, contents []byte, size int64) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "virtual.txt")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	config := testConfig(dir)
	config.Size = func(os.FileInfo) int64 { return size }
	config.IsSysfs = func(ReadHandle) (bool, error) { return true, nil }
	return New(config), path
}

func TestReadFileOverreportedEOF(t *testing.T) {
	t.Parallel()
	n, path := overreportedFileNode(t, []byte("up\n"), 4096)
	for _, tc := range []struct {
		name   string
		offset int64
		text   string
	}{
		{"content", 0, "up\n"},
		{"at EOF", 3, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc, err := n.Read(path, "", tc.offset, false)
			if err != nil {
				t.Fatal(err)
			}
			if fc.Text != tc.text || fc.Size != 3 || fc.Offset != tc.offset || fc.Truncated || fc.Next != 0 {
				t.Fatalf("page at EOF = %+v", fc)
			}
		})
	}
	for _, offset := range []int64{4, 4096} {
		if fc, err := n.Read(path, "", offset, false); err == nil {
			t.Fatalf("offset %d past the actual end: %+v", offset, fc)
		}
	}
}

func TestReadFileOverreportedPages(t *testing.T) {
	t.Parallel()
	// The first page cuts a multi-byte rune in a line longer than a page.
	content := strings.Repeat("x", maxFileBytes-2) + "界\n" + strings.Repeat("y", 200)
	n, path := overreportedFileNode(t, []byte(content), int64(len(content)+4096))
	first, err := n.Read(path, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Truncated || first.Next != int64(maxFileBytes-2) || first.Text != content[:maxFileBytes-2] {
		t.Fatalf("first page: size=%d next=%d truncated=%v text bytes=%d", first.Size, first.Next, first.Truncated, len(first.Text))
	}
	last, err := n.Read(path, "", first.Next, false)
	if err != nil {
		t.Fatal(err)
	}
	if last.Truncated || last.Next != 0 || last.Size != int64(len(content)) || first.Text+last.Text != content {
		t.Fatalf("last page: size=%d next=%d truncated=%v text bytes=%d", last.Size, last.Next, last.Truncated, len(last.Text))
	}
}

func TestReadFileShortOrdinaryReadIsNotComplete(t *testing.T) {
	t.Parallel()
	n, path := overreportedFileNode(t, []byte("up\n"), 4096)
	n.config.IsSysfs = func(ReadHandle) (bool, error) { return false, nil }
	if fc, err := n.Read(path, "", 0, false); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ordinary short read = %+v, %v", fc, err)
	}
}

func TestReadFileEmptyRegular(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	n := New(testConfig(dir))
	fc, err := n.Read(path, "", 0, false)
	if err != nil || fc == nil || fc.Size != 0 || fc.Text != "" || fc.Data != "" || fc.Truncated || fc.Next != 0 {
		t.Fatalf("empty regular file = %+v, %v", fc, err)
	}
}

func TestReadFileVirtualBinaryAndStream(t *testing.T) {
	t.Parallel()
	data := []byte{0xff, 0, 0x80, 0x42}
	n, path := overreportedFileNode(t, data, 4096)
	fc, err := n.Read(path, "", 0, false)
	if err != nil || fc == nil || fc.Data != base64.StdEncoding.EncodeToString(data) || fc.Size != int64(len(data)) || fc.Truncated {
		t.Fatalf("binary page = %+v, %v", fc, err)
	}
	fc, err = n.Read(path, "", 0, true)
	if err != nil || fc == nil || !fc.Stream || fc.Data != "" || fc.Size != 4096 || fc.Revision == "" {
		t.Fatalf("binary stream descriptor = %+v, %v", fc, err)
	}
}

func TestReadFileSysfsIdentificationFailure(t *testing.T) {
	t.Parallel()
	want := errors.New("synthetic filesystem identification failure")
	n, path := overreportedFileNode(t, []byte("up\n"), 4096)
	n.config.IsSysfs = func(ReadHandle) (bool, error) { return false, want }
	if fc, err := n.Read(path, "", 0, false); !errors.Is(err, want) {
		t.Fatalf("filesystem identification failure = %+v, %v", fc, err)
	}
}

type interceptedReadFile struct {
	*os.File
	readErr   error
	readAtErr error
	afterRead func()
}

func (f *interceptedReadFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	if f.readErr != nil {
		return n, f.readErr
	}
	return n, err
}

func (f *interceptedReadFile) ReadAt(p []byte, offset int64) (int, error) {
	n, err := f.File.ReadAt(p, offset)
	if f.afterRead != nil {
		after := f.afterRead
		f.afterRead = nil
		after()
	}
	if f.readAtErr != nil {
		return n, f.readAtErr
	}
	return n, err
}

func TestReadFilePropagatesReadErrors(t *testing.T) {
	t.Parallel()
	want := errors.New("synthetic read failure")
	for _, tc := range []struct {
		name     string
		contents string
		stream   bool
		read     bool
	}{
		{"ordinary", "content", false, false},
		{"zero-size regular", "", false, true},
		{"stream sniff", "content", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "sample.mp4")
			if err := os.WriteFile(path, []byte(tc.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			config := testConfig(dir)
			config.Open = func(path string) (ReadHandle, error) {
				f, err := os.Open(path)
				if err != nil {
					return nil, err
				}
				wrapped := &interceptedReadFile{File: f}
				if tc.read {
					wrapped.readErr = want
				} else {
					wrapped.readAtErr = want
				}
				return wrapped, nil
			}
			n := New(config)
			if fc, err := n.Read(path, "", 0, tc.stream); !errors.Is(err, want) {
				t.Fatalf("read = %+v, %v", fc, err)
			}
		})
	}
}

func TestReadFileDetectsFileChanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, path string, f *os.File) (beforeOpen bool, afterRead func())
	}{
		{"truncated before open", func(t *testing.T, path string, _ *os.File) (bool, func()) {
			if err := os.Truncate(path, 1); err != nil {
				t.Fatal(err)
			}
			return true, nil
		}},
		{"truncated during read", func(t *testing.T, path string, _ *os.File) (bool, func()) {
			return false, func() {
				if err := os.Truncate(path, 1); err != nil {
					t.Fatal(err)
				}
			}
		}},
		{"path replaced during read", func(t *testing.T, path string, _ *os.File) (bool, func()) {
			return false, func() {
				if err := os.Rename(path, path+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "sample.txt")
			if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
				t.Fatal(err)
			}
			config := testConfig(dir)
			config.Open = func(path string) (ReadHandle, error) {
				// This hook runs after the guarded stat and before the open.
				before, after := tc.prepare(t, path, nil)
				f, err := os.Open(path)
				if err != nil {
					return nil, err
				}
				if before {
					return f, nil
				}
				return &interceptedReadFile{File: f, afterRead: after}, nil
			}
			n := New(config)
			if fc, err := n.Read(path, "", 0, false); !errors.Is(err, filemeta.ErrChanged) {
				t.Fatalf("changed read = %+v, %v", fc, err)
			}
		})
	}
}

func TestReadFileRejectsChangedVirtualFile(t *testing.T) {
	t.Parallel()
	n, path := overreportedFileNode(t, []byte("up\n"), 4096)
	n.config.Open = func(path string) (ReadHandle, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &interceptedReadFile{File: f, afterRead: func() {
			if err := os.Truncate(path, 1); err != nil {
				t.Fatal(err)
			}
		}}, nil
	}
	if fc, err := n.Read(path, "", 0, false); !errors.Is(err, filemeta.ErrChanged) {
		t.Fatalf("changed virtual read = %+v, %v", fc, err)
	}
}

func TestReadFileDetectsSymlinkRetarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, second, link := filepath.Join(dir, "first.txt"), filepath.Join(dir, "second.txt"), filepath.Join(dir, "link.txt")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(first, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	config := testConfig(dir)
	config.Open = func(path string) (ReadHandle, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &interceptedReadFile{File: f, afterRead: func() {
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(second, link); err != nil {
				t.Fatal(err)
			}
		}}, nil
	}
	n := New(config)
	if fc, err := n.Read(link, "", 0, false); !errors.Is(err, filemeta.ErrChanged) {
		t.Fatalf("retargeted read = %+v, %v", fc, err)
	}
}
