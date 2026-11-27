package node

import (
	"cmp"
	"testing"
)

// The last lines a fresh shell's startup files leave on screen while they wait for a key, and prompts that do not.
func TestShellQuestion(t *testing.T) {
	for _, tc := range []struct {
		screen string
		asks   bool
	}{
		{"Last login: Mon Nov  9 22:01:13 on ttys004\n[oh-my-zsh] Would you like to update? [Y/n] ", true},
		{"Proceed with the upgrade? [y/N]: ", true},
		{"Continue (y/n)?", true},
		{"Overwrite the existing file? (yes/no) ", true},
		{"Press Enter to continue", true},
		{"press any key to go on…", true},
		// #10365: the questions no environment variable turns off, which read single keys
		{"zsh compinit: insecure directories, run compaudit for list.\nIgnore insecure directories and continue [y] or abort compinit [n]? ", true},
		{"ED25519 key fingerprint is SHA256:abc.\nAre you sure you want to continue connecting (yes/no/[fingerprint])? ", true},
		{"(0)  Exit, creating the file ~/.zshrc containing just a comment.\n\n--- Type one of the keys in parentheses --- ", true},
		{"[Oh My Bash] Would you like to check for updates? [Y/n]: ", true},
		{"  Does this look like a diamond (rotated square)?\n\n(q)  Quit and do nothing.\n\nChoice [ynq]: ", true},
		{"Enter passphrase for key '/home/me/.ssh/id_ed25519': ", true},
		{"[sudo] password for me: ", true},
		{"Password: ", true},
		{"Do you wish to trust '/p/.rvmrc'? y[es], n[o], v[iew], c[ancel]> ", true},
		{"[main] $ ", false},
		{"me@host ~/password-tools % ", false},
		{"choice of words: ", false},
		{"$ ", false},
		{"user@host:~/proj$ ", false},
		{"➜  proj git:(main) ", false},
		{"~/proj on main\n❯ ", false},
		{"user@host ~/proj> ", false},
		{"[oh-my-zsh] Would you like to update? [Y/n] n\n➜  proj ", false}, // answered: a prompt below it
		{"", false},
	} {
		if got := shellQuestion(tc.screen) != ""; got != tc.asks {
			t.Errorf("%q: asks %v, want %v", tc.screen, got, tc.asks)
		}
	}
}

// What the app offers for each startup question (start.continue): the answers, whether it reads a line, one key (no
// Enter after it, even echoed) or a secret, and what Enter alone answers. A secret prompt gets Ctrl-C alone and ssh's
// host key never a "yes".
func TestShellQuestionChoices(t *testing.T) {
	for _, tc := range []struct {
		line, kind, choices         string
		lineMode, singleKey, secret bool
		def                         string
	}{
		{"[oh-my-zsh] Would you like to update? [Y/n]", "update", "y,n,enter", false, true, false, "y"},
		{"[Oh My Bash] Would you like to check for updates? [Y/n]:", "update", "y,n,enter", true, false, false, "y"},
		{"Proceed with the upgrade? [y/N]:", "yes_no", "y,n,enter", true, false, false, "n"},
		{"Continue (y/n)?", "yes_no", "y,n,enter", false, false, false, ""},
		{"Overwrite the existing file? (yes/no)", "yes_no", "yes,no", true, false, false, ""},
		{"Press Enter to continue", "press_key", "enter", false, false, false, ""},
		{"Ignore insecure directories and continue [y] or abort compinit [n]?", "compinit", "y,n", false, true, false, ""},
		{"Are you sure you want to continue connecting (yes/no/[fingerprint])?", "ssh_host_key", "no,cancel", true, false, false, ""},
		{"--- Type one of the keys in parentheses ---", "zsh_newuser", "q,0", false, true, false, ""},
		{"Choice [ynq]:", "p10k_wizard", "q", false, true, false, ""},
		{"Do you wish to trust '/p/.rvmrc'? y[es], n[o], v[iew], c[ancel]>", "rvm_trust", "n,c", true, false, false, ""},
		{"Enter passphrase for key '/home/me/.ssh/id_ed25519':", "secret", "cancel", true, false, true, ""},
		{"[sudo] password for me:", "secret", "cancel", true, false, true, ""},
	} {
		if shellQuestion(tc.line) != tc.line {
			t.Errorf("%q is not a question", tc.line)
		}
		ask := questionChoices(tc.line)
		data := map[string]any{}
		ask.data(data)
		if ask.Kind != tc.kind || data["choices"] != tc.choices || ask.LineMode != tc.lineMode || ask.SingleKey != tc.singleKey ||
			ask.Secret != tc.secret || ask.Default != tc.def {
			t.Errorf("%q: %+v (%v)", tc.line, ask, data)
		}
		for _, c := range ask.Choices {
			for _, k := range c.Keys {
				if len([]rune(k)) != 1 && k != "enter" && k != "ctrl+c" {
					t.Errorf("%q: %s presses %q", tc.line, c.ID, k)
				}
			}
		}
	}
	if c, ok := questionChoices("Enter passphrase for key 'k':").choice("cancel"); !ok || len(c.Keys) != 1 || c.Keys[0] != "ctrl+c" {
		t.Errorf("a secret prompt's cancel: %+v", c)
	}
	if _, ok := questionChoices("Are you sure you want to continue connecting (yes/no/[fingerprint])?").choice("yes"); ok {
		t.Error("trusting a host key is offered from the app")
	}
}

// sent are the lines typed into pane, in order (a start file's line as typed, not its command).
func sent(f *fakeHerdr, pane string) []string {
	var out []string
	for _, c := range f.calls() {
		if c.Method == "pane.send_input" && str(c.Params["pane_id"]) == pane {
			out = append(out, cmp.Or(c.Typed, str(c.Params["text"])))
		}
	}
	return out
}
