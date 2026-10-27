package sweep_test

import (
	"github.com/janearc/game/internal/repo"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/sweep"
)

// a trailer anywhere in a revision's history is one hit, named
// (history); a clean history is none; a revision git does not know is
// an error.
func TestHistoryFindsATrailerAndStopsOnABadRevision(t *testing.T) {
	mem := treeMem(t, map[string]string{"a.txt": "one\n"})
	tree := repo.At("/work", mem)
	if hits, err := sweep.History(tree, "HEAD"); err != nil ||
		count.Positive(len(hits)) {
		t.Errorf("a clean history: %v %v", hits, err)
	}
	if _, err := mem.Commit("two\n\nCo-Auth"+"ored-By: someone",
		map[string]string{"a.txt": "two\n"}); err != nil {
		t.Fatal(err)
	}
	hits, err := sweep.History(tree, "HEAD")
	if err != nil || kinds(hits) != "attribution trailers in (history)" {
		t.Errorf("a trailer in the history: %v %v", hits, err)
	}
	if _, err := sweep.History(tree, "nope"); err == nil {
		t.Error("a revision git does not know was not an error")
	}
}

// a revision git cannot list is an error from Tree too.
func TestTreeStopsOnARevisionItCannotList(t *testing.T) {
	tree := treeFixture(t, map[string]string{"a.txt": "fine\n"})
	if _, err := sweep.Tree(tree, "nope", sweep.Never{}, "",
		nil); err == nil {
		t.Error("a revision git does not know was not an error")
	}
}
