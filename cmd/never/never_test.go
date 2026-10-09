package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a home with a never-words list of made-up words, and a game directory
// with the place the hashes go.
func setup(t *testing.T, list string) (home, game string) {
	t.Helper()
	home, game = t.TempDir(), t.TempDir()
	at := filepath.Join(home, ".config", "game")
	if err := os.MkdirAll(at, 0o700); err != nil {
		t.Fatal(err)
	}
	if list != "" {
		if err := os.WriteFile(filepath.Join(at, "sweep.words"),
			[]byte(list), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(game, "internal", "sweep",
		"never"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home, game
}

func hashesIn(game string) string {
	body, _ := os.ReadFile(filepath.Join(game, "internal", "sweep", "never",
		"hashes"))
	return string(body)
}

// the list is baked into hashes that hold none of its words, with a salt
// made once and kept beside the list, so baking again is the same bytes.
func TestBakeTheList(t *testing.T) {
	home, game := setup(t, "# made up\nzorblax\n\nquinta veltrane\n")
	said, err := bake(home, home, game)
	if err != nil {
		t.Fatal(err)
	}
	first := hashesIn(game)
	if !strings.Contains(said, "2 never-words") ||
		strings.Contains(first, "zorblax") ||
		strings.Contains(first, "veltrane") {
		t.Errorf("said %q and baked:\n%s", said, first)
	}
	salt, err := os.Stat(filepath.Join(home, ".config", "game", "sweep.salt"))
	if err != nil || salt.Mode().Perm() != 0o600 {
		t.Errorf("the salt is not kept beside the list, for its owner: %v",
			err)
	}
	if _, err := bake(home, home, game); err != nil ||
		hashesIn(game) != first {
		t.Error("baking the same list again changed the hashes")
	}
}

// with no list, the hashes from before are taken away and it says so:
// that game builds, and refuses to release.
func TestNoListBakesNothing(t *testing.T) {
	home, game := setup(t, "zorblax\n")
	if _, err := bake(home, home, game); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(home, ".config", "game", "sweep.words"))
	said, err := bake(home, home, game)
	if err != nil {
		t.Fatal(err)
	}
	if hashesIn(game) != "" || !strings.Contains(said, "refuse to release") {
		t.Errorf("with no list it said %q and left hashes behind", said)
	}
}

// outside game's directory there is nowhere to bake into, and it says
// where to stand.
func TestBakeOutsideGame(t *testing.T) {
	home, _ := setup(t, "zorblax\n")
	_, err := bake(home, home, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "game's directory") {
		t.Errorf("baking outside game was not refused: %v", err)
	}
}
