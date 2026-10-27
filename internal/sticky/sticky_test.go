package sticky_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sticky"
)

// execBits are a mode's execute bits, as unix has defined them.
const execBits = 0o111

// stickyFile is the Claim's name, committed at the repository's root.
// the tests name it themselves, so a rename in the code fails them.
const stickyFile = ".sticky"

// git runs git in dir, outside the package under test, and answers what
// it said, stopping the test on an error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// clone is a clone of the road with its own git identity, so a commit
// in one is never mistaken for the other's.
func clone(t *testing.T, dir, road, name string) repo.Repo {
	t.Helper()
	at := filepath.Join(dir, name)
	git(t, dir, "clone", "-q", road, at)
	git(t, at, "config", "commit.gpgsign", "false")
	git(t, at, "config", "user.email", name+"@example.invalid")
	git(t, at, "config", "user.name", name)
	// a container has no hooks of its own, but this machine's git
	// config may, and a hook that appends a signature would make these
	// throwaway repositories the operator's, not the code's. pointing
	// hooks at an empty directory makes the suite the same everywhere.
	git(t, at, "config", "core.hooksPath", filepath.Join(dir, "no-hooks"))
	return repoAt(at)
}

// fixture is a bare road, standing in for the shared remote, and two
// clones of it, standing in for two people.
func fixture(t *testing.T) (one, two repo.Repo, road string) {
	t.Helper()
	dir := t.TempDir()
	road = filepath.Join(dir, "road.git")
	git(t, dir, "init", "-q", "--bare", road)
	// the road needs one commit before it has a branch to clone: an
	// empty bare repository has no HEAD for a clone to check out.
	seed := filepath.Join(dir, "seed")
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(seed, openDir); err != nil {
		t.Fatal(err)
	}
	git(t, seed, "init", "-q", "-b", "main")
	git(t, seed, "config", "commit.gpgsign", "false")
	git(t, seed, "config", "user.email", "seed@example.invalid")
	git(t, seed, "config", "user.name", "seed")
	git(t, seed, "config", "core.hooksPath", filepath.Join(dir, "no-hooks"))
	openFile := os.FileMode(0o644)
	claim := filepath.Join(seed, "a.txt")
	if err := os.WriteFile(claim, []byte("a\n"), openFile); err != nil {
		t.Fatal(err)
	}
	git(t, seed, "add", "a.txt")
	git(t, seed, "commit", "-q", "-m", "seed")
	git(t, seed, "push", road, "main")
	return clone(t, dir, road, "one"), clone(t, dir, road, "two"), road
}

// commit is a plain commit of whatever is in the working tree, under
// the clone's own identity.
func commit(t *testing.T, work repo.Repo, path, body, message string) {
	t.Helper()
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(work.Path(path), []byte(body), openFile); err != nil {
		t.Fatal(err)
	}
	if _, err := work.Git("add", path); err != nil {
		t.Fatal(err)
	}
	if _, err := work.GitIn("", "commit", "-q", "-m", message); err != nil {
		t.Fatal(err)
	}
}

func push(t *testing.T, work repo.Repo, road, branch string) {
	t.Helper()
	if _, err := work.Git("push", road, branch); err != nil {
		t.Fatalf("push: %v", err)
	}
}

// onBranch checks out a new branch, off whatever the clone is on now,
// since stick refuses to Claim main and most of these tests need a
// branch that is not it.
func onBranch(t *testing.T, work repo.Repo, name string) {
	t.Helper()
	if _, err := work.Git("checkout", "-q", "-b", name); err != nil {
		t.Fatalf("checkout -b %s: %v", name, err)
	}
}

