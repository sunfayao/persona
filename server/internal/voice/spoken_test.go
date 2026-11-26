package voice

import (
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/i18n"
)

func TestSpeakable(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"改了 `server/internal/voice/runs.go` 和 docs/assistant/voice.md", "改了 the file runs.go 和 the file voice.md"},
		{"见 runs.go:42 和 /etc/hosts", "见 line 42 of runs.go 和 the file hosts"},
		{"运行 `npm test`，**全部**通过", "运行 npm test，全部通过"},
		{"看 https://example.com/a?b=1。然后", "看 (a link)。然后"},
		{"[文档](https://example.com/doc) 和 [https://example.com](https://example.com)", "文档 和 (a link)"},
		{"提交 a0781ebb8 和 550e8400-e29b-41d4-a716-446655440000", "提交 (a hash) 和 (a hash)"},
		{"1234567 个、deadbeef、2026/11/06、3/4、and/or、TCP/IP", "1234567 个、deadbeef、2026/11/06、3/4、and/or、TCP/IP"},
		{"# 标题\n- [x] 第一项\n2. 第二项\n> 引用\n***", "标题\n第一项\n第二项\n引用"},
		{"~~旧的~~ *新的*", "旧的 新的"},
		{"```\n未闭合的代码\n第二行", "(a code block of 2 lines, on the screen)"},
		{"| a | b |\n|:--|--:|\n| 1 | 2 |\n后面", "(a table of 1 row, on the screen)\n后面"},
		{"`" + strings.Repeat("x", inlineCodeMost+1) + "`", "(a piece of code)"},
		// Link text is read as text: what it holds is said as everywhere else, never a mark.
		{"见 [`npm test`](https://example.com/x) 的输出", "见 npm test 的输出"},
		{"- [`runs.go`](server/internal/voice/runs.go)：跟踪一轮", "runs.go：跟踪一轮"},
		{"见 [`abc1234`](https://example.com/commit/abc1234) 的改动", "见 (a hash) 的改动"},
		{"[server/internal/voice/runs.go](https://example.com/x) 和 [Login.tsx:42](src/Login.tsx#L42)", "the file runs.go 和 line 42 of Login.tsx"},
		{"[`a/b.go`](x) 里的 [**粗体**](y)", "the file b.go 里的 粗体"},
		{"不是标记：\uE0000\uE001", "不是标记：0"},
	} {
		got := speakable(i18n.ZhHans, c.in)
		if got != c.want {
			t.Errorf("%q\n got %q\nwant %q", c.in, got, c.want)
		}
		if strings.ContainsAny(got, "\uE000\uE001") {
			t.Errorf("%q: a mark is left in %q", c.in, got)
		}
	}
	// The stand-ins are English whatever the language: they are heard by the voice model, not read by the user.
	for _, l := range []i18n.Lang{i18n.En, i18n.Ja} {
		if got := speakable(l, "see `a/b/c.go:7`"); got != "see line 7 of c.go" {
			t.Errorf("%s: %q", l, got)
		}
	}
}

// A long answer keeps its start and its end within spokenMost.
func TestSpokenAnswerKeepsItsEnds(t *testing.T) {
	long := "开头的结论。" + strings.Repeat("中间的过程。", 1000) + "最后的结论。"
	got := spokenAnswer(i18n.ZhHans, long)
	if n := len([]rune(got)); n > spokenMost {
		t.Fatalf("%d characters", n)
	}
	if !strings.HasPrefix(got, "开头的结论。") || !strings.HasSuffix(got, "最后的结论。") || !strings.Contains(got, "the middle is left out") {
		t.Fatalf("%q…%q", got[:60], got[len(got)-60:])
	}
	short := "一句话。"
	if spokenAnswer(i18n.ZhHans, short) != short {
		t.Fatal("a short answer was cut")
	}
}

// The codex dialect says in pieces of at most 500 bytes; the live one in one commentary (its limit is characters).
func TestSayPieces(t *testing.T) {
	text := strings.Repeat("中a", 600)
	var whole strings.Builder
	for _, f := range (codexWire{}).say(text, "") {
		piece := f.(map[string]any)["content"].([]contentPart)[0].Text
		if len(piece) > codexAppendBytes {
			t.Fatalf("a piece of %d bytes", len(piece))
		}
		whole.WriteString(piece)
	}
	if whole.String() != text {
		t.Fatal("the pieces are not the text")
	}
	frames := (liveWire{}).say(text, "")
	if len(frames) != 1 || frames[0].(map[string]any)["content"] != text {
		t.Fatalf("live say: %v", frames)
	}
	got := (liveWire{}).reply("d", strings.Repeat("字", 5000), true, "")[0].(map[string]any)["content"].(string)
	if len([]rune(got)) > commentaryRunes+1 {
		t.Fatalf("a commentary of %d characters", len([]rune(got)))
	}
}
