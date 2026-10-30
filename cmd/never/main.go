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
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/sweep"
)

// build and built are the commit and its time, stamped in when game
// builds never into bin/. go run, as bootstrap.sh does it, leaves them
// as they are here.
var (
	build       = "dev"
	built       = ""
	stampedWho  = ""
	stampedSids = ""
)

// main bakes the never-words into the game directory it is run from, or
// answers --age.
func main() {
	args := os.Args[count.Next(count.First):]
	code := runNeverPzm(args, config.Whose(), os.Getenv("HOME"), ".",
		os.Stdin, os.Stdout, os.Stderr)
	exit(code)
}

// never's exit codes, as the shell has defined them: success, and the
// list could not be baked.
const (
	exitOK     = 0 // defined by the shell: success
	exitFailed = 1 // defined by convention: the bake failed
)

// run is never given its arguments, whose config to read, the home it
// is in, the game directory, and where to say things. it is the exit
// code: failed when the list could not be baked.
func run(args []string, whose, home, dir string, out, errs io.Writer) int {
	if answer(args, out) {
		return exitOK
	}
	said, err := bake(whose, home, dir)
	if err != nil {
		fmt.Fprintln(errs, "never:", err)
		return exitFailed
	}
	fmt.Fprintln(out, "never:", said)
	return exitOK
}

// bake writes the never-word hashes into the game directory and says what
// it did.
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
	list, err := sweep.Words(cfg.Words())
	if err != nil {
		return "", err
	}
	if count.None(len(list)) {
		if err := os.Remove(out); err != nil &&
			!errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		return fmt.Sprintf("no never-words at %s, so none are "+
			"baked in; this game will build, and refuse to "+
			"release", cfg.Words()), nil
	}
	salt, err := saltBeside(cfg.Words(), rand.Read)
	if err != nil {
		return "", err
	}
	body, err := sweep.Bake(salt, list)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(out, []byte(body), ownerOnly); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d never-words baked in, as hashes", len(list)), nil
}

// ownerOnly is the mode the hashes and the salt are written with: the
// owner alone reads and writes them, defined as 0600 in sys/stat.h's
// bits.
const ownerOnly = 0o600

// saltBytes is how long a fresh salt is: sixteen random bytes, defined
// here as more than anyone will guess.
const saltBytes = 16

// exit ends the process after a bake or an answer.
//
//ninja:vow decoy a test cannot let os.Exit end its own process
var exit = os.Exit

// saltBeside gives the salt kept beside the list, making it on first use.
// it is sixteen random bytes, from randomRead, readable only by this user.
func saltBeside(
	list string,
	randomRead func([]byte) (int, error),
) (string, error) {
	path := filepath.Join(filepath.Dir(list), "sweep.salt")
	kept, err := os.ReadFile(path)
	if err == nil && strings.TrimSpace(string(kept)) != "" {
		return strings.TrimSpace(string(kept)), nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	fresh := make([]byte, saltBytes)
	if _, err := randomRead(fresh); err != nil {
		return "", err
	}
	salt := hex.EncodeToString(fresh)
	if err := os.WriteFile(path, []byte(salt+"\n"), ownerOnly); err != nil {
		return "", err
	}
	return salt, nil
}

// answer says this never's age, if that is what it was asked, and says
// whether it was.
func answer(args []string, out io.Writer) bool {
	// the first word is never itself; the flag comes after it
	after := args[count.Next(count.First):]
	if count.None(len(after)) || after[count.First] != "--age" {
		return false
	}
	said := age(build, built, now())
	fmt.Fprintln(out, said)
	return true
}

// age says which commit never was built from and how old that commit is,
// in the words game uses for its own.
func age(commit, at string, now time.Time) string {
	if at == "" && commit == "dev" {
		return "never, built from a tree by go rather than by game: " +
			"no commit and no commit time in it"
	}
	if at == "" {
		return fmt.Sprintf("never %s, with no commit time in it",
			commit)
	}
	made, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return fmt.Sprintf("never %s, committed at %s", commit, at)
	}
	return fmt.Sprintf("never %s, committed %s ago", commit,
		now.Sub(made).Round(time.Second))
}

// now is the clock age reads.
func now() time.Time {
	return time.Now()
}
