package main

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/janearc/game/internal/repo"
)

// who says who made the release on dist and who the last three commits
// here were, each with the signature when the message ends in one and
// without it when it does not; and with no dist, says there is nothing
// to ask.
func TestWhoNamesTheReleaseAndTheLastThree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tree := gitTree(t, filepath.Join(root, "own"), map[string]string{
		"a.txt": "one\n",
	})
	gitAt(t, tree.Dir(), "commit", "-q", "--allow-empty", "-m",
		"two\n\njm/0badf00d")
	gitAt(t, tree.Dir(), "commit", "-q", "--allow-empty", "-m", "three")
	gitAt(t, tree.Dir(), "commit", "-q", "--allow-empty", "-m",
		"four\n\nsigned by nobody\n\nab/0123abcd")
	dist := filepath.Join(root, "dist.git")
	gitAt(t, root, "init", "-q", "--bare", dist)
	released := gitAt(t, tree.Dir(), "rev-parse", "HEAD~2")
	gitAt(t, tree.Dir(), "push", "-q", dist, released+":refs/heads/main")
	cfg := loadedAs(t, "x", block("x", "", dist), nil)
	var out bytes.Buffer
	if err := who(tree, cfg, &out); err != nil {
		t.Fatal(err)
	}
	said := out.String()
	// each line in full: hash, date, author, the signature if any, and
	// the subject, in that order and nothing else
	day := `\d{4}-\d{2}-\d{2}`
	lines := []string{
		`^released: ` + repo.Short(released) + ` ` + day + ` stack  jm/0badf00d  two$`,
		`^last three on main:$`,
		`^  [0-9a-f]{7} ` + day + ` stack  ab/0123abcd  four$`,
		`^  [0-9a-f]{7} ` + day + ` stack  unsigned  three$`,
		`^  [0-9a-f]{7} ` + day + ` stack  jm/0badf00d  two$`,
	}
	for _, want := range lines {
		if !regexp.MustCompile("(?m)" + want).MatchString(said) {
			t.Errorf("no line matches %s:\n%s", want, said)
		}
	}
	if strings.Count(said, "\n") != len(lines) {
		t.Errorf("want the release, the header and the three:\n%s", said)
	}
}

// with no dist, who says there is nothing to ask; a dist that is
// nowhere, and a repository with no commits, are errors.
func TestWhoWithoutADistSaysSo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tree := gitTree(t, filepath.Join(root, "own"), map[string]string{
		"a.txt": "one\n",
	})
	noDist := "released: no dist in your config"
	var out bytes.Buffer
	without := loadedAs(t, "x", nil, nil)
	if err := who(tree, without, &out); err != nil ||
		!strings.HasPrefix(out.String(), noDist) {
		t.Errorf("no block: %v\n%s", err, out.String())
	}
	noRemote := loadedAs(t, "x", block("x", "road.git", ""), nil)
	out.Reset()
	if err := who(tree, noRemote, &out); err != nil ||
		!strings.HasPrefix(out.String(), noDist) {
		t.Errorf("a block with no dist: %v\n%s", err, out.String())
	}
	gone := filepath.Join(root, "nowhere.git")
	nowhere := loadedAs(t, "x", block("x", "", gone), nil)
	if err := who(tree, nowhere, &bytes.Buffer{}); err == nil {
		t.Error("a dist that is nowhere was not an error")
	}
	empty := filepath.Join(root, "empty")
	gitAt(t, root, "init", "-q", "-b", "main", empty)
	if err := who(repoAt(empty), blankConfig(), &out); err == nil {
		t.Error("a repository with no commits was not an error")
	}
}

// who stands behind a commit: a signature git verified names the key's
// holder, whatever the message says; a session's line last in the
// message names the session; neither is the author, unsigned.
func TestVouchedIsTheKeyThenTheSessionThenNobody(t *testing.T) {
	t.Parallel()
	cases := []struct{ verified, holder, message, want string }{
		{"G", "Ada", "x\n\njm/0badf00d", "signed by Ada"},
		{"U", "Ada", "x", "signed by Ada"},
		{"N", "", "x\n\njm/0badf00d", "stack  jm/0badf00d"},
		{"N", "", "x\n\njm/0badf00d\n\nmore", "stack  unsigned"},
		{"B", "Ada", "x", "stack  unsigned"},
		{"N", "", "", "stack  unsigned"},
	}
	for _, each := range cases {
		got := vouched("stack", each.verified, each.holder, each.message)
		if got != each.want {
			t.Errorf("%q %q %q: got %q, want %q",
				each.verified, each.holder, each.message, got, each.want)
		}
	}
}
