package node

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// git.diff and git.show stop reading git's output at maxDiffBytes: a huge change costs about the cap in memory, not the
// whole diff, and a cut diff ends on a whole line, so it is valid UTF-8 and has no half line for the viewers to draw.

// cjkLines is n numbered lines of CJK text, about 80 bytes each.
func cjkLines(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "%06d %s\n", i, strings.Repeat("漢字かな", 6))
	}
	return b.String()
}

func checkCutDiff(t *testing.T, what, diff string, truncated bool) {
	t.Helper()
	if !truncated {
		t.Errorf("%s: truncated = false for a diff over the cap", what)
	}
	if len(diff) > maxDiffBytes || len(diff) < maxDiffBytes-1024 {
		t.Errorf("%s: %d bytes, want just under %d", what, len(diff), maxDiffBytes)
	}
	if !utf8.ValidString(diff) {
		t.Errorf("%s: cut diff is not valid UTF-8 (ends %q)", what, diff[max(len(diff)-16, 0):])
	}
	if !strings.HasSuffix(diff, "\n") {
		t.Errorf("%s: cut diff ends mid-line: %q", what, diff[max(len(diff)-40, 0):])
	}
}

func TestGitDiffCapEndsOnWholeLine(t *testing.T) {
	r := newReviewRepo(t, map[string]string{"notes.txt": "start\n"})
	ctx := context.Background()
	big := cjkLines(12000) // about 1 MB

	r.write("notes.txt", big)
	d, err := r.node.gitDiff(ctx, r.dir, "notes.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	checkCutDiff(t, "unstaged", d["diff"].(string), d["truncated"].(bool))

	r.git("add", "notes.txt")
	d, err = r.node.gitDiff(ctx, r.dir, "notes.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	checkCutDiff(t, "staged", d["diff"].(string), d["truncated"].(bool))

	r.write("new.txt", big) // untracked: diff --no-index against /dev/null
	d, err = r.node.gitDiff(ctx, r.dir, "new.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	checkCutDiff(t, "untracked", d["diff"].(string), d["truncated"].(bool))

	r.git("commit", "-q", "-m", "Grow notes")
	s, err := r.node.gitShow(ctx, r.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	checkCutDiff(t, "show", s.Diff, s.Truncated)
}

// A diff under the cap is git's output, byte for byte.
func TestGitDiffCapSmallDiffUnchanged(t *testing.T) {
	r := newReviewRepo(t, map[string]string{"a.txt": "one\ntwo\n"})
	ctx := context.Background()
	r.write("a.txt", "one\n二\nthree") // no newline at the end
	want, err := git(ctx, r.dir, "--literal-pathspecs", "diff", "--no-color", "--", "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.node.gitDiff(ctx, r.dir, "a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if d["diff"] != want || d["truncated"] != false {
		t.Errorf("git.diff = %q (truncated %v), want %q", d["diff"], d["truncated"], want)
	}

	r.git("commit", "-q", "-am", "Edit a")
	want, err = git(ctx, r.dir, "show", "-M", "--no-color", "--format=", "HEAD", "--")
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.node.gitShow(ctx, r.dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if s.Diff != want || s.Truncated {
		t.Errorf("git.show = %q (truncated %v), want %q", s.Diff, s.Truncated, want)
	}
}
