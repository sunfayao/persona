package extmgr

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"github.com/coder/websocket"

	"github.com/sunfayao/persona/server/internal/db/dbtest"
	"github.com/sunfayao/persona/server/internal/extension/sandbox"
	"github.com/sunfayao/persona/server/internal/extension/sourcevalidate"
	"github.com/sunfayao/persona/server/internal/voice"
	"github.com/sunfayao/persona/server/internal/volc"
)

// TestMain doubles as a plugin: re-run with PERSONA_TEST_PLUGIN=1 the test binary serves on PERSONA_PLUGIN_SOCKET.
// Otherwise the tests run on a database of their own (dbtest, #583).
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == sandbox.ValidationRunCommand {
		os.Exit(sandbox.RunValidation(os.Args[2:]))
	}
	if len(os.Args) > 1 && os.Args[1] == sourcevalidate.Command {
		os.Exit(sourcevalidate.Run(os.Args[2:], os.Stdin, os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == sandbox.RunCommand { // a sandbox's first process (extension_sandbox_test.go)
		os.Exit(sandbox.Run(os.Args[2:], os.Stderr))
	}
	// A transcription contribution's program, also run as the built-in voice extension (transcription_test.go).
	if os.Getenv("PERSONA_TEST_PLUGIN") == "transcription" {
		serveTranscriptionExtensionForTest()
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "volc" { // the built-in Volcengine speech extension run by the hub (volc_extension_test.go)
		if err := volc.Run(context.Background(), "test"); err != nil { // newTestManager's version
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "voice" { // the built-in voice extension run by the hub (voice_extension_test.go)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := voice.Run(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "boxed" {
		serveBoxedExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "1" {
		serveExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "connector" {
		serveConnectorExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "auth" {
		serveAuthExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "settingscheck" {
		serveSettingsCheckExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "errors" {
		serveErrorsExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "relay" {
		serveAuthRelayExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "secrets" {
		serveSecretProviderExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "repos" {
		serveRepositoryProviderExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "secretuser" {
		serveSecretUserExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "ui" {
		serveUIExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "images" {
		serveImagesExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "inboxui" {
		serveInboxUIExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "quota" {
		serveQuotaExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "usage" {
		serveUsageExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "messaging" {
		serveMessagingExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "api" {
		serveAPIExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "scoped" {
		serveScopedExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "agentsrc" {
		serveAgentSourceExtensionForTest()
		return
	}
	if os.Getenv("PERSONA_TEST_PLUGIN") == "live" {
		serveLiveExtensionForTest()
		return
	}
	os.Exit(dbtest.Main(m))
}

func serveExtensionForTest() {
	ln, err := net.Listen("unix", os.Getenv("PERSONA_PLUGIN_SOCKET"))
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /echo", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"path": r.URL.Path, "query": r.URL.RawQuery, "auth": r.Header.Get("Authorization"),
			"hub": os.Getenv("PERSONA_HUB_URL"), "id": os.Getenv("PERSONA_PLUGIN_ID"), "extra": os.Getenv("EXTRA"),
			"config": os.Getenv("PERSONA_PLUGIN_CONFIG"), "token": os.Getenv("PERSONA_HUB_TOKEN"),
			"manager": os.Getenv("PERSONA_MANAGER"),
		})
	})
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
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
			if err := c.Write(r.Context(), typ, b); err != nil {
				return
			}
		}
	})
	_ = http.Serve(ln, mux)
}

func TestHubLocalURL(t *testing.T) {
	for in, want := range map[string]string{":18660": "http://127.0.0.1:18660", "0.0.0.0:80": "http://127.0.0.1:80",
		"hub.example.com:9": "http://hub.example.com:9", "bad": "http://127.0.0.1:18660"} {
		if got := hubLocalURL(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
