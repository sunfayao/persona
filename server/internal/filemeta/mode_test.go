package filemeta

import (
	"os"
	"testing"
)

func TestUnixModeKeepsSpecialBits(t *testing.T) {
	t.Parallel()
	if m := UnixMode(os.ModeSetuid | os.ModeSticky | 0o755); m != 0o5755 {
		t.Fatalf("mode %o, want 5755", m)
	}
	if m := UnixMode(os.ModeSetgid | 0o640); m != 0o2640 {
		t.Fatalf("mode %o, want 2640", m)
	}
}
