package portprobe

import (
	"strings"
	"testing"
)

func TestPageTitle(t *testing.T) {
	t.Parallel()
	for page, want := range map[string]string{
		`<html><head><title>Vite + React</title></head>`: "Vite + React",
		"<TITLE lang=en>\n  My &amp; App\n</TITLE>":      "My & App",
		`<html><body>no title</body>`:                    "",
		"<title>" + strings.Repeat("长", 80) + "</title>": strings.Repeat("长", 60) + "…",
	} {
		if got := pageTitle(page); got != want {
			t.Errorf("pageTitle(%.30q) = %q, want %q", page, got, want)
		}
	}
}
