package repo_test

import (
	"errors"
	"github.com/janearc/game/internal/gittest"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"go.uber.org/mock/gomock"
)

func names(t *testing.T, mem *gittest.Mem, tree string) []string {
	t.Helper()
	listed, err := mem.Names(tree)
	if err != nil {
		t.Fatal(err)
	}
	return listed
}

func TestCleanIsTrueOnlyForAFreshCommit(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	if clean, err := work.Clean(); err != nil || !clean {
		t.Fatalf("a fresh commit: clean %v, err %v", clean, err)
	}
}

// changes are the ways a tree stops being clean, by name.
var changes = map[string]func(*gittest.Mem) error{
	"untracked": untrackedFile,
	"modified":  modifiedFile,
	"staged":    stagedFile,
}

func untrackedFile(mem *gittest.Mem) error {
	return mem.Write("new.txt", "new\n")
}

func modifiedFile(mem *gittest.Mem) error {
	return mem.Write("a.txt", "changed\n")
}

func stagedFile(mem *gittest.Mem) error {
	if err := mem.Write("new.txt", "new\n"); err != nil {
		return err
	}
	return mem.Stage("new.txt")
}

// change is one way a tree stops being clean, run as a sub-test.
type change func(*gittest.Mem) error

// check is the sub-test: after the change the tree is not clean.
func (apply change) check(t *testing.T) {
	work, mem := fixture(t)
	if err := apply(mem); err != nil {
		t.Fatal(err)
	}
	if clean, err := work.Clean(); err != nil || clean {
		t.Errorf("after the change: clean %v, err %v", clean, err)
	}
}

func TestCleanSeesUntrackedModifiedAndStaged(t *testing.T) {
	t.Parallel()
	for name, apply := range changes {
		t.Run(name, change(apply).check)
	}
}

func TestBranchIsTheCheckedOutBranch(t *testing.T) {
	t.Parallel()
	work, mem := fixture(t)
	if err := mem.Checkout("work"); err != nil {
		t.Fatal(err)
	}
	if got, err := work.Branch(); err != nil || got != "work" {
		t.Errorf("got %q, %v, want work", got, err)
	}
}

func TestTreeIsTheRevisionsTree(t *testing.T) {
	t.Parallel()
	work, mem := fixture(t)
	got, err := work.Tree("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if listed := names(t, mem, got); !slices.Equal(listed,
		[]string{"a.txt", "docs/b.txt"}) {
		t.Errorf("the tree holds %q", listed)
	}
	if _, err := work.Tree("no-such-rev"); err == nil {
		t.Error("a revision that is not there was not refused")
	}
}

func TestTreeWithoutNothingIsTheTree(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	runner.EXPECT().Run("/work", "", nil,
		[]string{"rev-parse", "HEAD^{tree}"}).Return("abc\n", nil)
	got, err := work.TreeWithout("HEAD", nil)
	if err != nil || got != "abc" {
		t.Errorf("got %q, %v; want the revision's own tree", got, err)
	}
}

func TestTreeWithoutLeavesOutThePathsAndTouchesNothing(t *testing.T) {
	t.Parallel()
	work, mem := fixture(t)
	tree, err := work.TreeWithout("HEAD", []string{"docs/b.txt", "not-here"})
	if err != nil {
		t.Fatal(err)
	}
	if listed := names(t, mem, tree); !slices.Equal(listed,
		[]string{"a.txt"}) {
		t.Errorf("the tree holds %v, want only a.txt", listed)
	}
	if clean, err := work.Clean(); err != nil || !clean {
		t.Errorf("the working tree changed: clean %v, %v", clean, err)
	}
	kept, _ := work.Tree("HEAD")
	if listed := names(t, mem, kept); len(listed) != fileCount {
		t.Errorf("the commit lost files: %v", listed)
	}
}

func TestTreeWithoutRefusesARevisionThatIsNotThere(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	_, err := work.TreeWithout("no-such-rev", []string{"a.txt"})
	if err == nil || !strings.Contains(err.Error(), "read-tree no-such-rev") {
		t.Errorf("a revision that is not there: %v", err)
	}
}

func TestTreeWithoutRefusesAnOmitGitWillNotTake(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	read := runner.EXPECT().Run(anything, anything, anything, anything).
		Return("", nil)
	omit := runner.EXPECT().Run(anything, anything, anything, anything).
		Return("", errors.New("git rm: outside the repository"))
	gomock.InOrder(read, omit)
	_, err := work.TreeWithout("HEAD", []string{"../outside"})
	if err == nil || !strings.Contains(err.Error(), "git rm") {
		t.Errorf("an omit outside the tree: %v", err)
	}
}

func TestTreeWithLicenseAddsTheFileAndOmits(t *testing.T) {
	t.Parallel()
	work, mem := fixture(t)
	tree, err := work.TreeWithLicense("HEAD", []string{"a.txt"},
		"LICENSE.txt", licence(t, "terms\n"))
	if err != nil {
		t.Fatal(err)
	}
	if listed := names(t, mem, tree); !slices.Equal(listed,
		[]string{"LICENSE.txt", "docs/b.txt"}) {
		t.Errorf("the tree holds %v", listed)
	}
	if clean, _ := work.Clean(); !clean {
		t.Error("the working tree changed")
	}
}

