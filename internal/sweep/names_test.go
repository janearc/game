package sweep_test

import (
	"strings"
	"testing"

	"github.com/janearc/game/internal/sweep"
)

// nameFixture is the hits of a tree whose files have these names and
// whose bodies are clean, swept for a never-word and the author.
func nameFixture(
	t *testing.T, names []string, allow map[string][]string, exclude ...string,
) []sweep.Hit {
	t.Helper()
	never := bakedFrom(t, "a-salt", []string{"zorblax"})
	files := map[string]string{}
	for _, name := range names {
		files[name] = "a clean body\n"
	}
	hits, err := sweep.Tree(treeFixture(t, files), "HEAD", never, theAuthor,
		allow, exclude...)
	if err != nil {
		t.Fatal(err)
	}
	return hits
}

// TestTreeReadsEachFilesNameForWhatItIsHeldTo: a never-word in a name is
// a hit in that file. So is the author's name with its marks taken as
// spaces. So are a session id and a uuid. The body may be clean.
func TestTreeReadsEachFilesNameForWhatItIsHeldTo(t *testing.T) {
	uuid := strings.Join(
		[]string{"0123abcd", "0123", "abcd", "0123", "abcd0123abcd"}, "-")
	session := "sess" + "ion_0123abcd"
	hits := nameFixture(t, []string{
		"zorblax-notes.md", "ada-lovelace.md", "docs/ada_lovelace/x.md",
		"notes-" + uuid + ".md", "log-" + session + ".txt", "clean.md",
	}, nil)
	want := strings.Join([]string{
		neverKind + " (name) in zorblax-notes.md",
		"session urls (name) in log-" + session + ".txt",
		"the author " + theAuthor + " (name) in ada-lovelace.md",
		"the author " + theAuthor + " (name) in docs/ada_lovelace/x.md",
		"uuids (name) in notes-" + uuid + ".md",
	}, "\n")
	if got := kinds(hits); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestTreeLeavesNamesThatOnlyLookLikeLeaks: an asset name with the shape
// of an address, a directory called home and a name that goes on in
// words after a session are not hits. Addresses and machine paths are
// not looked for in names.
func TestTreeLeavesNamesThatOnlyLookLikeLeaks(t *testing.T) {
	at := "@"
	hits := nameFixture(t, []string{
		"logo" + at + "2x.png", "home/readme.md", "Us" + "ers/readme.md",
		"session_notes.md", "ada.md", "lovelace.md",
	}, nil)
	if got := kinds(hits); got != "" {
		t.Errorf("names that only look like leaks were hit:\n%s", got)
	}
}

// TestNoAllowanceExcusesAName: a file allowed the author and its paths
// is still hit for a name that holds them.
func TestNoAllowanceExcusesAName(t *testing.T) {
	allow := map[string][]string{"ada-lovelace.md": {"author", "word"}}
	hits := nameFixture(t, []string{"ada-lovelace.md"}, allow)
	want := "the author " + theAuthor + " (name) in ada-lovelace.md"
	if got := kinds(hits); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestTreeSkipsTheNamesOfExcludedFiles: a name under an excluded prefix
// is not read.
func TestTreeSkipsTheNamesOfExcludedFiles(t *testing.T) {
	hits := nameFixture(t, []string{"vendor/zorblax.md", "zorblax.md"}, nil,
		"vendor/")
	want := neverKind + " (name) in zorblax.md"
	if got := kinds(hits); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestTreeFindsTextWithNulsBetweenItsLetters: text written two bytes to
// a letter has a nul between every pair. It is found. Binaries that hold
// no such text are not hit.
func TestTreeFindsTextWithNulsBetweenItsLetters(t *testing.T) {
	spread := strings.Join(strings.Split(machinePath, ""), "\x00")
	hits, err := sweep.Tree(treeFixture(t, map[string]string{
		"wide.bin":  "\xff\xfe" + spread,
		"quiet.bin": "\x00\x01\x02\x03 nothing here \x00",
	}), "HEAD", sweep.Never{}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(hits); got != "machine paths in wide.bin" {
		t.Errorf("got\n%s", got)
	}
}
