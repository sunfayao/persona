package filemeta

import "testing"

func TestFileMimeNamesWhatTheViewersOpen(t *testing.T) {
	t.Parallel()
	heic := append([]byte{0, 0, 0, 24}, []byte("ftypheic\x00\x00\x00\x00mif1heic")...)
	for _, test := range []struct {
		name string
		head []byte
		want string
	}{
		{"a.DOCX", nil, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"b.xlsx", nil, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"c.ipynb", nil, "application/x-ipynb+json"},
		{"d.heic", nil, "image/heic"},
		{"e.m4a", nil, "audio/mp4"},
		{"slides.key", []byte("PK\x03\x04rest"), "application/vnd.apple.keynote"},
		{"id.key", []byte("-----BEGIN PRIVATE KEY-----"), "text/plain; charset=utf-8"},
		{"photo", heic, "image/heic"}, // no extension: sniffed
		{"clip", []byte("\x00\x00\x00\x18ftypqt  "), "video/quicktime"},
		{"doc", []byte("%PDF-1.7\n"), "application/pdf"},
		{"notes", []byte("plain words\n"), "text/plain; charset=utf-8"},
		{"empty", nil, ""},
	} {
		if got := MIME(test.name, test.head); got != test.want {
			t.Errorf("%s: %q, want %q", test.name, got, test.want)
		}
	}
}
