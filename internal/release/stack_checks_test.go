package release_test

import (
	"errors"
	"github.com/janearc/game/internal/count"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
	"go.uber.org/mock/gomock"
)

// TestAStackRefusesATreeThatIsNotClean: a root with a change not
// committed is refused as a release refuses it. Nothing is kept.
func TestAStackRefusesATreeThatIsNotClean(t *testing.T) {
	t.Parallel()
	tree, dist, members := stackOf(t, "x", map[string]string{"x.txt": "x\n"})
	stray := filepath.Join(tree.Dir(), "stray.txt")
	readable := os.FileMode(0o644)
	if err := os.WriteFile(stray, []byte("stray\n"), readable); err != nil {
		t.Fatal(err)
	}
	_, err := release.Stack(tree, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(), "the tree is dirty") {
		t.Errorf("an untracked file: %v", err)
	}
	if kept, _ := tree.Git("rev-parse", "--verify", "-q",
		refOf("v1")); kept != "" {
		t.Errorf("a refused stack kept %s", kept)
	}
}

// TestAStackLeavesOutWhatIsOmitted: the root omits a file from every
// release. The file is in the root. It is not in the stack. Files the
// root keeps stay.
func TestAStackLeavesOutWhatIsOmitted(t *testing.T) {
	t.Parallel()
	tree, dist, members := stackOf(t, "x", map[string]string{"x.txt": "x\n"})
	addFiles(t, tree.Dir(), map[string]string{
		"hidden.txt": "hidden\n", "inside/also.txt": "also\n",
		"shown.txt": "shown\n"})
	omitting := stackOptions(dist).Omitting([]string{"hidden.txt", "inside"})
	out, err := release.Stack(tree, omitting, members)
	if err != nil {
		t.Fatal(err)
	}
	files := filesAt(t, tree, out.Commit())
	got := strings.Join(files, " ")
	if got != "README.md shown.txt x/x.txt" {
		t.Errorf("the stack holds %q", got)
	}
}

// TestAStackRefusesTheTrailersInDistsHistory: the commit is parented on
// dist's main, so dist's history is the stack's. Trailers there refuse
// the stack as they refuse a release, and nothing is kept.
func TestAStackRefusesTheTrailersInDistsHistory(t *testing.T) {
	t.Parallel()
	tree, dist, members := stackOf(t, "x", map[string]string{"x.txt": "x\n"})
	past := filepath.Join(t.TempDir(), "past")
	gitRepo(t, past, map[string]string{"old.txt": "old\n"})
	trailer := "Co-Auth" + "ored-By: someone <s@example.invalid>"
	gitIn(t, past, "commit", "-q", "--allow-empty", "-m", "old\n\n"+trailer)
	gitIn(t, past, "push", "-q", dist, "main")
	_, err := release.Stack(tree, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(), "attribution trailers") {
		t.Errorf("a trailer in dist's history: %v", err)
	}
	if kept, _ := tree.Git("rev-parse", "--verify", "-q",
		refOf("v1")); kept != "" {
		t.Errorf("a refused stack kept %s", kept)
	}
}

// TestAStackLetsCleanHistoryThrough: dist's main without a trailer is
// parented on and the stack is kept.
func TestAStackLetsCleanHistoryThrough(t *testing.T) {
	t.Parallel()
	tree, dist, members := stackOf(t, "x", map[string]string{"x.txt": "x\n"})
	past := filepath.Join(t.TempDir(), "past")
	gitRepo(t, past, map[string]string{"old.txt": "old\n"})
	gitIn(t, past, "push", "-q", dist, "main")
	if _, err := release.Stack(tree, stackOptions(dist),
		members); err != nil {
		t.Fatal(err)
	}
}

// refAbsent is a check that the release ref for the tag is not there.
type refAbsent struct {
	t    *testing.T
	tree repo.Repo
}

// Qualify is the mock's answer. It looks for the ref as the stack is
// shown. The ref must not be written yet.
func (look refAbsent) Qualify(assembled release.Stacked) error {
	look.t.Helper()
	if kept, _ := look.tree.Git("rev-parse", "--verify", "-q",
		refOf("v1")); kept != "" {
		look.t.Errorf("the ref was written before it was asked: %s", kept)
	}
	if assembled.Commit() == "" {
		look.t.Error("the stack was shown without its commit")
	}
	return nil
}

// TestAStackIsShownToItsQualifierBeforeItsRefIsWritten: the qualifier is
// asked once about the stack. The ref is written after.
func TestAStackIsShownToItsQualifierBeforeItsRefIsWritten(t *testing.T) {
	t.Parallel()
	tree, dist, members := stackOf(t, "x", map[string]string{"x.txt": "x\n"})
	qualifier := NewMockQualifier(gomock.NewController(t))
	look := refAbsent{t: t, tree: tree}
	qualifier.EXPECT().Qualify(gomock.Any()).DoAndReturn(look.Qualify).Times(count.One)
	out, err := release.Stack(tree,
		stackOptions(dist).Qualifying(qualifier), members)
	if err != nil {
		t.Fatal(err)
	}
	kept, _ := tree.Git("rev-parse", "--verify", "-q", refOf("v1"))
	if kept != out.Commit() {
		t.Errorf("the ref is %q, want %q", kept, out.Commit())
	}
}

// TestARefusedStackLeavesTheEarlierRefAsItWas: the qualifier refuses.
// The stack stops with the qualifier's own refusal. The stack kept at the
// same tag before is still there.
func TestARefusedStackLeavesTheEarlierRefAsItWas(t *testing.T) {
	t.Parallel()
	tree, dist, members := stackOf(t, "x", map[string]string{"x.txt": "x\n"})
	earlier, err := release.Stack(tree, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	qualifier := NewMockQualifier(gomock.NewController(t))
	refusal := errors.New("not clean by the members' rules")
	qualifier.EXPECT().Qualify(gomock.Any()).Return(refusal).Times(count.One)
	_, err = release.Stack(tree, stackOptions(dist).Qualifying(qualifier),
		members)
	if !errors.Is(err, refusal) {
		t.Fatalf("the refusal was %v", err)
	}
	kept, _ := tree.Git("rev-parse", "--verify", "-q", refOf("v1"))
	if kept != earlier.Commit() {
		t.Errorf("the earlier ref is %q, want %q", kept, earlier.Commit())
	}
}
