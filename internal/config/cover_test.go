package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
)

// folded reads a dotfile and then a .game into one config, as Load does,
// and gives the error of whichever refused.
func folded(dotfile, game string) (config.Config, error) {
	var cfg config.Config
	err := cfg.Parse(strings.NewReader(dotfile), "the.dotfile", "/h", true)
	if err != nil {
		return cfg, err
	}
	err = cfg.Parse(strings.NewReader(game), "the.game", "/h", false)
	return cfg, err
}

// the floor is the dotfile's N, or a .game's M, or the higher of both.
func TestCoverFloorIsTheHigherOfBoth(t *testing.T) {
	t.Parallel()
	for _, each := range []struct {
		dotfile, game string
		floor         int
	}{
		{"", "", 0},
		{"cover 80\n", "", 80},
		{"", "cover 90\n", 90},
		{"cover 80\n", "cover 90\n", 90},
		{"cover 80\n", "cover 80\n", 80},
		{"cover 1\n", "name x\n", 1},
		{"", "cover 100\n", 100},
	} {
		cfg, err := folded(each.dotfile, each.game)
		if err != nil || cfg.CoverFloor() != each.floor {
			t.Errorf("%q then %q: floor %d, err %v, want %d",
				each.dotfile, each.game, cfg.CoverFloor(), err,
				each.floor)
		}
	}
}

// anti-test: a project that lowers the floor is refused, and the refusal
// names both numbers and both files.
func TestCoverBelowTheFloorIsRefused(t *testing.T) {
	t.Parallel()
	_, err := folded("author x\ncover 80\n", "name x\ncover 70\n")
	if err == nil {
		t.Fatal("a .game lowered the floor")
	}
	for _, want := range []string{"the.game:2", "the.dotfile:2", "70",
		"80"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q is not in %q", want, err)
		}
	}
}

// anti-test: a cover that is not a number from 1 to 100 is refused, at
// its line.
func TestCoverRefusesWhatIsNotAPercentage(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"cover 0", "cover 101", "cover -5",
		"cover ten", "cover 80 90", "cover 8.5", "cover 80%"} {
		for _, dotted := range []bool{true, false} {
			var cfg config.Config
			err := cfg.Parse(strings.NewReader("author x\n"+bad+"\n"),
				"f", "/h", dotted)
			if err == nil || !strings.Contains(err.Error(), "f:2") ||
				!strings.Contains(err.Error(), "cover wants N") {
				t.Errorf("%q: %v", bad, err)
			}
		}
	}
}

// a cover allow is a file and its reason, as many words as the reason
// takes.
func TestCoverAllowKeepsTheReason(t *testing.T) {
	t.Parallel()
	cfg, err := folded("", "cover allow a/b.go needs a real terminal\n"+
		"cover allow c.go  tested by hand \n")
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.CoverAllowed()
	if len(got) != 2 || got["a/b.go"] != "needs a real terminal" ||
		got["c.go"] != "tested by hand" {
		t.Errorf("allowed: %v", got)
	}
	if none, _ := folded("cover 90\n", "name x\n"); len(none.CoverAllowed()) != 0 {
		t.Errorf("nothing allowed: %v", none.CoverAllowed())
	}
}

// anti-test: an allow with no reason, or no file, is refused; and an allow
// is a project's, so the builder's config refuses it.
func TestCoverAllowWithoutAReasonIsRefused(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"cover allow a/b.go", "cover allow",
		"cover allow a/b.go   "} {
		var cfg config.Config
		err := cfg.Parse(strings.NewReader(bad+"\n"), "the.game", "/h",
			false)
		if err == nil || !strings.Contains(err.Error(), "the.game:1") ||
			!strings.Contains(err.Error(), "cover allow wants FILE REASON") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	var cfg config.Config
	err := cfg.Parse(strings.NewReader("cover allow a.go why\n"),
		"the.dotfile", "/h", true)
	if err == nil || !strings.Contains(err.Error(), "a project's") {
		t.Errorf("an allow in the builder's config: %v", err)
	}
}

// Load names the dotfile's path and the .game's in the refusal.
func TestLoadRefusesALoweredFloorNamingBothFiles(t *testing.T) {
	t.Parallel()
	home, dir := t.TempDir(), t.TempDir()
	mode := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Join(home, ".config", "game"),
		mode); err != nil {
		t.Fatal(err)
	}
	dotfile := filepath.Join(home, ".config", "game", "config")
	file := os.FileMode(0o644)
	if err := os.WriteFile(dotfile, []byte("cover 95\n"), file); err != nil {
		t.Fatal(err)
	}
	game := filepath.Join(dir, ".game")
	if err := os.WriteFile(game, []byte("cover 50\n"), file); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(home, home, dir)
	if err == nil || !strings.Contains(err.Error(), dotfile+":1") ||
		!strings.Contains(err.Error(), game+":1") {
		t.Fatalf("a lowered floor in Load: %v", err)
	}
	if err := os.WriteFile(game, []byte("cover 96\n"), file); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(home, home, dir)
	if err != nil || cfg.CoverFloor() != 96 {
		t.Errorf("a raised floor: %d %v", cfg.CoverFloor(), err)
	}
}
