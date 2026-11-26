package doctor

import (
	"slices"
	"testing"
)

// A shell-form hook's program is its command line's first word; an exec-form one's ("args" given) is its command as it
// is, spaces included.
func TestConfigCommandsBothForms(t *testing.T) {
	t.Parallel()
	b := []byte(`{"hooks": {
		"Stop": [{"hooks": [{"type": "command", "command": "'/opt/my dir/personad' hook"}]}],
		"MessageDisplay": [{"hooks": [{"type": "command", "command": "/opt/my dir/persona-node", "args": ["hook"]}]}],
		"Notification": [{"hooks": [{"type": "command", "command": "/opt/no args", "args": []}]}]
	}}`)
	got := configCommands(b)
	slices.Sort(got)
	if want := []string{"/opt/my dir/persona-node", "/opt/my dir/personad", "/opt/no args"}; !slices.Equal(got, want) {
		t.Fatalf("configCommands = %q, want %q", got, want)
	}
}
