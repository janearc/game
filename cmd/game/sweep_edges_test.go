package main

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/janearc/game/internal/sweep"
)

// fakeObject is a commit name no object store holds: forty hex digits,
// as sha-1 has defined a git object's name.
const fakeObject = "abababababababababababababababababababab"

// words is never-words the sweep tests look for.
func words(t *testing.T) sweep.Never {
	t.Helper()
	body, err := sweep.Bake("a-salt", []string{"zorblax"})
	if err != nil {
		t.Fatal(err)
	}
	return sweep.Baked(fstest.MapFS{"never/hashes": {Data: []byte(body)}})
}

// swept is a repository whose first commit holds a never-word in one
// file, whose second puts it in another, and whose third takes both out.
func swept(t *testing.T) string {
	t.Helper()
	work := freshTree(t, "x", map[string]string{
		"a.txt": "a zorblax here\n"})
	commitFiles(t, work.Dir(), map[string]string{"b.txt": "and zorblax\n"})
	gitAt(t, work.Dir(), "rm", "-q", "a.txt", "b.txt")
	gitAt(t, work.Dir(), "commit", "-q", "-m", "taken out")
	return work.Dir()
}

// sweep reads HEAD unless told a revision, says each hit and how many,
// and says a clean one is clean.
func TestSweepReadsTheRevisionItIsTold(t *testing.T) {
	t.Parallel()
	work := repoAt(swept(t))
	var out bytes.Buffer
	if err := verbTo(t, "sweep", sweepVerb, work, blankConfig(), nil,
		&out); err != nil || out.String() != "sweep: HEAD is clean\n" {
		t.Errorf("HEAD: %v, said %q", err, out.String())
	}
	out.Reset()
	err := verbTo(t, "sweep", sweepVerb, work, blankConfig(), []string{"HEAD~1"}, &out)
	if err == nil || err.Error() != "2 hit(s)" {
		t.Errorf("HEAD~1: %v", err)
	}
	for _, file := range []string{"a.txt", "b.txt"} {
		if !strings.Contains(out.String(), " in "+file+"\n") {
			t.Errorf("HEAD~1: no hit in %s:\n%s", file, out.String())
		}
	}
	if strings.Contains(out.String(), "clean") {
		t.Errorf("HEAD~1 said clean:\n%s", out.String())
	}
	out.Reset()
	err = verbTo(t, "sweep", sweepVerb, work, blankConfig(), []string{"HEAD~2"}, &out)
	if err == nil || err.Error() != "1 hit(s)" ||
		strings.Contains(out.String(), "clean") {
		t.Errorf("HEAD~2: %v, said %q", err, out.String())
	}
}

// with no never-words a sweep is never clean, and a revision that is
// not there, or a tree it cannot read, is the error.
func TestSweepSaysWhatItCannotSay(t *testing.T) {
	work := repoAt(swept(t))
	var out bytes.Buffer
	// not parallel: the never-words are the program's, and this test
	// takes them away for the one sweep.
	baked := neverWords
	neverWords = func() sweep.Never { return sweep.Baked(fstest.MapFS{}) }
	err := verbTo(t, "sweep", sweepVerb, work, blankConfig(), nil, &out)
	neverWords = baked
	if err == nil || !strings.HasPrefix(err.Error(),
		"HEAD has none of the fixed patterns, but this game was "+
			"built without never-words") ||
		strings.Contains(out.String(), "clean") {
		t.Errorf("no never-words: %v, said %q", err, out.String())
	}
	err = verbTo(t, "sweep", sweepVerb, work, blankConfig(), []string{"nowhere"},
		&bytes.Buffer{})

	if err == nil {
		t.Error("a revision that is not there was swept")
	}
	// a submodule whose commit is not here: the history reads, the
	// tree does not
	gitAt(t, work.Dir(), "update-index", "--add", "--cacheinfo",
		"160000,"+fakeObject+",sub")
	gitAt(t, work.Dir(), "commit", "-q", "-m", "a submodule not here")
	out.Reset()
	if err := verbTo(t, "sweep", sweepVerb, work, blankConfig(), nil,
		&out); err == nil || strings.Contains(out.String(), "clean") {
		t.Errorf("a tree it cannot read: %v, said %q", err,
			out.String())
	}
}
