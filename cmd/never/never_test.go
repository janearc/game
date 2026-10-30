package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
)

// a home with a never-words list of made-up words, and a game directory
// with the place the hashes go.
func setup(t *testing.T, list string) (home, game string) {
	t.Helper()
	home, game = t.TempDir(), t.TempDir()
	at := filepath.Join(home, ".config", "game")
	shutHome := os.FileMode(0o700)
	if err := os.MkdirAll(at, shutHome); err != nil {
		t.Fatal(err)
	}
	if list != "" {
		shutFile := os.FileMode(0o600)
		words := filepath.Join(at, "sweep.words")
		if err := os.WriteFile(words, []byte(list), shutFile); err != nil {
			t.Fatal(err)
		}
	}
	openDir := os.FileMode(0o755)
	never := filepath.Join(game, "internal", "sweep", "never")
	if err := os.MkdirAll(never, openDir); err != nil {
		t.Fatal(err)
	}
	return home, game
}

func hashesIn(game string) string {
	body, _ := os.ReadFile(filepath.Join(game, "internal", "sweep",
		"never", "hashes"))
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
	kept := filepath.Join(home, ".config", "game", "sweep.salt")
	salt, err := os.Stat(kept)
	shutFile := os.FileMode(0o600)
	if err != nil || salt.Mode().Perm() != shutFile {
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
	words := filepath.Join(home, ".config", "game", "sweep.words")
	if err := os.Remove(words); err != nil {
		t.Fatal(err)
	}
	said, err := bake(home, home, game)
	if err != nil {
		t.Fatal(err)
	}
	if hashesIn(game) != "" ||
		!strings.Contains(said, "refuse to release") {
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

// never says which commit it was built from and how long ago that commit
// was made, the way game does; built by go, it says it has neither.
func TestAgeSaysTheCommitOrThatThereIsNone(t *testing.T) {
	base, err := time.Parse(time.RFC3339, "2026-10-15T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(time.Hour)
	for _, stamp := range []struct{ commit, at, want string }{
		{"dev", "", "never, built from a tree by go rather than by " +
			"game: no commit and no commit time in it"},
		{"abc1234", "", "never abc1234, with no commit time in it"},
		{"abc1234", "not a time", "never abc1234, committed at not a time"},
		{"abc1234", base.Format(time.RFC3339),
			"never abc1234, committed 1h0m0s ago"},
	} {
		if got := age(stamp.commit, stamp.at, now); got != stamp.want {
			t.Errorf("%q %q: %q, want %q", stamp.commit, stamp.at, got,
				stamp.want)
		}
	}
}

// --age is answered, and nothing else is: with no arguments never bakes.
func TestOnlyAgeIsAnswered(t *testing.T) {
	var out strings.Builder
	if !answer([]string{"never", "--age"}, &out) ||
		!strings.HasPrefix(out.String(), "never") {
		t.Errorf("--age: answered %q", out.String())
	}
	out.Reset()
	for _, args := range [][]string{{"never"}, {"never", "--other"}} {
		if answer(args, &out) || count.Positive(out.Len()) {
			t.Errorf("%q was answered: %q", args, out.String())
		}
	}
}

// run answers --age without baking, bakes otherwise and says so, and
// is 1, with the reason on errs, when the list cannot be baked.
func TestRunAnswersBakesOrSaysWhyNot(t *testing.T) {
	home, game := setup(t, "zorblax\n")
	var out, errs strings.Builder
	if code := run([]string{"never", "--age"}, home, home, game, &out,
		&errs); code != count.Nothing || hashesIn(game) != "" {
		t.Errorf("--age: %d, and baked %q", code, hashesIn(game))
	}
	out.Reset()
	if code := run([]string{"never"}, home, home, game, &out,
		&errs); code != count.Nothing || !strings.HasPrefix(out.String(),
		"never: 1 never-words baked in") {
		t.Errorf("bake: %d, said %q", code, out.String())
	}
	if code := run([]string{"never"}, home, home, t.TempDir(), &out,
		&errs); code != count.One || !strings.HasPrefix(errs.String(),
		"never: ") {
		t.Errorf("outside game: %d, said %q", code, errs.String())
	}
}

// the salt is sixteen random bytes, written as hex, and the hashes are
// for their owner alone. an empty salt file is made again, not used.
func TestTheSaltAndTheHashes(t *testing.T) {
	home, game := setup(t, "zorblax\n")
	kept := filepath.Join(home, ".config", "game", "sweep.salt")
	shutFile := os.FileMode(0o600)
	if err := os.WriteFile(kept, []byte("\n"), shutFile); err != nil {
		t.Fatal(err)
	}
	if _, err := bake(home, home, game); err != nil {
		t.Fatal(err)
	}
	salt, err := os.ReadFile(kept)
	hexDigits := int(held(t, "salt-hex-digits"))
	digits := strings.TrimSpace(string(salt))
	if err != nil || len(digits) != hexDigits ||
		strings.Trim(digits, "0123456789abcdef") != "" {
		t.Errorf("the salt: %q, %v, want %d lowercase hex digits", salt, err,
			hexDigits)
	}
	hashes, err := os.Stat(filepath.Join(game, "internal", "sweep",
		"never", "hashes"))
	if err != nil || hashes.Mode().Perm() != shutFile {
		t.Errorf("the hashes: %v, %v, want %v", hashes.Mode(), err,
			shutFile)
	}
}

// each thing that can stop a bake stops it with an error, and nothing
// is said to have been baked.
func TestBakeRefusesWhatItCannotDo(t *testing.T) {
	if os.Geteuid() == rootUID {
		t.Skip("root writes through a read-only directory")
	}
	cases := []refusalCase{
		{"an entry too long", entryTooLong},
		{"a salt that cannot be read", saltUnreadable},
		{"hashes that cannot be written", hashesUnwritable},
		{"hashes that cannot be removed", hashesUnremovable},
		{"nowhere to keep a salt", nowhereForSalt},
		{"a config that cannot be read", configUnreadable},
		{"a list that cannot be read", listUnreadable},
	}
	for _, each := range cases {
		t.Run(each.name, each.check)
	}
}

// rootUID is the superuser's user id, as unix has defined it.
const rootUID = 0

// refusalCase is one way a bake cannot, by name: what sets it up and
// what the error names.
type refusalCase struct {
	name string
	set  func(t *testing.T, home, game string) string
}

// check is the refusal as a sub-test: an error that names the place,
// nothing said baked, and no word printed.
func (each refusalCase) check(t *testing.T) {
	home, game := setup(t, "zorblax\n")
	want := each.set(t, home, game)
	said, err := bake(home, home, game)
	if err == nil || !strings.Contains(err.Error(), want) ||
		said != "" {
		t.Errorf("said %q, err %v, want an error naming %q",
			said, err, want)
	}
	if err != nil && strings.Contains(err.Error(), "zorblax") {
		t.Errorf("the error prints a word: %v", err)
	}
}

// entryTooLong puts a four-word never-word in the list.
func entryTooLong(t *testing.T, home, _ string) string {
	t.Helper()
	write(t, filepath.Join(home, ".config", "game",
		"sweep.words"), "one two three four\n")
	return "1"
}

// saltUnreadable puts a directory where the salt is read from.
func saltUnreadable(t *testing.T, home, _ string) string {
	t.Helper()
	mkdir(t, filepath.Join(home, ".config", "game", "sweep.salt"))
	return "sweep.salt"
}

// hashesUnwritable puts a directory where the hashes are written.
func hashesUnwritable(t *testing.T, _ string, game string) string {
	t.Helper()
	mkdir(t, filepath.Join(game, "internal", "sweep", "never",
		"hashes"))
	return "hashes"
}

// hashesUnremovable puts a directory with something in it where the
// hashes are taken away from.
func hashesUnremovable(t *testing.T, home, game string) string {
	t.Helper()
	words := filepath.Join(home, ".config", "game", "sweep.words")
	if err := os.Remove(words); err != nil {
		t.Fatal(err)
	}
	mkdir(t, filepath.Join(game, "internal", "sweep", "never",
		"hashes", "inside"))
	return "hashes"
}

// shutOut is a directory that cannot be written until the test ends.
type shutOut struct {
	dir  string
	open os.FileMode
}

// reopen opens the directory again.
func (shut shutOut) reopen() {
	_ = os.Chmod(shut.dir, shut.open)
}

// nowhereForSalt leaves the config directory read-only.
func nowhereForSalt(t *testing.T, home, _ string) string {
	t.Helper()
	at := filepath.Join(home, ".config", "game")
	readOnly := os.FileMode(0o500)
	if err := os.Chmod(at, readOnly); err != nil {
		t.Fatal(err)
	}
	open := os.FileMode(0o700)
	t.Cleanup(shutOut{dir: at, open: open}.reopen)
	return "sweep.salt"
}

// configUnreadable puts a directory where the config is read from.
func configUnreadable(t *testing.T, home, _ string) string {
	t.Helper()
	mkdir(t, filepath.Join(home, ".config", "game", "config"))
	return "config"
}

func write(t *testing.T, path, body string) {
	t.Helper()
	shutFile := os.FileMode(0o600)
	if err := os.WriteFile(path, []byte(body), shutFile); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	shutHome := os.FileMode(0o700)
	if err := os.MkdirAll(path, shutHome); err != nil {
		t.Fatal(err)
	}
}

// a salt that is there but cannot be read is refused, and left as it
// was: making a fresh one over it would change every hash without a
// word.
func TestASaltItCannotReadIsNeverReplaced(t *testing.T) {
	if os.Geteuid() == rootUID {
		t.Skip("root reads a file its mode refuses")
	}
	home, game := setup(t, "zorblax\n")
	kept := filepath.Join(home, ".config", "game", "sweep.salt")
	write(t, kept, "a-salt\n")
	writeOnly := os.FileMode(0o200)
	if err := os.Chmod(kept, writeOnly); err != nil {
		t.Fatal(err)
	}
	_, err := bake(home, home, game)
	shutFile := os.FileMode(0o600)
	if err := os.Chmod(kept, shutFile); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(kept)
	if err == nil || string(body) != "a-salt\n" {
		t.Errorf("err %v, and the salt is now %q", err, body)
	}
}

type failedRandom struct{ err error }

func (failure failedRandom) read([]byte) (int, error) {
	return count.Nothing, failure.err
}

func TestSaltRefusesRandomFailure(t *testing.T) {
	wanted := failedRandom{err: errors.New("random failed")}
	list := filepath.Join(t.TempDir(), "sweep.words")
	_, err := saltBeside(list, wanted.read)
	if !errors.Is(err, wanted.err) {
		t.Fatalf("saltBeside error %v, want %v", err, wanted.err)
	}
}

type recordedExit struct{ code int }

func (record *recordedExit) exit(given int) { record.code = given }

type mainState struct {
	args []string
	exit func(int)
}

func (state mainState) restore() { os.Args, exit = state.args, state.exit }

func TestMainAnswersAge(t *testing.T) {
	t.Cleanup(mainState{args: os.Args, exit: exit}.restore)
	os.Args = []string{"never", "--age"}
	record := &recordedExit{code: exitFailed}
	exit = record.exit
	main()
	if record.code != exitOK {
		t.Fatalf("main exit %d, want %d", record.code, exitOK)
	}
}

// listUnreadable puts a directory where the never-words list is read from.
func listUnreadable(t *testing.T, home, _ string) string {
	t.Helper()
	words := filepath.Join(home, ".config", "game", "sweep.words")
	if err := os.Remove(words); err != nil {
		t.Fatal(err)
	}
	mkdir(t, words)
	return "sweep.words"
}

// Lists that cannot be read refuse the bake and leave the hashes as they
// were. They do not bake none. That would take the hashes away and say
// the game will build.
func TestAListItCannotReadLeavesTheHashes(t *testing.T) {
	home, game := setup(t, "zorblax\n")
	if _, err := bake(home, home, game); err != nil {
		t.Fatal(err)
	}
	before := hashesIn(game)
	listUnreadable(t, home, game)
	said, err := bake(home, home, game)
	if err == nil || said != "" {
		t.Errorf("said %q, err %v, want a refusal", said, err)
	}
	if after := hashesIn(game); after != before || after == "" {
		t.Errorf("the hashes changed:\n%q\nto\n%q", before, after)
	}
}

// Comments past the scanner's limit do not cut the list short. Both
// words around one are baked.
func TestALongCommentDoesNotCutTheListShort(t *testing.T) {
	scanner := 1 << 16 // defined here: the scanner's default limit
	long := strings.Repeat("c", scanner+scanner)
	home, game := setup(t, "zorblax\n# "+long+"\nquinta\n")
	said, err := bake(home, home, game)
	if err != nil || !strings.Contains(said, "2 never-words") {
		t.Errorf("said %q, err %v, want two words baked", said, err)
	}
}
