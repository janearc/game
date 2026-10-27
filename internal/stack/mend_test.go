package stack_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/stack"
)

// branchOffer is a fixer that offers one branch for every failure.
type branchOffer struct{ branch string }

// Fix offers the branch.
func (offer branchOffer) Fix(
	_ context.Context, _ stack.Member, _, _, _ string,
) (string, error) {
	return offer.branch, nil
}

// the fix on a branch that cannot be cloned is never built. the member
// failed once and the table says it was built once.
func TestAFixThatCannotBeClonedIsNotBuilt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	deterministic := `echo '{"step":"check","state":"fail",` +
		`"kind":"deterministic","text":"no"}'; exit 1` + "\n"
	urls := roads{"bad": bareRepo(t, dir, "bad",
		map[string]string{"build.sh": deterministic})}
	members := []stack.Member{stack.Named("bad", "v1")}
	options := made(members, filepath.Join(dir, "work"), child).
		From(urls.at).Fixing(branchOffer{branch: "no-such-branch"})
	rows, err := runBefore(t, context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	got := tableOf(t, rows)["bad"]
	if len(rows) != count.One || got.result != "failed at check" ||
		got.tries != count.One {
		t.Errorf("a fix with no branch to clone: %+v", got)
	}
}
