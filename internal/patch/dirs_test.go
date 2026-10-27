package patch_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/patch"
)

// a stack that can't put patches in a member's own repository keeps them
// beside it, under a directory .game names; Series reads them from there,
// in name order, as it reads patches/NAME.
func TestSeriesReadsANamedDirectory(t *testing.T) {
	root := t.TempDir()
	held := filepath.Join(root, "charmbracelet", "patches", "member")
	writeIn(t, held, "0002-b.patch", "b")
	writeIn(t, held, "0001-a.patch", "a")
	writeIn(t, held, "0001-a.md", "what it's for")

	series, err := patch.Series(root, []string{"charmbracelet/patches"},
		"member")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(held, "0001-a.patch"),
		filepath.Join(held, "0002-b.patch")}
	if !slices.Equal(series, want) {
		t.Errorf("series: %v, want %v", series, want)
	}
}

// with a directory named, patches/NAME is still read for a member that
// keeps its patches there, and a member in neither has none.
func TestSeriesStillReadsPatchesDirectory(t *testing.T) {
	root := t.TempDir()
	writeIn(t, filepath.Join(root, "patches", "ours"), "01.patch", "x")
	dirs := []string{"charmbracelet/patches"}

	series, err := patch.Series(root, dirs, "ours")
	if err != nil || len(series) != count.One {
		t.Errorf("ours: %v, %v; want patches/ours read", series, err)
	}
	series, err = patch.Series(root, dirs, "nobody")
	if err != nil || count.Positive(len(series)) {
		t.Errorf("nobody: %v, %v; want no patches", series, err)
	}
}

// a member whose patches are in two places is refused, naming both, since
// laid in name order across them, which came first would be a guess.
func TestSeriesRefusesTwoPlaces(t *testing.T) {
	root := t.TempDir()
	writeIn(t, filepath.Join(root, "patches", "member"), "01.patch", "x")
	writeIn(t, filepath.Join(root, "charm", "patches", "member"),
		"01.patch", "y")

	_, err := patch.Series(root, []string{"charm/patches"}, "member")
	if err == nil {
		t.Fatal("two places taken as one")
	}
	// named from the stack's root, as game release stack names them
	for _, want := range []string{"both patches/member and " +
		"charm/patches/member"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't say %q", err, want)
		}
	}
}

// an empty directory isn't a place: patches/NAME holding only notes,
// beside a named directory with the patches, is no conflict, as the
// release's own count in the commit, which holds no empty directories,
// would agree.
func TestSeriesIgnoresAPlaceWithNoPatches(t *testing.T) {
	root := t.TempDir()
	writeIn(t, filepath.Join(root, "patches", "member"), "notes.md", "n")
	held := filepath.Join(root, "charm", "patches", "member")
	writeIn(t, held, "01.patch", "y")

	series, err := patch.Series(root, []string{"charm/patches"}, "member")
	want := []string{filepath.Join(held, "01.patch")}
	if err != nil || !slices.Equal(series, want) {
		t.Errorf("series = %v, %v; want %v", series, err, want)
	}
}

// a place that can't be read is an error, not a place with no patches: a
// file where the directory should be is a stack laid out wrong, and
// carrying on would read the default and lay nothing.
func TestSeriesSaysWhenAPlaceCantBeRead(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "patches", "member"), "a file, not a directory")
	if _, err := patch.Series(root, nil, "member"); err == nil {
		t.Error("a file where patches/member should be was read as empty")
	}
}

// the same under a named directory: a file where charm/patches/member
// should be is an error, not a member with no patches beside the stack.
func TestSeriesSaysWhenANamedPlaceCantBeRead(t *testing.T) {
	root := t.TempDir()
	writeIn(t, filepath.Join(root, "charm", "patches"), "member", "a file")
	_, err := patch.Series(root, []string{"charm/patches"}, "member")
	if err == nil {
		t.Error("a file where charm/patches/member should be was read " +
			"as no patches")
	}
}

// Every place is looked at. One that cannot be read is an error even when
// another place holds the patches.
func TestSeriesLooksAtEveryPlaceEvenAfterFindingPatches(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "patches", "member", "0001-a.patch"), "x\n")
	write(t, filepath.Join(root, "charm", "member"), "a file, not a directory")
	series, err := patch.Series(root, []string{"charm"}, "member")
	if err == nil || series != nil {
		t.Errorf("series = %v, %v; want an error", series, err)
	}
}

// Patches lying in the stack's root itself belong to no member. The places
// are patches/NAME and the ones .game names. The root is none of them.
func TestSeriesDoesNotReadTheRootAsAPlace(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "stray.patch"), "x\n")
	write(t, filepath.Join(root, "patches", "member", "0001-a.patch"), "x\n")
	series, err := patch.Series(root, nil, "member")
	want := []string{filepath.Join(root, "patches", "member", "0001-a.patch")}
	if err != nil || !slices.Equal(series, want) {
		t.Errorf("series = %v, %v; want %v", series, err, want)
	}
}
