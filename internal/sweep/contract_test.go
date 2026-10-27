package sweep_test

import (
	"os"
	"reflect"
	"testing"

	"github.com/janearc/game/internal/sweep"
)

func TestBakedMatchesTheEmbeddedSource(t *testing.T) {
	t.Parallel()
	want := sweep.Baked(os.DirFS("."))
	built := sweep.Embedded()
	if !reflect.DeepEqual(sweep.Baked(built), want) {
		t.Fatal("embedded never-words differ from the build's source")
	}
}

func TestUnknownAllowanceDoesNotPermitMachinePaths(t *testing.T) {
	t.Parallel()
	tree := treeFixture(t, map[string]string{"notes": machinePath})
	allow := map[string][]string{"notes": {"unknown", "author"}}
	hits, err := sweep.Tree(tree, "HEAD", sweep.Never{}, theAuthor, allow)
	if err != nil || kinds(hits) != "machine paths in notes" {
		t.Fatalf("an unrelated allowance suppressed paths: %v %v", hits, err)
	}
}

func TestMatchingAllowanceStillWorksAfterUnknownKind(t *testing.T) {
	t.Parallel()
	tree := treeFixture(t, map[string]string{"notes": machinePath})
	allow := map[string][]string{"notes": {"unknown", "author", "paths"}}
	hits, err := sweep.Tree(tree, "HEAD", sweep.Never{}, theAuthor, allow)
	if err != nil || kinds(hits) != "" {
		t.Fatalf("a matching allowance was ignored: %v %v", hits, err)
	}
}