// TestStickWritesAndCommitsTheClaim proves the Claim is a committed
// file at the root, there for the other clone to see once it is
// pushed, and that it names the holder.
func TestStickWritesAndCommitsTheClaim(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	made, err := sticky.Stick(one, road, "ada")
	if err != nil {
		t.Fatalf("stick: %v", err)
	}
	if made.Holder() != "ada" || made.Branch() != "work" {
		t.Errorf("Claim = %+v", made)
	}
	body, err := os.ReadFile(one.Path(stickyFile))
	if err != nil || strings.TrimSpace(string(body)) != "ada" {
		t.Errorf(".sticky = %q, %v", body, err)
	}
	if clean, _ := one.Clean(); !clean {
		t.Error("stick left the tree dirty; it should have committed")
	}
	msg, _ := one.Git("log", "-1", "--format=%B")
	if strings.TrimSpace(msg) != "stick" {
		t.Errorf("commit message = %q", msg)
	}
}

// TestStickRefusesOnMain is the explicit rule: a Claim never lands on
// main, whatever else is true of the repository.
func TestStickRefusesOnMain(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	if _, err := sticky.Stick(one, road, "ada"); err == nil {
		t.Fatal("stuck main")
	}
	if _, ok, _ := sticky.Stuck(one.Dir()); ok {
		t.Error(".sticky was written even though stick refused")
	}
}

