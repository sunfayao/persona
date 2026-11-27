package extmgr

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/extwire"
	"github.com/sunfayao/persona/server/internal/voice"
)

// serveTranscriptionExtensionForTest is a transcription contribution's program (PERSONA_TEST_PLUGIN=transcription):
// /transcription/<cid>/info and a job socket for each cid of TRANSCRIPTION_CIDS, 404 for any other. The socket first
// says what it was asked with ("seen"), then echoes every frame; {"type":"big"} is answered with a 200 KiB frame.
func serveTranscriptionExtensionForTest() {
	ln, err := net.Listen("unix", os.Getenv("PERSONA_PLUGIN_SOCKET"))
	if err != nil {
		log.Fatal(err)
	}
	cids := strings.Split(os.Getenv("TRANSCRIPTION_CIDS"), ",")
	ours := func(w http.ResponseWriter, r *http.Request) bool {
		if !slices.Contains(cids, r.PathValue("cid")) {
			http.NotFound(w, r)
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /echo", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"path": r.URL.Path})
	})
	mux.HandleFunc("GET /transcription/{cid}/info", func(w http.ResponseWriter, r *http.Request) {
		if ours(w, r) {
			_ = json.NewEncoder(w).Encode(extwire.TranscriptionInfo{Protocol: extwire.TranscriptionProtocol,
				Features: []string{extwire.TranscriptionLive, extwire.TranscriptionReplay}})
		}
	})
	mux.HandleFunc("GET /transcription/{cid}/ws", func(w http.ResponseWriter, r *http.Request) {
		if !ours(w, r) {
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := r.Context()
		seen, _ := json.Marshal(map[string]string{"type": "seen", "path": r.URL.Path, "query": r.URL.RawQuery,
			"auth": r.Header.Get("Authorization"), "role": r.Header.Get(extwire.HeaderRole)})
		if c.Write(ctx, websocket.MessageText, seen) != nil {
			return
		}
		for {
			typ, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			if string(b) == `{"type":"big"}` {
				b = []byte(`{"type":"sentence","text":"` + strings.Repeat("x", 200<<10) + `"}`)
			}
			if c.Write(ctx, typ, b) != nil {
				return
			}
		}
	})
	_ = http.Serve(ln, mux)
}

// The installation a choice is pinned to never moves while the extension stays installed: not when the manager
// restarts (the database keeps microseconds, an install's clock nanoseconds, and a built-in's row is first written
// meanwhile), not when its market renames it. Installing it again moves it.
func TestTranscriptionInstallSurvivesARestart(t *testing.T) {
	manifest := func(id string) json.RawMessage {
		return json.RawMessage(`{"manifest_version": 1, "id": "` + id + `", "name": "ASR", "version": "1.0.0",
			"engines": {"plugin_protocol": 1}, "runtime": {"command": "bin/asr", "rpc": "none", "http": true},
			"activation_events": ["on_startup"], "contributes": {"transcription": [{"id": "asr", "name": "ASR"}]}}`)
	}
	h := newTestManager(t)
	h.extensions = newExtensions(h.cfg.dir, nil, h.logf)
	// load is the registry as the manager reads it when it starts: its built-in "talk", then the rows.
	load := func(rows ...extension.Record) {
		t.Helper()
		store := newExtensionStore()
		for _, r := range rows {
			store.rows[r.ID] = r
		}
		reg := extension.NewRegistry(h.extensions.root, store, nil, 0)
		m, err := extension.Parse(manifest("talk"))
		if err != nil {
			t.Fatal(err)
		}
		if err := reg.AddBuiltin(m, "", true); err != nil {
			t.Fatal(err)
		}
		if err := reg.Load(context.Background()); err != nil {
			t.Fatal(err)
		}
		h.extensions.reg = reg
	}
	installed := time.Date(2026, 11, 13, 10, 0, 0, 123456789, time.UTC)
	acme := extension.Record{ID: "acme", Source: extension.SourceInstalled, Version: "1.0.0", Manifest: manifest("acme"),
		Enabled: true, InstalledAt: installed}
	talk := extension.Record{ID: "talk", Source: extension.SourceBuiltin, Version: "1.0.0", Enabled: true, InstalledAt: installed}

	// As the install left it in memory; the built-in has no row yet.
	load(acme)
	pinned, builtin := h.TranscriptionInstall("acme/asr"), h.TranscriptionInstall("talk/asr")
	if pinned == "" || builtin == "" {
		t.Fatalf("pins %q %q", pinned, builtin)
	}

	// Restarted: the row as the database reads it back (to the microsecond, in its zone), the built-in's written.
	reread := acme
	reread.InstalledAt = installed.Truncate(time.Microsecond).In(time.FixedZone("UTC+8", 8*3600))
	load(reread, talk)
	if got := h.TranscriptionInstall("acme/asr"); got != pinned {
		t.Errorf("after a restart %q, pinned %q", got, pinned)
	}
	if got := h.TranscriptionInstall("talk/asr"); got != builtin {
		t.Errorf("the built-in after a restart %q, pinned %q", got, builtin)
	}

	// Renamed by its market: the old id still names it, and it is the same installation.
	renamed := reread
	renamed.ID, renamed.StorageID, renamed.PreviousID, renamed.Aliases = "acme-asr", "acme", "acme", []string{"acme"}
	renamed.Manifest = manifest("acme-asr")
	load(renamed, talk)
	if got := h.TranscriptionInstall("acme/asr"); got != pinned {
		t.Errorf("after a rename %q, pinned %q", got, pinned)
	}

	// Uninstalled and installed again: another installation.
	again := acme
	again.InstalledAt = installed.Add(time.Minute)
	load(again, talk)
	if got := h.TranscriptionInstall("acme/asr"); got == "" || got == pinned {
		t.Errorf("installed again %q, pinned %q", got, pinned)
	}
}

// The built-in voice extension contributes gpt-live to transcription beside its voice, and stays of kind voice.
func TestVoiceManifestTranscribes(t *testing.T) {
	h := newTestManager(t)
	h.version = "1.2.3"
	for _, id := range []string{"voice", "talk"} {
		raw, _ := json.Marshal(voiceManifest(h, id, ""))
		m, err := extension.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if m.Kind() != "voice" || !slices.Equal(m.Items("voice"), []string{id}) || !slices.Equal(m.Items("transcription"), []string{voice.TranscriptionContribution}) {
			t.Fatalf("%s: %v", id, m.Contributions())
		}
		if title := extension.ContributionTitle(m, "transcription", voice.TranscriptionContribution); title != "GPT-Live" {
			t.Fatalf("title %q", title)
		}
	}
}

// An assistant socket (assistantwire.SocketRoutes), relayed here as the extensions' sockets are, is the
// assistant's to answer, with who is calling in the hub's headers. It does not count in Serving: it lasts as long
// as the peer stays and must not hold a redeploy. Without the assistant it is 404.
func TestAssistantSocketIsNotServing(t *testing.T) {
	h := newTestManager(t)
	ctx := context.Background()
	type seen struct {
		role, device, auth, query string
		serving                   int64
	}
	got := make(chan seen, 1)
	h.opts.AssistantSocket = func(w http.ResponseWriter, r *http.Request) {
		got <- seen{r.Header.Get(extwire.HeaderRole), r.Header.Get(extwire.HeaderDevice), r.Header.Get("Authorization"), r.URL.RawQuery, h.Serving()}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			typ, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if c.Write(r.Context(), typ, b) != nil {
				return
			}
		}
	}
	mux := h.routes()
	counted := make(chan int64, 1)
	h.apiMux.HandleFunc("GET /api/v1/serving-now", func(w http.ResponseWriter, r *http.Request) { counted <- h.Serving() })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ws := "ws" + strings.TrimPrefix(srv.URL, "http")

	phone := deviceToken("phone", roleFull)
	c, _, err := websocket.Dial(ctx, ws+"/api/v1/a2a/m1/s1/.well-known/agent-card.json?at=1&token="+phone, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if s := <-got; s.role != roleFull || s.device != "phone" || s.auth != "" || s.query != "at=1" || s.serving != 0 {
		t.Fatalf("%+v", s)
	}
	if err := c.Write(ctx, websocket.MessageBinary, []byte{0, 1}); err != nil {
		t.Fatal(err)
	}
	if typ, b, err := c.Read(ctx); err != nil || typ != websocket.MessageBinary || string(b) != "\x00\x01" {
		t.Fatalf("%v %q %v", typ, b, err)
	}
	if n := h.Serving(); n != 0 {
		t.Fatalf("an open assistant socket counts: %d", n)
	}
	if code, _ := call(t, mux, "GET", "/api/v1/serving-now", h.cfg.Token, ""); code != 200 || <-counted != 1 {
		t.Fatal("another request counts")
	}
	c.Close(websocket.StatusNormalClosure, "")

	h.opts.AssistantSocket = nil
	if code, _ := call(t, mux, "GET", "/api/v1/a2a/m1/s1/.well-known/agent-card.json", h.cfg.Token, ""); code != http.StatusNotFound {
		t.Fatalf("without the assistant: %d", code)
	}
	if !isAssistantSocket(httptest.NewRequest("GET", "/api/v1/a2a/m/s/.well-known/agent-card.json", nil)) ||
		isAssistantSocket(httptest.NewRequest("PUT", "/api/v1/a2a/m/s/.well-known/agent-card.json", nil)) ||
		isAssistantSocket(httptest.NewRequest("GET", "/api/v1/a2a/m/s", nil)) {
		t.Fatal("isAssistantSocket")
	}
}
