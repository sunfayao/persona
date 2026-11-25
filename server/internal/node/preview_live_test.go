package node

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/preview"
)

// previewPage: a button that renames the page, a field that does too as it is typed in, and a link elsewhere.
const previewPage = `<!doctype html><html><head><title>Hi</title><style>
body { margin: 0; background: #00f } #go { position: absolute; left: 0; top: 0; width: 100px; height: 50px }
#in { position: absolute; left: 0; top: 100px; width: 200px; height: 40px }
#ext { position: absolute; left: 0; top: 200px; width: 200px; height: 40px; display: block }
#next { position: absolute; left: 0; top: 300px; width: 200px; height: 40px; display: block }
</style></head><body>
<button id="go" onclick="document.title='clicked'">Go</button>
<input id="in" oninput="document.title='v:'+this.value">
<a id="ext" href="https://example.com/away">elsewhere</a>
<a id="next" href="/second">second</a>
</body></html>`

type liveMsg struct {
	kind byte
	json map[string]any
	bin  []byte
}

// next reads messages until want says one is it, acknowledging pictures as the web does.
func next(t *testing.T, r *bufio.Reader, w net.Conn, what string, want func(liveMsg) bool) liveMsg {
	t.Helper()
	if err := w.SetReadDeadline(time.Now().Add(20 * time.Second)); err != nil {
		t.Fatal(err)
	}
	defer w.SetReadDeadline(time.Time{})
	for {
		k, p, err := preview.ReadFrame(r)
		if err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		m := liveMsg{kind: k, bin: p}
		if k == preview.Text {
			_ = json.Unmarshal(p, &m.json)
		} else if len(p) > 0 && p[0] == previewFrameMsg {
			say(t, w, map[string]any{"t": "ack", "seq": binary.BigEndian.Uint32(p[1:])})
		}
		if want(m) {
			return m
		}
	}
}

func say(t *testing.T, w net.Conn, v any) {
	b, _ := json.Marshal(v)
	if err := preview.WriteFrame(w, preview.Text, b); err != nil {
		t.Errorf("write: %v", err)
	}
}

func isText(typ string, more func(map[string]any) bool) func(liveMsg) bool {
	return func(m liveMsg) bool {
		return m.kind == preview.Text && m.json["t"] == typ && (more == nil || more(m.json))
	}
}

func click(t *testing.T, w net.Conn, x, y float64) {
	say(t, w, map[string]any{"t": "mouse", "type": "move", "x": x, "y": y})
	say(t, w, map[string]any{"t": "mouse", "type": "down", "x": x, "y": y, "button": "left", "buttons": 1, "clicks": 1})
	say(t, w, map[string]any{"t": "mouse", "type": "up", "x": x, "y": y, "button": "left", "clicks": 1})
}