// updates remembers the arguments of the update-index it was asked.
type updates struct{ args []string }

func (seen *updates) note(_, _ string, _, args []string) (string, error) {
	seen.args = args
	return "", nil
}

func TestTreeWithLicenseEntersItAsAPlainFileWithTheBodyGiven(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	seen := &updates{}
	read := runner.EXPECT().Run(anything, anything, anything, anything).
		Return("", nil)
	store := runner.EXPECT().Run("/work", "terms\n", nil,
		[]string{"hash-object", "-w", "--stdin"}).Return("beef\n", nil)
	update := runner.EXPECT().Run(anything, anything, anything, anything).
		DoAndReturn(seen.note)
	write := runner.EXPECT().Run(anything, anything, anything, anything).
		Return("cafe\n", nil)
	gomock.InOrder(read, store, update, write)
	tree, err := work.TreeWithLicense("HEAD", nil, "LICENSE.txt",
		licence(t, "terms\n"))
	if err != nil || tree != "cafe" {
		t.Errorf("tree %q, %v", tree, err)
	}
	want := []string{"update-index", "--add", "--cacheinfo",
		"100644,beef,LICENSE.txt"}
	if !slices.Equal(seen.args, want) {
		t.Errorf("update-index was %q", seen.args)
	}
}

func TestTreeWithLicenseAndNothingToOmit(t *testing.T) {
	t.Parallel()
	work, mem := fixture(t)
	tree, err := work.TreeWithLicense("HEAD", nil, "LICENSE.txt",
		licence(t, "terms\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"LICENSE.txt", "a.txt", "docs/b.txt"}
	if listed := names(t, mem, tree); !slices.Equal(listed, want) {
		t.Errorf("the tree holds %v, want %v", listed, want)
	}
}

func TestTreeWithLicenseRefusesAMissingOrEmptyLicence(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	missing := filepath.Join(t.TempDir(), "missing")
	_, err := work.TreeWithLicense("HEAD", nil, "LICENSE.txt", missing)
	if !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(),
		missing) {
		t.Errorf("a missing licence: %v", err)
	}
	empty := licence(t, "")
	_, err = work.TreeWithLicense("HEAD", nil, "LICENSE.txt", empty)
	if err == nil || errors.Is(err, fs.ErrNotExist) ||
		!strings.Contains(err.Error(), empty) {
		t.Errorf("an empty licence: %v", err)
	}
}

func TestTreeWithLicenseRefusesARevisionThatIsNotThere(t *testing.T) {
	t.Parallel()
	work, _ := fixture(t)
	_, err := work.TreeWithLicense("no-such-rev", nil, "LICENSE.txt",
		licence(t, "terms\n"))
	if err == nil || !strings.Contains(err.Error(), "read-tree no-such-rev") {
		t.Errorf("a revision that is not there: %v", err)
	}
}

func TestTreeWithLicenseRefusesWhenTheLicenceCannotBeStored(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	read := runner.EXPECT().Run(anything, anything, anything, anything).
		Return("", nil)
	store := runner.EXPECT().Run(anything, anything, anything, anything).
		Return("", errors.New("git hash-object: store is read only"))
	gomock.InOrder(read, store)
	_, err := work.TreeWithLicense("HEAD", nil, "LICENSE.txt",
		licence(t, "terms\n"))
	if err == nil || !strings.Contains(err.Error(), "hash-object") {
		t.Errorf("an object store that cannot be written: %v", err)
	}
}

func TestTreeWithLicenseRefusesAPathGitWillNotTake(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	read := runner.EXPECT().Run(anything, anything, anything, anything).
		Return("", nil)
	store := runner.EXPECT().Run(anything, anything, anything, anything).
		Return("beef\n", nil)
	update := runner.EXPECT().Run(anything, anything, anything, anything).
		Return("", errors.New("git update-index: outside the repository"))
	gomock.InOrder(read, store, update)
	_, err := work.TreeWithLicense("HEAD", nil, "../LICENSE.txt",
		licence(t, "terms\n"))
	if err == nil || !strings.Contains(err.Error(), "update-index") {
		t.Errorf("a licence outside the tree: %v", err)
	}
}

func TestTreeBuildersOmitADirectory(t *testing.T) {
	t.Parallel()
	work, mem := fixture(t)
	without, err := work.TreeWithout("HEAD", []string{"docs"})
	if err != nil {
		t.Fatal(err)
	}
	with, err := work.TreeWithLicense("HEAD", []string{"docs"},
		"LICENSE.txt", licence(t, "terms\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := names(t, mem, without); !slices.Equal(got, []string{"a.txt"}) {
		t.Errorf("without holds %v", got)
	}
	if got := names(t, mem, with); !slices.Equal(got,
		[]string{"LICENSE.txt", "a.txt"}) {
		t.Errorf("with holds %v", got)
	}
}

func TestOmittingADirectoryLeavesAFileWithTheSamePrefix(t *testing.T) {
	t.Parallel()
	work, mem := fixture(t)
	if _, err := mem.Commit("more", map[string]string{
		"docs.md": "x\n"}); err != nil {
		t.Fatal(err)
	}
	tree, err := work.TreeWithout("HEAD", []string{"docs"})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(t, mem, tree); !slices.Equal(got,
		[]string{"a.txt", "docs.md"}) {
		t.Errorf("the tree holds %v", got)
	}
}

func TestShowIsTheFileAtTheRevision(t *testing.T) {
	t.Parallel()
	work, mem := fixture(t)
	if err := mem.Write("a.txt", "changed\n"); err != nil {
		t.Fatal(err)
	}
	if got, err := work.Show("HEAD", "a.txt"); err != nil || got != "a" {
		t.Errorf("got %q, %v, want a", got, err)
	}
	if _, err := work.Show("HEAD", "not-here"); err == nil {
		t.Error("a path that is not there was not refused")
	}
}

func TestScratchHoldsTheTreeLessWhatIsOmitted(t *testing.T) {
	t.Parallel()
	work, mem := fixture(t)
	scratch, err := work.Scratch("HEAD", []string{"docs"})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := work.GitEnv(scratch.Env(), "write-tree")
	if err != nil || !slices.Equal(names(t, mem, tree), []string{"a.txt"}) {
		t.Errorf("the scratch index holds %q (%v)", tree, err)
	}
	named := scratch.Env()[count.First]
	_, file, _ := strings.Cut(named, "=")
	scratch.Close()
	if _, err := os.Stat(file); err == nil {
		t.Error("closing left the scratch index")
	}
	if _, err := work.Scratch("no-such-revision", nil); err == nil {
		t.Error("a revision git does not know was read in")
	}
}

func TestTreeBuildersRefuseWithoutATempDirectory(t *testing.T) {
	work, _ := fixture(t)
	gone := filepath.Join(t.TempDir(), "not-here")
	t.Setenv("TMPDIR", gone)
	if _, err := work.TreeWithout("HEAD", []string{"a.txt"}); err == nil {
		t.Error("TreeWithout made an index with nowhere to put it")
	}
}

// made remembers the scratch index it was told to use, and writes it.
type made struct{ index string }

func (seen *made) write(_, _ string, env, _ []string) (string, error) {
	seen.index = strings.TrimPrefix(env[count.First], "GIT_INDEX_FILE=")
	_ = os.WriteFile(seen.index, []byte("x"), 0o600)
	return "", nil
}

func (seen *made) refuse(
	_, _ string, env, args []string,
) (string, error) {
	_, _ = seen.write("", "", env, args)
	return "", errors.New("no")
}

func TestScratchIsCleanedUpWhenReadTreeRefuses(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	seen := &made{}
	runner.EXPECT().Run(anything, anything, anything, anything).
		DoAndReturn(seen.refuse)
	if _, err := work.Scratch("HEAD", nil); err == nil {
		t.Fatal("a refusal was lost")
	}
	if _, err := os.Stat(seen.index); err == nil {
		t.Error("a refused scratch index was left behind")
	}
}

func TestScratchIsCleanedUpWhenOmitRefuses(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	seen := &made{}
	scratch := runner.EXPECT().Run(anything, anything, anything, anything).
		DoAndReturn(seen.write)
	omit := runner.EXPECT().Run(anything, anything, anything, anything).
		Return("", errors.New("no"))
	gomock.InOrder(scratch, omit)
	if _, err := work.Scratch("HEAD", []string{"a"}); err == nil {
		t.Fatal("a refusal was lost")
	}
	if _, err := os.Stat(seen.index); err == nil {
		t.Error("a refused scratch index was left behind")
	}
}

func TestRemoteMainIsTheRoadsMain(t *testing.T) {
	t.Parallel()
	work, mem := fixture(t)
	road, err := gittest.New()
	if err != nil {
		t.Fatal(err)
	}
	mem.Remote("road", "/road.git", road)
	if got, err := work.RemoteMain("road"); err != nil || got != "" {
		t.Fatalf("a road with no main: %q, %v", got, err)
	}
	head, err := road.Commit("seed", fixtureFiles)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := work.RemoteMain("road"); err != nil || got != head {
		t.Errorf("got %q, %v, want %q", got, err, head)
	}
	if _, err := work.RemoteMain("none"); err == nil {
		t.Error("a road that is not there was not refused")
	}
}

func TestRemoteMainIgnoresOtherBranches(t *testing.T) {
	t.Parallel()
	work, runner := mocked(t)
	runner.EXPECT().Run("/work", "", nil,
		[]string{"ls-remote", "road", "refs/heads/main"}).Return("", nil)
	if got, err := work.RemoteMain("road"); err != nil || got != "" {
		t.Errorf("a road with only work: %q, %v", got, err)
	}
}
