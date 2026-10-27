package patch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/patch"
)

// a stack whose directory has a glob's characters in its name still
// finds its member's patches, and only those: the name is a place, not
// a pattern.
func TestARootNamedLikeAPatternFindsItsOwnPatches(t *testing.T) {
	for _, name := range []string{"a[1]", "b[", "c*", "d?"} {
		holder := t.TempDir()
		root := filepath.Join(holder, name)
		writeIn(t, filepath.Join(root, "patches", "member"), "01.patch", first)
		// what the root's name would match as a pattern
		plain := filepath.Join(holder, "a1")
		writeIn(t, filepath.Join(plain, "patches", "member"), "99.patch", first)
		series, err := patch.Series(root, nil, "member")
		if err != nil || len(series) != count.One ||
			filepath.Base(series[count.First]) != "01.patch" ||
			!filepath.IsAbs(series[count.First]) {
			t.Errorf("%s: series %v, err %v", name, series, err)
		}
	}
}

// only files whose names end .patch are in a series: a directory so
// named is not one, nor is a note beside them.
func TestASeriesIsThePatchFilesAlone(t *testing.T) {
	_, root := member(t, map[string]string{"01.patch": first,
		"README": "notes\n", "02.patch/f": first})
	series, err := patch.Series(root, nil, "member")
	if err != nil || len(series) != count.One ||
		filepath.Base(series[count.First]) != "01.patch" {
		t.Errorf("series %v, err %v", series, err)
	}
}

// no patches is nothing to do anywhere, even where there is no tree to
// lay them on.
func TestNoPatchesNeedNoTree(t *testing.T) {
	results, err := patch.Lay(t.TempDir(), nil, false)
	if err != nil || results != nil {
		t.Errorf("results %v, err %v", results, err)
	}
}

// a directory that is no git tree is said to be one, not some other
// trouble found after.
func TestADirectoryThatIsNoTreeIsSaidSo(t *testing.T) {
	_, root := member(t, map[string]string{"01.patch": first})
	series, err := patch.Series(root, nil, "member")
	if err != nil {
		t.Fatal(err)
	}
	_, err = patch.Lay(t.TempDir(), series, false)
	if err == nil || !strings.Contains(err.Error(), "is not a git tree") {
		t.Errorf("err %v, want it said to be no git tree", err)
	}
}

// a tree whose index git cannot read is refused with git's words, not
// called a tree with changes.
func TestATreeGitCannotReadIsRefusedWithGitsWords(t *testing.T) {
	dir, root := member(t, map[string]string{"01.patch": first})
	write(t, filepath.Join(dir, ".git", "index"), "not an index\n")
	_, err := lay(t, dir, root, false)
	if err == nil || strings.Contains(err.Error(), "has changes") ||
		!strings.Contains(err.Error(), "index") {
		t.Errorf("err %v, want git's words about its index", err)
	}
}

// a reset that git refuses after a patch fails is said, since the clone
// may be left half laid.
func TestAResetThatFailsIsSaid(t *testing.T) {
	dir, root := member(t, map[string]string{"01.patch": wrong})
	lock := filepath.Join(dir, ".git", "index.lock")
	write(t, lock, "")
	defer os.Remove(lock)
	_, err := lay(t, dir, root, false)
	if err == nil || !strings.Contains(err.Error(), "resetting") {
		t.Errorf("err %v, want the failed reset said", err)
	}
}

// a member's patches that are a file, not a directory, are said to be
// wrong, not taken as none.
func TestPatchesThatAreAFileAreRefused(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "patches", "member"), first)
	series, err := patch.Series(root, nil, "member")
	if err == nil {
		t.Errorf("series %v from a file, want it refused", series)
	}
}
