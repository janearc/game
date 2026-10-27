package sticky_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sticky"
)

// rootUID is the superuser's user id, as unix has defined it.
const rootUID = 0

// says is whether err is an error that says want.
func says(err error, want string) bool {
	return err != nil && strings.Contains(err.Error(), want)
}

// unborn is a repository with no commit yet: a HEAD that names nothing.
func unborn(t *testing.T) repo.Repo {
	t.Helper()
	fresh := repoAt(t.TempDir())
	if _, err := fresh.Git("init", "-q", "-b", "work"); err != nil {
		t.Fatal(err)
	}
	return fresh
}

// refusing makes the clone's next commit fail, by a pre-commit hook in
// the hooks directory the fixture gives it.
func refusing(t *testing.T, clone repo.Repo) {
	t.Helper()
	hooks, err := clone.Git("config", "core.hooksPath")
	if err != nil {
		t.Fatal(err)
	}
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(hooks, openDir); err != nil {
		t.Fatal(err)
	}
	openScript := os.FileMode(0o755)
	hook := filepath.Join(hooks, "pre-commit")
	if err := os.WriteFile(hook,
		[]byte("#!/bin/sh\necho refused by the hook >&2\nexit 1\n"),
		openScript); err != nil {
		t.Fatal(err)
	}
}

// TestBehindCountsWhatTheRoadHas: the commits on the road's branch this
// tree lacks, none for a branch the road does not have, and an error,
// counting nothing, for a road that cannot be read.
func TestBehindCountsWhatTheRoadHas(t *testing.T) {
	t.Parallel()
	one, two, road := fixture(t)
	onBranch(t, two, "work")
	newWork := []struct{ file, body string }{
		{"b.txt", "b\n"}, {"c.txt", "c\n"},
	}
	for _, piece := range newWork {
		commit(t, two, piece.file, piece.body, piece.file)
	}
	push(t, two, road, "work")
	if behind, err := sticky.Behind(one, road, "work"); err != nil ||
		behind != len(newWork) {
		t.Errorf("the pushed commits: %d, %v", behind, err)
	}
	if behind, err := sticky.Behind(one, road, "nowhere"); err != nil ||
		behind != count.Nothing {
		t.Errorf("a branch the road lacks: %d, %v", behind, err)
	}
	gone := filepath.Join(t.TempDir(), "gone.git")
	if behind, err := sticky.Behind(one, gone, "work"); err == nil ||
		behind != count.Nothing || !says(err, "git ls-remote") {
		t.Errorf("a road that is not there: %d, %v", behind, err)
	}
	if behind, err := sticky.Behind(unborn(t), road, "work"); err == nil ||
		behind != count.Nothing || !says(err, "git rev-list") {
		t.Errorf("a tree with no HEAD to count from: %d, %v", behind, err)
	}
}

// TestBehindRefusesARoadThatCannotGiveTheBranch: a road that lists the
// branch but cannot send its commit is an error, not a count.
func TestBehindRefusesARoadThatCannotGiveTheBranch(t *testing.T) {
	t.Parallel()
	one, two, road := fixture(t)
	onBranch(t, two, "work")
	commit(t, two, "b.txt", "b\n", "b")
	push(t, two, road, "work")
	tip, err := two.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(road, "objects", tip[:fanout],
		tip[fanout:])
	if err := os.Remove(loose); err != nil {
		t.Fatalf("the tip is not a loose object on the road: %v", err)
	}
	if behind, err := sticky.Behind(one, road, "work"); err == nil ||
		behind != count.Nothing || !says(err, "git fetch") {
		t.Errorf("a road missing its tip: %d, %v", behind, err)
	}
}

// TestSyncRefusesWhatItCannotRead: a road that is not there, or a tree
// with no HEAD, is an error before anything moves.
func TestSyncRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	gone := filepath.Join(t.TempDir(), "gone.git")
	if _, moved, err := sticky.Sync(one, gone, "main"); moved ||
		!says(err, "git fetch") {
		t.Errorf("a road that is not there: moved %v, %v", moved, err)
	}
	if _, moved, err := sticky.Sync(unborn(t), road, "main"); moved ||
		!says(err, "git rev-parse HEAD") {
		t.Errorf("a tree with no HEAD: moved %v, %v", moved, err)
	}
}

