package sweep_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/sweep"
)

// the word list reads one word per line and ignores blanks and comments.
func TestWords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words")
	openFile := os.FileMode(0o644)
	body := []byte("# names\nzebra\n\nquokka\n")
	if err := os.WriteFile(path, body, openFile); err != nil {
		t.Fatal(err)
	}
	words, err := sweep.Words(path)
	if want := []string{"zebra", "quokka"}; err != nil ||
		!slices.Equal(words, want) {
		t.Fatalf("got %v, %v, want %v", words, err, want)
	}
	missing, err := sweep.Words(path + ".missing")
	if count.Positive(len(missing)) || err != nil {
		t.Errorf("a missing list was %v, %v, not none", missing, err)
	}
}

// Lines are read whole at any length. Comments past the scanner's limit
// do not end the read. The words after one are not lost.
func TestWordsReadsLinesOfAnyLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "words")
	openFile := os.FileMode(0o644)
	scanner := 1 << 16 // defined here: the scanner's default limit
	long := strings.Repeat("c", scanner+scanner)
	body := []byte("zebra\n# " + long + "\nquokka\n" + long + "\n")
	if err := os.WriteFile(path, body, openFile); err != nil {
		t.Fatal(err)
	}
	words, err := sweep.Words(path)
	want := []string{"zebra", "quokka", long}
	if err != nil || !slices.Equal(words, want) {
		t.Fatalf("read %d words, %v; want %d", len(words), err, len(want))
	}
}

// Lists that cannot be read are an error with no words. They are not an
// empty list. Good policies are not thrown away by reads that failed.
func TestWordsOfAListThatCannotBeReadIsAnError(t *testing.T) {
	path := t.TempDir()
	got := make(chan answer, count.One)
	go readInTheBackground(path, got)
	var said answer
	select {
	case said = <-got:
	case <-time.After(readPatience):
		t.Fatal("a list that cannot be read was read for ever")
	}
	if said.err == nil || count.Positive(len(said.words)) {
		t.Fatalf("a directory as the list: %v, %v", said.words, said.err)
	}
	if !strings.Contains(said.err.Error(), path) {
		t.Errorf("the error does not name the list: %v", said.err)
	}
}

// answer is what Words gave for a list: its words and its error.
type answer struct {
	words []string
	err   error
}

// readInTheBackground reads a list and sends what Words gave, so a read
// that never ends can be given up on.
func readInTheBackground(path string, got chan<- answer) {
	words, err := sweep.Words(path)
	got <- answer{words, err}
}

// the patterns find what they are for. the fixtures are assembled at
// run time so this file spells none of them, and the last check proves
// that by sweeping this file's own source with every pattern.
func TestPatterns(t *testing.T) {
	uuid := strings.Join(
		[]string{"0123abcd", "0123", "abcd", "0123", "abcd0123abcd"},
		"-",
	)
	cases := map[string]string{
		"session urls": "see https://cla" + "ude.ai/code/sess" +
			"ion_0123abcd",
		"uuids":           "id " + uuid,
		"machine paths":   "kept at /Us" + "ers/someone/x",
		"email addresses": "write to a.b" + "@" + "c.de",
	}
	for kind, text := range cases {
		got := proseKinds(t, sweep.Never{}, text)[count.First]
		if !strings.Contains(got, kind) {
			t.Errorf("%s missed its case: %v", kind, got)
		}
	}
	sources := map[string]string{}
	for _, file := range []string{"sweep.go", "sweep_test.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sources[file] = string(src)
	}
	hits, err := sweep.Tree(treeFixture(t, sources), "HEAD", sweep.Never{},
		"", nil)
	if err != nil || count.Positive(len(hits)) {
		t.Errorf("the sources sweep to %v, %v", hits, err)
	}
}

// an address in a documentation domain is nobody's and does not stop a
// release; a real one does.
func TestReservedDomainsAreNotAddresses(t *testing.T) {
	for _, got := range proseKinds(t, sweep.Never{},
		"set CONTACT ada@example.com",
		"ada@example.org and bob@example.net",
		"someone@thing.example",
		"contact ada@example.invalid",
		"dist git@github.com:ada/game.git",
	) {
		if strings.Contains(got, "email addresses") {
			t.Errorf("a documentation address was taken for a real one")
		}
	}
	// built from pieces so that no address appears in this file: the
	// sweep reads its own source and must find none.
	at := "@"
	texts := []string{
		"write to ada" + at + "ada.dev",
		"set CONTACT ada@example.com, bob" + at + "bob.nl",
	}
	for place, got := range proseKinds(t, sweep.Never{}, texts...) {
		if !strings.Contains(got, "email addresses") {
			t.Errorf("a real address was missed: %q", texts[place])
		}
	}
}

// TestBareUUID is the record id in a catalogue link, which is not a
// session, against the same id on its own, which might be.
func TestBareUUID(t *testing.T) {
	// assembled from pieces, like the patterns, so the sweep of this
	// tree does not find its own fixture
	id := "22be4b55-2466-" + "4320-e053-10a3070a5236"
	got := proseKinds(t, sweep.Never{},
		"index_url: https://ecat.example.org/records/"+id,
		"seen in "+id+" yesterday")
	if strings.Contains(got[count.First], "uuids") {
		t.Error("a uuid inside a link was counted")
	}
	if !strings.Contains(got[count.Next(count.First)], "uuids") {
		t.Error("a bare uuid was missed")
	}
}

// the git@host:path form of a remote is not an address, and only that
// form is let by: an address followed by a colon, or a git@host with
// nothing after it, is an address.
func TestGitRemotesAreNotAddressesButTheRestAre(t *testing.T) {
	at := "@"
	texts := []string{
		"dist git@github.com:ada/game.git",
		"write to ada" + at + "ada.dev: soon",
		"pushed to git" + at + "github.com",
	}
	got := proseKinds(t, sweep.Never{}, texts...)
	if strings.Contains(got[count.First], "email addresses") {
		t.Error("a git remote was taken for an address")
	}
	for place, one := range got[count.Next(count.First):] {
		if !strings.Contains(one, "email addresses") {
			t.Errorf("an address was missed: %q",
				texts[count.Next(count.First)+place])
		}
	}
}

// Lists that cannot be opened for a reason other than being absent are an
// error too. Here the path runs through a file.
func TestWordsOfAListThatCannotBeOpenedIsAnError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(file, []byte("x"), openFile); err != nil {
		t.Fatal(err)
	}
	words, err := sweep.Words(filepath.Join(file, "words"))
	if err == nil || count.Positive(len(words)) {
		t.Errorf("a list under a file: %v, %v", words, err)
	}
}

// A list that cannot be opened gives the error of opening it, not a later
// one: the caller learns which path and why.
func TestWordsOfAListThatCannotBeOpenedSaysWhyItFailedToOpen(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), fileMode); err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(file, "words")
	_, err := sweep.Words(list)
	var failed *fs.PathError
	if !errors.As(err, &failed) || failed.Op != "open" ||
		!errors.Is(err, syscall.ENOTDIR) {
		t.Errorf("a list under a file: %#v", err)
	}
}

// A list that is not there is no words and no error: the anti-test of the
// two above.
func TestWordsOfAListThatIsNotThereIsNoWordsAndNoError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")
	words, err := sweep.Words(path)
	if err != nil || words != nil {
		t.Errorf("an absent list: %v, %v", words, err)
	}
}

// fileMode is the mode a list is written with; readPatience is how long a
// read of a list may take before it is called endless.
const (
	fileMode     = fs.FileMode(0o644)
	readPatience = 5 * time.Second // defined here: long enough for any read, short enough to give up
)
