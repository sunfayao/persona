package node

import (
	"testing"
)

// agyIdleScreen is an agy at rest with an empty input box: what a start that lost its first message left (#11345).
const agyIdleScreen = " Antigravity CLI\n╭──────────────────────────────────────╮\n│ >                                    │\n╰──────────────────────────────────────╯\n ? for shortcuts\n"

const agyFirstMessage = "Review the build script and list what breaks on Windows."

func TestPromptShown(t *testing.T) {
	long := "请按 docs/review.md 里的要求做一份独立评审，评审写成中文 Markdown，存到 reviews 目录下，最后只回复文件路径。"
	for _, tc := range []struct {
		name, screen, prompt string
		want                 bool
	}{
		{"empty box", agyIdleScreen, agyFirstMessage, false},
		{"wrapped in a border", "│ > Review the build script and │\n│ list what breaks on Windows. │", agyFirstMessage, true},
		{"only its end in a scrolled box", "│ … list what breaks on Windows. │", agyFirstMessage, true},
		{"CJK, wrapped", "│ > 请按 docs/review.md 里的要求做一份独立 │\n│ 评审，评审写成中文 Markdown，存到 │", long, true},
		{"CJK, empty box", agyIdleScreen, long, false},
		{"paste chip", "│ > [Pasted text #2 +40 lines] │", long, true},
		{"short message", "│ > hi │", "hi", true},
		{"short message absent", agyIdleScreen, "go on", false},
	} {
		if got := promptShown(tc.screen, tc.prompt); got != tc.want {
			t.Errorf("%s: promptShown = %v, want %v", tc.name, got, tc.want)
		}
	}
}
