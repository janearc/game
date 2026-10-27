package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
)

// a patches line names a directory in the repository where the stack keeps
// members' patches beside patches/, in a named directory. more than one
// may be named, and they're kept in the order named, cleaned.
func TestPatchesNamesDirectoriesInOrder(t *testing.T) {
	var cfg config.Config
	src := "patches one/patches\npatches ./janearc/patches/\n"
	if err := cfg.Parse(strings.NewReader(src), ".game", "/h",
		false); err != nil {
		t.Fatal(err)
	}
	want := []string{"one/patches", "janearc/patches"}
	if !slices.Equal(cfg.Patches(), want) {
		t.Errorf("patches: %v, want %v", cfg.Patches(), want)
	}
}

// a patches line holds one directory inside the repository: nothing above
// it, nothing absolute, not two words, not empty.
func TestPatchesStaysInsideTheRepository(t *testing.T) {
	for _, src := range []string{
		"patches ../outside\n", "patches /abs/patches\n",
		"patches a/../../up\n", "patches one two\n", "patches\n",
	} {
		var cfg config.Config
		err := cfg.Parse(strings.NewReader(src), ".game", "/h", false)
		if err == nil || !strings.Contains(err.Error(), "patches") {
			t.Errorf("%q: %v, want it refused", src, err)
		}
		kept := cfg.Patches()
		if count.Positive(len(kept)) {
			t.Errorf("%q: kept %v", src, kept)
		}
	}
}

// patches/ is always read, so naming it is refused, and so is naming a
// directory twice: either would make one place look like two.
func TestPatchesRefusesTheDefaultAndRepeats(t *testing.T) {
	for _, src := range []string{
		"patches patches\n", "patches ./patches/\n",
		"patches shared/p\npatches shared/p/\n",
	} {
		var cfg config.Config
		err := cfg.Parse(strings.NewReader(src), ".game", "/h", false)
		if err == nil {
			t.Errorf("%q: taken as %v, want it refused", src, cfg.Patches())
		}
	}
}

// patches are a repository's, said in its .game; the dotfile can't say
// them.
func TestPatchesIsNotTheDotfiles(t *testing.T) {
	var cfg config.Config
	err := cfg.Parse(strings.NewReader("patches one/patches\n"),
		"config", "/h", true)
	if err == nil || !strings.Contains(err.Error(), ".game") {
		t.Errorf("the dotfile's patches line: %v, want it refused", err)
	}
}

// the review: the repository itself, bin, and a name that differs from
// another only by case are refused at the line. ".", "./" and "a/.." all
// clean to the repository; bin is where game puts what it builds and
// clones; on a disk blind to case, Patches is patches.
func TestPatchesRefusesTheRootBinAndCaseTwins(t *testing.T) {
	for _, src := range []string{
		"patches .\n", "patches ./\n", "patches a/..\n",
		"patches bin\n", "patches bin/stack\n", "patches BIN/x\n",
		"patches Patches\n", "patches PATCHES\n",
		"patches shared/p\npatches SHARED/P\n",
	} {
		var cfg config.Config
		err := cfg.Parse(strings.NewReader(src), ".game", "/h", false)
		if err == nil {
			t.Errorf("%q: taken as %v, want it refused", src, cfg.Patches())
		}
	}
}

// a patches directory inside a stack member's is refused once the whole
// .game is read, whichever line comes first: game lays members under
// their names, so it would read a member's own files as patches.
func TestPatchesInsideAMemberIsRefused(t *testing.T) {
	home := t.TempDir()
	for _, game := range []string{
		"stack a v1\npatches a/patches\n",
		"patches A/patches\nstack a v1\n",
	} {
		repo := t.TempDir()
		openFile := os.FileMode(0o644)
		if err := os.WriteFile(filepath.Join(repo, ".game"),
			[]byte(game), openFile); err != nil {
			t.Fatal(err)
		}
		_, err := config.Load(home, home, repo)
		if err == nil || !strings.Contains(err.Error(), "member a") {
			t.Errorf("%q: %v, want it refused naming member a", game,
				err)
		}
	}
	repo := t.TempDir()
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(filepath.Join(repo, ".game"),
		[]byte("stack a v1\npatches shared/patches\n"),
		openFile); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(home, home, repo); err != nil {
		t.Errorf("a place beside the members: %v", err)
	}
}
