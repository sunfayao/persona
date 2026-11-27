package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sunfayao/persona/server/internal/extwire"
	"github.com/sunfayao/persona/server/internal/voice/voicetest"
)

// trScript is a meeting of six sentences in Chinese, English and both in one sentence, with names and terms.
var trScript = []voicetest.Line{
	{Start: 0.5, End: 3.0, Text: "我们这周先把 export cap 保持在 4 MB。"},
	{Start: 3.8, End: 5.6, Text: "Bo owns the vendor follow-up."},
	{Start: 6.4, End: 9.6, Text: "下周三之前 Alice 要把 pricing 的 proposal 发出来。"},
	{Start: 10.6, End: 12.4, Text: "Let's revisit pricing next week."},
	{Start: 13.2, End: 16.0, Text: "这个 release 先只发 iOS，Android 下个 sprint 再跟。"},
	{Start: 17.0, End: 19.0, Text: "Can we get the dashboard numbers before Friday?"},
}

const trScriptLength = 21.0

// trFast shortens the job's numbers for a test: windows of 4 s, settled after 1 s of audio.
func trFast(t *testing.T) {
	t.Helper()
	setVar(t, &trTick, 10*time.Millisecond)
	setVar(t, &trWindow, 4*time.Second)
	setVar(t, &trWindowMost, 12*time.Second)
	setVar(t, &trSettle, time.Second)
	setVar(t, &trSettleWall, 80*time.Millisecond)
	setVar(t, &trCaptionSettle, 500*time.Millisecond)
	setVar(t, &trWindowWait, 4*time.Second)
	setVar(t, &trReplyWait, time.Second)
	setVar(t, &trStall, 300*time.Millisecond)
	setVar(t, &trFlushSilence, time.Second)
	setVar(t, &trCatchUp, 200.0)
	setVar(t, &trCaptionJump, time.Minute) // the tests stream faster than real time: the caption session never skips
	setVar(t, &trReconnectWaits, []time.Duration{0, 20 * time.Millisecond, 20 * time.Millisecond})
	setVar(t, &trUsageMost, 200*time.Millisecond)
	setVar(t, &trReplayWindow, 4*time.Second)
	setVar(t, &trReplayWindowMost, 8*time.Second)
	setVar(t, &trReplayOver, 2*time.Second)
	setVar(t, &trRefGap, 3*time.Second)
	setVar(t, &trRefPad, 500*time.Millisecond)
	setVar(t, &trReplayTail, time.Second)
	setVar(t, &trReplayQuiet, 100*time.Millisecond)
	setVar(t, &trReplayTailMost, 3*time.Second)
	setVar(t, &trStopEarly, 2*time.Second)
}

// trLive is a plugin whose one upstream is the stand-in for script s.
func trLive(t *testing.T, s voicetest.Script) (*Plugin, *httptest.Server, *lockedLog, *voicetest.Live) {
	t.Helper()
	live := voicetest.NewLive(t, s)
	pl, srv, logs := transcriptionPlugin(t, Config{Providers: []Provider{{ID: "t", URL: live.URL(), Dialect: DialectLive, Key: "k",
		DefaultModel: "m1"}}, Default: "t/m1"})
	return pl, srv, logs, live
}

type lockedLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func transcriptionPlugin(t *testing.T, cfg Config) (*Plugin, *httptest.Server, *lockedLog) {
	t.Helper()
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
	logs := &lockedLog{}
	pl := New(cfg, "http://127.0.0.1:1", "hubtoken", log.New(logs, "", 0))
	srv := httptest.NewServer(pl.Handler())
	t.Cleanup(srv.Close)
	return pl, srv, logs
}

// bareTrPlugin serves a plugin of cfg as it is (one without upstreams is not valid to Config.validate).
func bareTrPlugin(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	pl := New(cfg, "http://127.0.0.1:1", "hubtoken", log.New(io.Discard, "", 0))
	srv := httptest.NewServer(pl.Handler())
	t.Cleanup(srv.Close)
	return srv
}

// trClient is the manager's side of a job.
type trClient struct {
	t      *testing.T
	c      *websocket.Conn
	frames chan extwire.TranscriptionFrame
	mu     sync.Mutex
	got    []extwire.TranscriptionFrame
	closed chan struct{}
	status websocket.StatusCode
}

func dialTr(t *testing.T, srv *httptest.Server, cid string, header http.Header) (*trClient, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/transcription/" + cid + "/ws"
	c, res, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return nil, res, err
	}
	tc := &trClient{t: t, c: c, frames: make(chan extwire.TranscriptionFrame, 4096), closed: make(chan struct{})}
	go func() {
		defer close(tc.closed)
		for {
			_, b, err := c.Read(context.Background())
			if err != nil {
				tc.status = websocket.CloseStatus(err)
				return
			}
			var f extwire.TranscriptionFrame
			if json.Unmarshal(b, &f) != nil {
				t.Errorf("not a frame: %s", b)
				continue
			}
			tc.mu.Lock()
			tc.got = append(tc.got, f)
			tc.mu.Unlock()
			tc.frames <- f
		}
	}()
	t.Cleanup(func() { _ = c.CloseNow() })
	return tc, res, nil
}