// TestStickRefusesATreeWithNoBranch: with no commit there is no branch
// to claim on, and nothing is written.
func TestStickRefusesATreeWithNoBranch(t *testing.T) {
	t.Parallel()
	_, _, road := fixture(t)
	fresh := unborn(t)
	if _, err := sticky.Stick(fresh, road, "ada"); !says(err,
		"git rev-parse") {
		t.Error("a tree with no commit was stuck")
	}
	if _, err := os.Stat(fresh.Path(stickyFile)); err == nil {
		t.Error("a refused stick wrote the claim")
	}
}

// refusal is one way unstick cannot, by name: what sets it up, whether
// there is a claim to fail on, what the refusal says, and whether the
// claim stays.
type refusal struct {
	name  string
	set   func(*testing.T, repo.Repo)
	stick bool
	want  string
	stuck bool
}

// check is the refusal as a sub-test: unstick refuses in its words,
// and a claim that could not be taken away is still there.
func (way refusal) check(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	if way.stick {
		if _, err := sticky.Stick(one, road, "ada"); err != nil {
			t.Fatal(err)
		}
	}
	if way.set != nil {
		way.set(t, one)
	}
	_, err := sticky.Unstick(one)
	if err == nil || !strings.Contains(err.Error(), way.want) {
		t.Errorf("%v, want %q", err, way.want)
	}
	if _, err := os.Stat(one.Path(stickyFile)); way.stuck && err != nil {
		t.Errorf("the claim is gone: %v", err)
	}
}

// TestUnstickRefusesEachWayItCannot: a tree not stuck, a claim that
// cannot be read, removed, staged or committed, each refuses, and a
// claim that could not be taken away is still there.
func TestUnstickRefusesEachWayItCannot(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == rootUID {
		t.Skip("root removes through a read-only directory")
	}
	ways := []refusal{
		{name: "not stuck", want: "not stuck; nothing to unstick"},
		{
			name: "a claim that cannot be read",
			set:  mkdirClaim,
			want: "is a directory",
		},
		{
			name:  "a claim that cannot be removed",
			set:   readOnly,
			stick: true,
			want:  "permission denied",
			stuck: true,
		},
		{
			name:  "a claim that cannot be staged",
			set:   lockIndex,
			stick: true,
			want:  "git add",
		},
		{
			name:  "a removal that cannot be committed",
			set:   refusing,
			stick: true,
			want:  "refused by the hook",
		},
	}
	for _, way := range ways {
		t.Run(way.name, way.check)
	}
}

// mkdirClaim puts a directory where the claim would be.
func mkdirClaim(t *testing.T, clone repo.Repo) {
	t.Helper()
	openDir := os.FileMode(0o755)
	if err := os.Mkdir(clone.Path(stickyFile), openDir); err != nil {
		t.Fatal(err)
	}
}

// lockIndex holds the index, so the removal cannot be staged.
func lockIndex(t *testing.T, clone repo.Repo) {
	t.Helper()
	lock := clone.Path(".git", "index.lock")
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(lock, nil, openFile); err != nil {
		t.Fatal(err)
	}
}

// shutTree is the clone's directory made read-only until the test ends.
type shutTree struct {
	dir  string
	open os.FileMode
}

// reopen gives the directory back, so the temporary tree can be
// removed.
func (shut shutTree) reopen() {
	_ = os.Chmod(shut.dir, shut.open)
}

// readOnly makes the clone's directory read-only until the test ends.
func readOnly(t *testing.T, clone repo.Repo) {
	t.Helper()
	shut := shutTree{dir: clone.Dir(), open: os.FileMode(0o755)}
	shutMode := os.FileMode(0o555)
	if err := os.Chmod(shut.dir, shutMode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shut.reopen)
}
