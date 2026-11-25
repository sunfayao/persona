package identity

import (
	"path/filepath"
	"testing"
)

// A conversation's hook or card can carry a subfolder of its directory: Claude Code reports the shell's directory
// after the model's `cd` (#2368). SpaceAt counts those as the space; a path part named like a track elsewhere is not
// one. SpaceOf stays exact.
func TestSpaceAt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERSONA_HOME", "")
	in := func(parts ...string) string { return filepath.Join(append([]string{home}, parts...)...) }
	for p, want := range map[string]string{
		in(".persona", "tracks", "qa-plain"):                              "qa-plain",
		in(".persona", "tracks", "qa-plain", "inbox"):                     "qa-plain",
		in(".persona", "tracks", "qa-plain", "inbox", "2026"):             "qa-plain",
		in(".persona", "tracks-m-0b0b0b", "qa-plain", "x"):                "qa-plain",
		in(".persona", "assistant", ".claude", "skills", "qa-plain"):      "",
		in(".persona", "tracks-bin", "qa-plain", "x"):                     "",
		filepath.Join(string(filepath.Separator), "tmp", "qa-plain", "x"): "",
		in(".persona", "assistant", "x"):                                  "",
		in(".persona"):                                                    "",
		"":                                                                "",
		".":                                                               "",
	} {
		if got := SpaceAt(p); got != want {
			t.Errorf("SpaceAt(%q) = %q, want %q", p, got, want)
		}
	}
	// Classification stays exact: a subfolder is not the space's directory.
	if SpaceOf(in(".persona", "tracks", "qa-plain", "inbox")) != "" || IsAssistantDirOf(in(".persona", "tracks", "qa-plain", "inbox"), "") {
		t.Error("SpaceOf and IsAssistantDirOf take only the directory itself")
	}
}

// A card is placed in its conversation's directory, unless the hook's is inside a space the conversation's is not: a
// mix-up can only hide more.
func TestCardCwd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERSONA_HOME", "")
	assistant, space := filepath.Join(home, ".persona", "assistant"), filepath.Join(home, ".persona", "tracks", "qa-plain")
	tmp := filepath.Join(string(filepath.Separator), "tmp")
	for _, c := range []struct{ home, here, want string }{
		{"", filepath.Join(space, "x"), filepath.Join(space, "x")},
		{assistant, filepath.Join(space, "x"), filepath.Join(space, "x")},
		{space, tmp, space},
		{space, filepath.Join(space, "inbox"), space},
		{space, assistant, space},
		{assistant, tmp, assistant},
		{assistant, filepath.Join(assistant, ".claude", "skills", "x"), assistant},
		{assistant, "", assistant},
	} {
		if got := CardCwd(c.home, c.here); got != c.want {
			t.Errorf("CardCwd(%q, %q) = %q, want %q", c.home, c.here, got, c.want)
		}
	}
}