// TestStickRefusesWhenAlreadyHeld is the baton rule: a second Claim
// beside the first is refused, and names whoever holds it, once the
// first clone's Claim has reached the road, because the Claim is
// visible there, not only in the clone that wrote it.
func TestStickRefusesWhenAlreadyHeld(t *testing.T) {
	t.Parallel()
	one, two, road := fixture(t)
	onBranch(t, one, "work")
	onBranch(t, two, "work")
	commit(t, one, "work.txt", "one\n", "some work")
	if _, err := sticky.Stick(one, road, "ada"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	push(t, one, road, "work")

	tip, err := one.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	_, err = sticky.Stick(two, road, "partner")
	if err == nil {
		t.Fatal("stuck a repository someone else already holds")
	}
	// the refusal names the holder, the branch and the commit's first
	// seven, so the second person can see where the baton is.
	for _, word := range []string{"ada", "work"} {
		if !strings.Contains(err.Error(), word) {
			t.Errorf("refusal lacks %q: %v", word, err)
		}
	}
	if !strings.HasSuffix(err.Error(), " at "+repo.Short(tip)) {
		t.Errorf("refusal does not end on the commit's seven: %v", err)
	}
	if _, ok, _ := sticky.Stuck(two.Dir()); ok {
		t.Error("the second clone wrote a Claim of its own")
	}
}

// TestStickRefusesARoadItCannotAsk: a road that is not there is an
// error before anything is written, not an empty answer.
func TestStickRefusesARoadItCannotAsk(t *testing.T) {
	t.Parallel()
	one, _, _ := fixture(t)
	onBranch(t, one, "work")
	nowhere := filepath.Join(t.TempDir(), "nowhere.git")
	if _, err := sticky.Stick(one, nowhere, "ada"); err == nil {
		t.Fatal("stuck against a road that is not there")
	}
	if _, ok, _ := sticky.Stuck(one.Dir()); ok {
		t.Error(".sticky was written even though the road was not asked")
	}
}

// TestStickRefusesOutsideARepository: a directory git does not know is
// refused before a Claim is written into it.
func TestStickRefusesOutsideARepository(t *testing.T) {
	t.Parallel()
	_, _, road := fixture(t)
	plain := t.TempDir()
	if _, err := sticky.Stick(repoAt(plain), road, "ada"); err == nil {
		t.Fatal("stuck a directory that is not a repository")
	}
	if _, err := os.Stat(filepath.Join(plain, stickyFile)); err == nil {
		t.Error(".sticky was written outside a repository")
	}
}

// TestStickCommitsAPlainFile: the Claim is committed as an ordinary
// file, not an executable one.
func TestStickCommitsAPlainFile(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	if _, err := sticky.Stick(one, road, "ada"); err != nil {
		t.Fatal(err)
	}
	staged, err := one.Git("ls-files", "-s", "--", stickyFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(staged, "100644 ") {
		t.Errorf("the Claim is committed as %q", staged)
	}
}

// TestStickWritesAPlainFile: the Claim on disk is not executable by
// anyone. git keeps only the owner's execute bit, so the test above cannot
// see a Claim that is executable by everyone else; this one can.
func TestStickWritesAPlainFile(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	if _, err := sticky.Stick(one, road, "ada"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(one.Path(stickyFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&execBits != count.Nothing {
		t.Errorf("the Claim is written %v", perm)
	}
}

// TestStickRefusedByAHookLeavesHeadAlone: when the commit itself is
// refused, by a hook here, stick is an error and HEAD has not moved.
func TestStickRefusedByAHookLeavesHeadAlone(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	hooks := t.TempDir()
	hook := filepath.Join(hooks, "pre-commit")
	refuse := []byte("#!/bin/sh\nexit 1\n")
	openScript := os.FileMode(0o755)
	if err := os.WriteFile(hook, refuse, openScript); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Git("config", "core.hooksPath", hooks); err != nil {
		t.Fatal(err)
	}
	before, err := one.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sticky.Stick(one, road, "ada"); err == nil {
		t.Fatal("stick succeeded though the commit was refused")
	}
	if after, _ := one.Git("rev-parse", "HEAD"); after != before {
		t.Error("a refused stick moved HEAD")
	}
}

// TestStickRefusesWhenAlreadyHeldLocally covers the same clone: a
// second stick on top of one you already hold is refused too, rather
// than silently replacing the Claim.
func TestStickRefusesWhenAlreadyHeldLocally(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	if _, err := sticky.Stick(one, road, "ada"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	if _, err := sticky.Stick(one, road, "ada"); err == nil {
		t.Fatal("stuck twice in the same clone")
	}
}

// TestStickSeesAClaimOnAnotherBranch is the per-repository rule: a Claim
// on a branch other than the one checked out still stops a second stick,
// because the baton is not scoped to a branch. the refusal names who
// holds it and where.
func TestStickSeesAClaimOnAnotherBranch(t *testing.T) {
	t.Parallel()
	one, two, road := fixture(t)
	onBranch(t, one, "feature")
	if _, err := sticky.Stick(one, road, "ada"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	push(t, one, road, "feature")

	// two is on a branch of its own, never feature, and still finds the
	// Claim.
	onBranch(t, two, "other")
	_, err := sticky.Stick(two, road, "ada")
	if err == nil || !strings.Contains(err.Error(), "ada already holds") ||
		!strings.Contains(err.Error(), "stuck on feature") {
		t.Errorf("stick over another branch's Claim: %v", err)
	}
	if _, statErr := os.Stat(two.Path(stickyFile)); statErr == nil {
		t.Error("a refused stick left a Claim file behind")
	}
}

// TestSyncFastForwards is the pairing verb: it takes what the other
// clone just pushed, and says whether anything moved.
func TestSyncFastForwards(t *testing.T) {
	t.Parallel()
	one, two, road := fixture(t)
	commit(t, one, "work.txt", "one\n", "some work")
	push(t, one, road, "main")

	tip, moved, err := sticky.Sync(two, road, "main")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !moved {
		t.Error("sync reported nothing moved, but the road had more")
	}
	head, _ := two.Git("rev-parse", "HEAD")
	if tip != head {
		t.Errorf("tip = %s, HEAD = %s", tip, head)
	}
	if _, err := os.Stat(two.Path("work.txt")); err != nil {
		t.Error("the merged work never reached the working tree")
	}

	// nothing new: a second sync is a no-op, not an error.
	_, moved, err = sticky.Sync(two, road, "main")
	if err != nil || moved {
		t.Errorf("second sync: moved=%v err=%v", moved, err)
	}
}

// TestSyncRefusesOnDivergedHistories is the detector the ff-only rule
// gives us: two clones that both committed since the last sync cannot
// be fast-forwarded, which is proof two people held the baton at once,
// and it is refused and reported rather than merged for them.
func TestSyncRefusesOnDivergedHistories(t *testing.T) {
	t.Parallel()
	one, two, road := fixture(t)
	commit(t, one, "from-one.txt", "one\n", "one's work")
	push(t, one, road, "main")
	commit(t, two, "from-two.txt", "two\n", "two's work")

	if _, _, err := sticky.Sync(two, road, "main"); err == nil {
		t.Fatal("fast-forwarded over diverged histories")
	}
	if clean, _ := two.Clean(); !clean {
		t.Error("a refused sync left the tree dirty")
	}
}

// TestSyncLeavesWorkAheadOfTheRoad: the one holding the Claim commits
// before pushing, and a build syncs. the road has nothing this tree
// lacks, so nothing is taken and nothing is refused.
func TestSyncLeavesWorkAheadOfTheRoad(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	commit(t, one, "ahead.txt", "one\n", "not pushed yet")
	head, _ := one.Git("rev-parse", "HEAD")

	tip, moved, err := sticky.Sync(one, road, "main")
	if err != nil || moved || tip != head {
		t.Errorf("ahead of the road: tip=%s moved=%v err=%v, HEAD %s",
			tip, moved, err, head)
	}
}

// TestUnstickRemovesAndCommits is the pass: the file is gone, and the
// removal itself is a commit, not a dirty working tree.
func TestUnstickRemovesAndCommits(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	if _, err := sticky.Stick(one, road, "ada"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	tip, err := sticky.Unstick(one)
	if err != nil {
		t.Fatalf("unstick: %v", err)
	}
	if _, ok, _ := sticky.Stuck(one.Dir()); ok {
		t.Error(".sticky is still there after unstick")
	}
	head, _ := one.Git("rev-parse", "HEAD")
	if tip != head {
		t.Errorf("tip = %s, HEAD = %s", tip, head)
	}
	if clean, _ := one.Clean(); !clean {
		t.Error("unstick left the tree dirty")
	}
	msg, _ := one.Git("log", "-1", "--format=%B")
	if strings.TrimSpace(msg) != "unstick" {
		t.Errorf("commit message = %q", msg)
	}
}

// TestUnstickRefusesWhenNotStuck: there is nothing to let go of.
func TestUnstickRefusesWhenNotStuck(t *testing.T) {
	t.Parallel()
	one, _, _ := fixture(t)
	if _, err := sticky.Unstick(one); err == nil {
		t.Fatal("unstuck a repository that was never stuck")
	}
}

// TestStickRefusesADetachedHead: her review checkouts leave a tree
// detached, and a Claim committed there lives on no branch, cannot be
// pushed, and blocks the next stick with "already stuck here". it is
// refused before anything is written.
func TestStickRefusesADetachedHead(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	if _, err := one.Git("checkout", "-q", "--detach"); err != nil {
		t.Fatal(err)
	}
	before, _ := one.Git("rev-parse", "HEAD")
	_, err := sticky.Stick(one, road, "ada")
	if err == nil {
		t.Fatal("stuck a detached head")
	}
	if !strings.Contains(err.Error(), "detached") {
		t.Errorf("the refusal does not say why: %v", err)
	}
	if _, ok, _ := sticky.Stuck(one.Dir()); ok {
		t.Error(".sticky was written even though stick refused")
	}
	if after, _ := one.Git("rev-parse", "HEAD"); after != before {
		t.Error("stick committed on the detached head")
	}
}

// stage writes half-done work and stages it.
func stage(t *testing.T, one repo.Repo, name string) {
	t.Helper()
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(one.Path(name), []byte("half\n"), openFile); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Git("add", name); err != nil {
		t.Fatal(err)
	}
}

// onlyTheClaim checks the verb committed the Claim alone and left the
// staged work staged.
func onlyTheClaim(t *testing.T, one repo.Repo, verb string) {
	t.Helper()
	files, _ := one.Git("show", "--name-only", "--format=", "HEAD")
	if strings.TrimSpace(files) != stickyFile {
		t.Errorf("%s committed %q, not only %s", verb, files, stickyFile)
	}
	staged, _ := one.Git("diff", "--cached", "--name-only")
	if !strings.Contains(staged, "half-done") {
		t.Errorf("%s took the staged work with it", verb)
	}
}

// TestStickAndUnstickCommitOnlyTheClaim: a person mid-work has things
// staged. the Claim is its own commit and leaves theirs staged, both
// on the way in and on the way out; otherwise half-done work rides
// into a commit called "stick".
func TestStickAndUnstickCommitOnlyTheClaim(t *testing.T) {
	t.Parallel()
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	stage(t, one, "half-done-in.txt")
	if _, err := sticky.Stick(one, road, "ada"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	onlyTheClaim(t, one, "stick")

	if _, err := one.Git("commit", "-q", "-m", "the work"); err != nil {
		t.Fatal(err)
	}
	stage(t, one, "half-done-out.txt")
	if _, err := sticky.Unstick(one); err != nil {
		t.Fatalf("unstick: %v", err)
	}
	onlyTheClaim(t, one, "unstick")
}

// TestSyncSaysDirtyNotDiverged: the commonest state in pairing is an
// uncommitted edit to a file the other person just changed, because
// you build to try your edit. that is a dirty tree behind the road,
// not a diverged one, and the refusal says so in git's words and
// leaves the edit alone.
func TestSyncSaysDirtyNotDiverged(t *testing.T) {
	t.Parallel()
	one, two, road := fixture(t)
	commit(t, one, "a.txt", "one's version\n", "one changes a")
	push(t, one, road, "main")
	mine := []byte("two's edit, not committed\n")
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(two.Path("a.txt"), mine, openFile); err != nil {
		t.Fatal(err)
	}

	_, _, err := sticky.Sync(two, road, "main")
	if err == nil {
		t.Fatal("fast-forwarded over an uncommitted edit")
	}
	text := err.Error()
	if strings.Contains(text, "diverged") {
		t.Errorf("a dirty tree was called diverged: %v", err)
	}
	if !strings.Contains(text, "a.txt") ||
		!strings.Contains(text, "local changes") {
		t.Errorf("the refusal does not carry git's words: %v", err)
	}
	if body, _ := os.ReadFile(two.Path("a.txt")); string(body) != string(mine) {
		t.Errorf("the edit was not left alone: %q", body)
	}

	// and a tree that truly diverged is still called that.
	if _, err := two.Git("checkout", "-q", "--", "a.txt"); err != nil {
		t.Fatal(err)
	}
	commit(t, two, "b.txt", "two\n", "two's own commit")
	if _, _, err := sticky.Sync(two, road, "main"); err == nil ||
		!strings.Contains(err.Error(), "diverged") {
		t.Errorf("a diverged tree was not called diverged: %v", err)
	}
}

// TestStickRefusesARoadThatWillNotGiveABranch: the road lists the
// Claim's branch, but the commit behind it is gone, so a fetch fails.
// that is an error, not "nobody holds it": the second person is not
// handed a baton the road cannot vouch for.
func TestStickRefusesARoadThatWillNotGiveABranch(t *testing.T) {
	t.Parallel()
	one, two, road := fixture(t)
	onBranch(t, one, "work")
	onBranch(t, two, "work")
	if _, err := sticky.Stick(one, road, "ada"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	push(t, one, road, "work")
	tip, err := one.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	object := filepath.Join(road, "objects", tip[:fanout],
		tip[fanout:])
	if err := os.Remove(object); err != nil {
		t.Fatal(err)
	}
	_, err = sticky.Stick(two, road, "partner")
	if err == nil || !strings.Contains(err.Error(), "will not give") {
		t.Fatalf("a road that will not give its branch: %v", err)
	}
	if _, ok, _ := sticky.Stuck(two.Dir()); ok {
		t.Error("the second clone wrote a Claim of its own")
	}
}

// fanout is where a loose object's directory name ends in its hash: two
// characters, defined by git's object store.
const fanout = 2
