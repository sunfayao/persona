package shellcmd

import (
	"testing"
)

func TestShellFirstWord(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"/usr/bin/personad hook":       "/usr/bin/personad",
		"'/opt/my dir/personad' hook":  "/opt/my dir/personad",
		`'/opt/it'\''s/personad' hook`: "/opt/it's/personad",
		"  /bin/x":                     "/bin/x",
		"":                             "",
		"'/unterminated hook":          "",
		// The node's directory in front (rootAssign) is not the program.
		"PERSONA_HOME=/h/.persona /usr/bin/personad hook":          "/usr/bin/personad",
		"PERSONA_HOME='/h/my dir' A=1 '/opt/my dir/personad' hook": "/opt/my dir/personad",
		"PERSONA_HOME=/h/.persona":                                 "",
		"'/opt/a=b/personad' hook":                                 "/opt/a=b/personad",
		"/opt/a=b/personad hook":                                   "/opt/a=b/personad",
		`PERSONA_HOME='/it'\''s' '/opt/it'\''s/personad' hook`:     "/opt/it's/personad",
	} {
		if got := FirstWord(in); got != want {
			t.Errorf("FirstWord(%q) = %q, want %q", in, got, want)
		}
	}
	// Whatever shellQuote writes reads back the same, with or without a directory in front.
	for _, p := range []string{"/a/b", "/a b/c", "/it's/x", "/q\"x/y", `/back\slash/x`, `/it\'s/x`} {
		if got := FirstWord(Quote(p) + " hook"); got != p {
			t.Errorf("round trip of %q: %q", p, got)
		}
		if got := FirstWord("PERSONA_HOME=" + Quote(p+"/.persona") + " " + Quote(p) + " hook"); got != p {
			t.Errorf("round trip of %q after its directory: %q", p, got)
		}
	}
}
