// Command never bakes game's never-words in. It reads the list that
// game's config names, ~/.config/game/sweep.words unless the config's
// words line says otherwise.
//
// It writes internal/sweep/never/hashes: a salt, then the salted sha-256
// of each word or name, one to a line. go embeds that file when game is
// built, so the binary carries hashes and never the words.
//
//	go run ./cmd/never    from game's directory; bootstrap.sh runs it
//
// The salt is made once and kept beside the list, in sweep.salt, so the
// same list makes the same hashes and the same sources still build the
// same binary.
//
// With no list, or an empty one, it removes any hashes left from before
// and says so. The game built then does everything but release, and
// says why when asked to.
//
// An entry longer than three words is refused by its place in the list,
// never by its words, which are not printed anywhere.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/sweep"
)

// main bakes the never-words into the game directory it is run from.
func main() {
	said, err := bake(config.Whose(), os.Getenv("HOME"), ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "never:", err)
		os.Exit(1)
	}
	fmt.Println("never:", said)
}

// bake writes the hashes for the list game's config names, read as game
// reads it, into the game directory at dir, and says what it did.
func bake(whose, home, dir string) (string, error) {
	cfg, err := config.Load(whose, home, dir)
	if err != nil {
		return "", err
	}
	at := filepath.Join(dir, "internal", "sweep", "never")
	if _, err := os.Stat(at); err != nil {
		return "", fmt.Errorf("%s is not here; run never from game's "+
			"directory", at)
	}
	out := filepath.Join(at, "hashes")
	list := sweep.Words(cfg.Words)
	if len(list) == 0 {
		if err := os.Remove(out); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		return fmt.Sprintf("no never-words at %s, so none are "+
			"baked in; this game will build, and refuse to "+
			"release", cfg.Words), nil
	}
	salt, err := saltBeside(cfg.Words)
	if err != nil {
		return "", err
	}
	body, err := sweep.Bake(salt, list)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(out, []byte(body), 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d never-words baked in, as hashes", len(list)), nil
}

// saltBeside is the salt kept beside the list, made the first time it is
// asked for: sixteen random bytes, written where only this user can read
// them.
func saltBeside(list string) (string, error) {
	path := filepath.Join(filepath.Dir(list), "sweep.salt")
	kept, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(kept)) != "" {
		return strings.TrimSpace(string(kept)), nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	fresh := make([]byte, 16)
	if _, err := rand.Read(fresh); err != nil {
		return "", err
	}
	salt := hex.EncodeToString(fresh)
	if err := os.WriteFile(path, []byte(salt+"\n"), 0o600); err != nil {
		return "", err
	}
	return salt, nil
}
