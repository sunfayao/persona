package extmgr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/sdk/go/wire"
	pserve "github.com/sunfayao/persona/server/internal/extension/serve"
)

func testPNG() []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewGray(image.Rect(0, 0, 4, 3)))
	return b.Bytes()
}

// pngClaiming is a png's header saying it is w × h (enough for the size check).
func pngClaiming(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&b, binary.BigEndian, uint32(13))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

// serveImagesExtensionForTest draws pictures by ref and serves them (ui.image), writing each call to IMAGES_LOG.
func serveImagesExtensionForTest() {
	logCall := func(ref string, width int) {
		f, err := os.OpenFile(os.Getenv("IMAGES_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%s %d\n", ref, width)
			_ = f.Close()
		}
	}
	b64 := func(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
	view := map[string]any{"sections": []any{map[string]any{"items": []any{
		map[string]any{"type": "image", "key": "cam", "src": map[string]any{"ref": "cam"}, "alt": "前门"},
		map[string]any{"type": "row", "key": "r", "title": "快递", "thumbnail": map[string]any{"ref": "cam"}},
		map[string]any{"type": "image", "key": "web", "src": map[string]any{"url": "https://example.com/a.png"}, "alt": "网上的"},
		map[string]any{"type": "grid", "key": "g", "tiles": []any{map[string]any{"key": "t", "title": "院子", "image": map[string]any{"ref": "slow"}}}},
	}}}}
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "example-images", Version: "1.0.0",
		Namespaces: map[string]pserve.Handler{
			"ui": func(_ context.Context, method string, params json.RawMessage) (any, error) {
				switch method {
				case "render":
					return view, nil
				case "action":
					return map[string]any{"items": []any{map[string]any{"type": "image", "key": "cam", "src": map[string]any{"ref": "cam"}, "alt": "前门"}}}, nil
				case "image":
					var p wire.UIImageParams
					_ = json.Unmarshal(params, &p)
					logCall(p.Ref, p.Width)
					age := 60
					switch p.Ref {
					case "slow":
						time.Sleep(300 * time.Millisecond) // sleep-ok: a slow camera, so the concurrent requests for it overlap and coalesce into one ui.image
					case "noage":
						age = 0
					case "svg":
						return map[string]any{"mime": "image/png", "data": b64([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))}, nil
					case "html":
						return map[string]any{"mime": "image/png", "data": b64([]byte("<!DOCTYPE html><script>alert(1)</script>"))}, nil
					case "huge":
						return map[string]any{"mime": "image/png", "data": b64(pngClaiming(9000, 10))}, nil
					case "fail":
						return nil, fmt.Errorf("the camera is off")
					}
					return map[string]any{"mime": "image/jpeg", "data": b64(testPNG()), "max_age": age}, nil
				}
				return nil, pserve.UnknownMethod(method)
			}}})
}

func calls(t *testing.T, log string) []string {
	b, _ := os.ReadFile(log)
	return strings.Fields(strings.ReplaceAll(string(b), "\n", " "))
}

// get fetches a picture's path as a device with token.
func get(h http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// A token is the hub's own: it opens with its key only, untouched, before its time.
func TestImageTokens(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1_800_000_000, 0)
	tok := signImage(key, imageToken{Ext: "e", UI: "u", Ref: "r", Exp: now.Add(time.Minute).Unix()})
	if got, err := openImage(key, tok, now); err != nil || got.Ref != "r" || got.Ext != "e" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := openImage(key, tok, now.Add(time.Minute)); err != errImageExpired {
		t.Fatalf("expired: %v", err)
	}
	if _, err := openImage([]byte("another key of thirty-two bytes!"), tok, now); err != errImageToken {
		t.Fatalf("another key: %v", err)
	}
	for _, bad := range []string{"", ".", "a.b", tok + "x", "x" + tok, strings.Repeat("a", 3000)} {
		if _, err := openImage(key, bad, now); err == nil {
			t.Errorf("%q opened", bad)
		}
	}
	if !wireOK(imagePath("ext", tok)) {
		t.Fatalf("a path the schema refuses: %s", imagePath("ext", tok))
	}
}

// wireOK says whether path holds to the schema's ImageSource.path.
func wireOK(path string) bool {
	return wire.Validate("ImageSource", map[string]any{"ref": "r", "path": path}) == nil
}
