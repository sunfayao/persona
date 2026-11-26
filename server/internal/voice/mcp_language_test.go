package voice

import (
	"encoding/json"
	"github.com/coder/websocket"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/extwire"
	"github.com/sunfayao/persona/server/internal/prompts"
)

func TestGoldenMCPVoiceLanguages(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en", "ja"} {
		for name, set := range map[string][]string{"ordinary": toolNames, "bound": boundTools} {
			tools := toolDecls(set, lang)
			b, err := json.MarshalIndent(tools, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			golden(t, "mcp_language."+name+"."+lang, string(b)+"\n")
			if name == "bound" {
				// The descriptions are English whatever the language, and name the English marks.
				for _, mark := range []string{"Status:", "Stop:", "Answer:"} {
					if !strings.Contains(string(b), mark) {
						t.Fatalf("%s lost parser prefix %s", lang, mark)
					}
				}
				// The marks older versions told the model in Chinese are still understood.
				for prefix, want := range map[string]string{"Status:": "status", "Stop:": "stop", "Answer:": "answer",
					"状态：": "status", "停止：": "stop", "回答：": "answer"} {
					kind, rest := splitMark(prefix + "source text")
					if kind != want || rest != "source text" {
						t.Fatalf("instruction prefix %s no longer parses", prefix)
					}
				}
			}
			if lang != "en" {
				if en := toolDecls(set, "en"); !reflect.DeepEqual(en, tools) {
					t.Fatalf("%s %s tools differ from the English ones", lang, name)
				}
			}
		}
	}
}

// The actual upstream session.start must carry the caller's language too.
func TestVoiceDeclaresLocalizedMCPToolsUpstream(t *testing.T) {
	for _, lang := range []string{"en", "ja", "zh-Hans"} {
		t.Run(lang, func(t *testing.T) {
			hub := newFakeHub(t)
			up := newFakeUpstream(t, func(u *fakeUpstream, c *websocket.Conn, f map[string]any) {
				if f["type"] == "session.start" {
					u.send(c, map[string]any{"type": "session.started", "session": map[string]any{}})
				}
			})
			pl := testPlugin(t, Config{Providers: []Provider{{ID: "custom", URL: up.url(), Models: []string{"m1"}}}}, hub.server(t))
			a := dialApp(t, pl, map[string]any{"type": "start", "model": "custom/m1"}, http.Header{extwire.HeaderLang: {lang}})
			a.until(ofType("ready"))
			start, _ := sessionStart(up)
			raw, _ := json.Marshal(start["tools"])
			var tools []toolDecl
			if err := json.Unmarshal(raw, &tools); err != nil {
				t.Fatal(err)
			}
			for _, tool := range tools {
				if tool.Description != prompts.Tool(tool.Name, lang).Description {
					t.Fatalf("upstream %s language mismatch", tool.Name)
				}
			}
			if len(tools) != len(toolNames) {
				t.Fatal("upstream tools changed")
			}
		})
	}
}
