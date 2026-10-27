package release_test

import (
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/release"
	"slices"
	"testing"
)

// TestAStackSaysWhichDirectoriesAreModules: they come in laid order.
func TestAStackSaysWhichDirectoriesAreModules(t *testing.T) {
	t.Parallel()
	out, err := stackOfModules(t, map[string]string{
		"a": "module example.invalid/a\n\ngo 1.26\n",
		"b": "module example.invalid/b\n\ngo 1.26\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Modules(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("modules %q, want a and b", got)
	}
}

// TestAStackOfNoModulesNamesNone: and writes no go.work.
func TestAStackOfNoModulesNamesNone(t *testing.T) {
	t.Parallel()
	tree, dist, members := stackOf(t, "x", map[string]string{"x.txt": "x\n"})
	out, err := release.Stack(tree, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.Modules(); count.Positive(len(got)) || out.Work() != "" {
		t.Errorf("modules %q, work %q, want none", got, out.Work())
	}
}
