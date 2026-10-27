package gittest_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/gittest"
)

// stagedWith is a memory repository whose scratch index "i" holds one
// blob under x/y.txt, set by the cacheinfo given, with the blob's hash.
func stagedWith(t *testing.T, info string) (*gittest.Mem, string, error) {
	t.Helper()
	mem, _ := seeded(t)
	blob, err := ask(mem, "new\n", nil, "hash-object", "-w", "--stdin")
	ok(t, err)
	blob = strings.TrimSpace(blob)
	_, err = ask(mem, "", []string{"GIT_INDEX_FILE=i"}, "update-index",
		"--add", "--cacheinfo", strings.ReplaceAll(info, "BLOB", blob))
	return mem, blob, err
}

// TestACacheinfoPutsTheBlobAndModeItNames: the hash is the second field
// and the mode is octal, as ls-tree shows them.
func TestACacheinfoPutsTheBlobAndModeItNames(t *testing.T) {
	t.Parallel()
	mem, blob, err := stagedWith(t, "100755,BLOB,x/y.txt")
	ok(t, err)
	tree, err := ask(mem, "", []string{"GIT_INDEX_FILE=i"}, "write-tree")
	ok(t, err)
	listed, err := ask(mem, "", nil, "ls-tree", "-r", "-z", strings.TrimSpace(tree))
	ok(t, err)
	if want := "100755 blob " + blob + "\tx/y.txt"; !strings.Contains(listed, want) {
		t.Errorf("listed %q, want %q", listed, want)
	}
}

// TestACacheinfoOfThreeFieldsOnly: the anti-test of the refusals: what
// is not mode, hash and path is refused by saying so, not by what a
// later read of it makes of it.
func TestACacheinfoOfThreeFieldsOnly(t *testing.T) {
	t.Parallel()
	for _, info := range []string{"100644,BLOB", "100644", ""} {
		_, _, err := stagedWith(t, info)
		if err == nil || !strings.Contains(err.Error(), `cacheinfo "`) {
			t.Errorf("cacheinfo %q: %v", info, err)
		}
	}
}

// TestACacheinfoModeIsOctalAndFitsThirtyTwoBits: a digit that is not
// octal is no mode, and neither is a number past thirty-two bits.
func TestACacheinfoModeIsOctalAndFitsThirtyTwoBits(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"100648", "100649", "40000000000", "zz"} {
		_, _, err := stagedWith(t, mode+",BLOB,x/y.txt")
		if err == nil {
			t.Errorf("mode %s was taken", mode)
		}
	}
	for _, mode := range []string{"100644", "37777777777"} {
		if _, _, err := stagedWith(t, mode+",BLOB,x/y.txt"); err != nil {
			t.Errorf("mode %s was refused: %v", mode, err)
		}
	}
}

// TestRmTakesWhatFollowsTheDashesAndNothingElse: a path before the
// dashes, or with no dashes, is not one to remove, and what follows
// them is.
func TestRmTakesWhatFollowsTheDashesAndNothingElse(t *testing.T) {
	t.Parallel()
	for args, want := range map[string]string{
		"rm -r --cached docs":          "a.txt docs/b.txt",
		"rm -r --cached docs -- a.txt": "docs/b.txt",
		"rm -r --cached -- docs":       "a.txt",
	} {
		mem, _ := seeded(t)
		env := []string{"GIT_INDEX_FILE=i"}
		_, err := ask(mem, "", env, "read-tree", "HEAD")
		ok(t, err)
		_, err = ask(mem, "", env, strings.Fields(args)...)
		ok(t, err)
		tree, err := ask(mem, "", env, "write-tree")
		ok(t, err)
		got, err := mem.Names(strings.TrimSpace(tree))
		ok(t, err)
		if strings.Join(got, " ") != want {
			t.Errorf("%s left %v, want %s", args, got, want)
		}
	}
}

// TestAbbrevRefNeedsExactlyItsRevision: --abbrev-ref with a revision is
// the branch; with none, or with more, it is a revision that is not
// there.
func TestAbbrevRefNeedsExactlyItsRevision(t *testing.T) {
	t.Parallel()
	mem, head := seeded(t)
	if got, err := ask(mem, "", nil, "rev-parse", "--abbrev-ref", "HEAD"); err != nil || got != "main" {
		t.Errorf("--abbrev-ref HEAD: %q, %v", got, err)
	}
	if got, err := ask(mem, "", nil, "rev-parse", "--abbrev-ref"); err == nil {
		t.Errorf("--abbrev-ref alone gave %q", got)
	}
	if got, err := ask(mem, "", nil, "rev-parse", "--abbrev-ref", "HEAD", "HEAD"); err == nil {
		t.Errorf("--abbrev-ref with two gave %q", got)
	}
	if got, err := ask(mem, "", nil, "rev-parse", "HEAD", "HEAD"); err != nil || strings.TrimSpace(got) != head {
		t.Errorf("two revisions: %q, %v; want the first, %s", got, err, head)
	}
}

// TestARefusalSaysWhatWasWrong: the words of a refusal are git's.
func TestARefusalSaysWhatWasWrong(t *testing.T) {
	t.Parallel()
	mem, _ := seeded(t)
	if _, err := ask(mem, "", nil); err == nil || !strings.Contains(err.Error(), "no command") {
		t.Errorf("no command: %v", err)
	}
	if _, err := ask(mem, "", nil, "ls-remote", "none"); err == nil ||
		!strings.Contains(err.Error(), `"none" is not a git repository`) {
		t.Errorf("a remote that is not there: %v", err)
	}
}

// TestShowGivesThreeLinesOfContextEachSide: without -U, three, and no
// more.
func TestShowGivesThreeLinesOfContextEachSide(t *testing.T) {
	t.Parallel()
	mem, err := gittest.New()
	ok(t, err)
	var body []string
	for line := count.One; line <= fileLines; line++ {
		body = append(body, fmt.Sprintf("line%02d", line))
	}
	commit(t, mem, map[string]string{"f.txt": strings.Join(body, "\n") + "\n"})
	body[changedLine] = "changed"
	second := commit(t, mem, map[string]string{"f.txt": strings.Join(body, "\n") + "\n"})
	got, err := ask(mem, "", nil, "show", "--format=", second)
	ok(t, err)
	for _, kept := range []string{" line07", " line08", " line09", " line11", " line12", " line13"} {
		if !strings.Contains(got, kept+"\n") {
			t.Errorf("context %q is missing from %q", kept, got)
		}
	}
	for _, left := range []string{"\n line06\n", "\n line14\n"} {
		if strings.Contains(got, left) {
			t.Errorf("context past three lines: %q in %q", left, got)
		}
	}
}

// the file the context test changes: how long it is, and which line
// (counting from none) is changed.
const (
	fileLines   = 20 // defined here: lines in the fixture file
	changedLine = 9  // defined here: the tenth line, counting from none
)