// startTr opens a job and sends its start.
func startTr(t *testing.T, srv *httptest.Server, start extwire.TranscriptionFrame) *trClient {
	t.Helper()
	tc, _, err := dialTr(t, srv, TranscriptionContribution, http.Header{extwire.HeaderRole: {roleFull}})
	if err != nil {
		t.Fatal(err)
	}
	tc.send(start)
	return tc
}

func (tc *trClient) send(f extwire.TranscriptionFrame) {
	b, _ := json.Marshal(f)
	_ = tc.c.Write(context.Background(), websocket.MessageText, b)
}

// stream sends pcm in 100 ms frames at speed times real time.
func (tc *trClient) stream(pcm []byte, speed float64) {
	tick := time.NewTicker(time.Duration(float64(100*time.Millisecond) / speed))
	defer tick.Stop()
	for off := 0; off < len(pcm); off += 3200 {
		<-tick.C
		if err := tc.c.Write(context.Background(), websocket.MessageBinary, pcm[off:min(off+3200, len(pcm))]); err != nil {
			return
		}
	}
}

// next is the next frame of type typ (the ones before it are skipped).
func (tc *trClient) next(typ string) extwire.TranscriptionFrame {
	tc.t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case f := <-tc.frames:
			if f.Type == typ {
				return f
			}
		case <-deadline:
			tc.t.Fatalf("no %s frame came", typ)
		}
	}
}

// done waits for the job's socket to close and is every frame it sent.
func (tc *trClient) done() []extwire.TranscriptionFrame {
	tc.t.Helper()
	select {
	case <-tc.closed:
	case <-time.After(20 * time.Second):
		tc.t.Fatal("the job never ended")
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return append([]extwire.TranscriptionFrame(nil), tc.got...)
}

func liveStart(glossary ...string) extwire.TranscriptionFrame {
	return extwire.TranscriptionFrame{Type: extwire.TFStart, Mode: extwire.TranscriptionLive, Language: "zh-CN",
		Glossary: glossary, At: extwire.F(0.0)}
}

// A malformed job is refused with bad_frame.
func TestTranscriptionBadFrames(t *testing.T) {
	trFast(t)
	_, srv, _, _ := trLive(t, voicetest.Script{Sentences: trScript})
	for name, send := range map[string]func(tc *trClient){
		"audio first":  func(tc *trClient) { _ = tc.c.Write(context.Background(), websocket.MessageBinary, make([]byte, 320)) },
		"another mode": func(tc *trClient) { tc.send(extwire.TranscriptionFrame{Type: extwire.TFStart, Mode: "batch"}) },
		"odd audio": func(tc *trClient) {
			tc.send(liveStart())
			tc.next(extwire.TFReady)
			_ = tc.c.Write(context.Background(), websocket.MessageBinary, make([]byte, 321))
		},
	} {
		t.Run(name, func(t *testing.T) {
			tc, _, err := dialTr(t, srv, TranscriptionContribution, nil)
			if err != nil {
				t.Fatal(err)
			}
			send(tc)
			if e := tc.next(extwire.TFError); e.Code != extwire.TEBadFrame || !e.Fatal {
				t.Fatalf("error %+v", e)
			}
			tc.done()
			if tc.status != websocket.StatusPolicyViolation {
				t.Errorf("closed with %d", tc.status)
			}
		})
	}
}

// replayStart is a final pass's start with the script's text as the reference, by sentence.
func replayStart(t *testing.T, srv *httptest.Server, speed float64, refs []voicetest.Line) *trClient {
	t.Helper()
	tc := startTr(t, srv, extwire.TranscriptionFrame{Type: extwire.TFStart, Mode: extwire.TranscriptionReplay, Speed: speed,
		Language: "zh-CN", At: extwire.F(0.0)})
	if ready := tc.next(extwire.TFReady); strings.Join(ready.Features, ",") != "replay" {
		t.Fatalf("ready %+v", ready)
	}
	for _, r := range refs {
		tc.send(extwire.TranscriptionFrame{Type: extwire.TFReference, From: extwire.F(r.Start), To: extwire.F(r.End), Text: extwire.F(r.Text)})
	}
	return tc
}

// A session that starts as its job ends is closed, never handed to a loop that is gone (its socket would stay open,
// billed, until the service dropped it).
func TestTranscriptionSessionStartedAsTheJobEndsIsClosed(t *testing.T) {
	trFast(t)
	pl, _, _, live := trLive(t, voicetest.Script{Sentences: trScript})
	ctx, cancel := context.WithCancel(context.Background())
	j := newTrJob(pl, nil, ctx, cancel, "")
	p, model, err := pl.transcriber(ctx)
	if err != nil {
		t.Fatal(err)
	}
	j.p, j.model = p, model
	started, ended := make(chan struct{}), make(chan struct{})
	setVar(t, &trStartedHook, func() {
		close(started)
		<-ended // the job ends while the session is on its way to the loop
	})
	newTrSession(j, trVerbatim, nil, 0, "first").connect(j.ctx, j.instructions(trVerbatim, nil))
	<-started
	cancel() // the loop is gone: nothing takes the session
	close(ended)
	waitFor(t, "the session closed", func() bool { return live.Open() == 0 })
}
