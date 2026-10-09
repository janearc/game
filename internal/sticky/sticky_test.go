package sticky

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/repo"
)

// fixture is a bare road, standing in for the shared remote, and two
// clones of it, standing in for two people. each clone has its own git
// identity, so a commit in one is never mistaken for the other's.
func fixture(t *testing.T) (one, two repo.Repo, road string) {
	t.Helper()
	dir := t.TempDir()
	road = filepath.Join(dir, "road.git")
	git := func(d string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git(dir, "init", "-q", "--bare", road)
	clone := func(name string) repo.Repo {
		at := filepath.Join(dir, name)
		git(dir, "clone", "-q", road, at)
		git(at, "config", "commit.gpgsign", "false")
		git(at, "config", "user.email", name+"@example.invalid")
		git(at, "config", "user.name", name)
		// a container has no hooks of its own, but this machine's git
		// config may, and a commit-msg hook that appends a signature
		// would make these throwaway repositories a property of the
		// operator's environment rather than of the code. pointing
		// hooks at a directory with none in it is what makes the
		// suite the same test everywhere it runs.
		git(at, "config", "core.hooksPath", filepath.Join(dir, "no-hooks"))
		return repo.Repo{Dir: at}
	}
	// the road needs one commit before it has a branch to clone: an
	// empty bare repository has no HEAD for a clone to check out.
	seed := filepath.Join(dir, "seed")
	os.MkdirAll(seed, 0o755)
	git(seed, "init", "-q", "-b", "main")
	git(seed, "config", "commit.gpgsign", "false")
	git(seed, "config", "user.email", "seed@example.invalid")
	git(seed, "config", "user.name", "seed")
	git(seed, "config", "core.hooksPath", filepath.Join(dir, "no-hooks"))
	os.WriteFile(filepath.Join(seed, "a.txt"), []byte("a\n"), 0o644)
	git(seed, "add", "a.txt")
	git(seed, "commit", "-q", "-m", "seed")
	git(seed, "push", road, "main")
	return clone("one"), clone("two"), road
}

// commit is a plain commit of whatever is in the working tree, under
// the clone's own identity.
func commit(t *testing.T, r repo.Repo, path, body, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Dir, path), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Git("add", path); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GitIn("", "commit", "-q", "-m", message); err != nil {
		t.Fatal(err)
	}
}

func push(t *testing.T, r repo.Repo, road, branch string) {
	t.Helper()
	if _, err := r.Git("push", road, branch); err != nil {
		t.Fatalf("push: %v", err)
	}
}

// onBranch checks out a new branch, off whatever the clone is on now,
// since stick refuses to claim main and most of these tests need a
// branch that is not it.
func onBranch(t *testing.T, r repo.Repo, name string) {
	t.Helper()
	if _, err := r.Git("checkout", "-q", "-b", name); err != nil {
		t.Fatalf("checkout -b %s: %v", name, err)
	}
}

// TestStickWritesAndCommitsTheClaim proves the claim is a committed
// file at the root, there for the other clone to see once it is
// pushed, and that it names the holder.
func TestStickWritesAndCommitsTheClaim(t *testing.T) {
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	claim, err := Stick(one, road, "grace")
	if err != nil {
		t.Fatalf("stick: %v", err)
	}
	if claim.Holder != "grace" || claim.Branch != "work" {
		t.Errorf("claim = %+v", claim)
	}
	body, err := os.ReadFile(filepath.Join(one.Dir, Path))
	if err != nil || strings.TrimSpace(string(body)) != "grace" {
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

// TestStickRefusesOnMain is the explicit rule: a claim never lands on
// main, whatever else is true of the repository.
func TestStickRefusesOnMain(t *testing.T) {
	one, _, road := fixture(t)
	if _, err := Stick(one, road, "grace"); err == nil {
		t.Fatal("stuck main")
	}
	if _, ok, _ := Stuck(one.Dir); ok {
		t.Error(".sticky was written even though stick refused")
	}
}

// TestStickRefusesWhenAlreadyHeld is the baton rule: a second claim
// beside the first is refused, and names whoever holds it, once the
// first clone's claim has reached the road, because the claim is
// visible there, not only in the clone that wrote it.
func TestStickRefusesWhenAlreadyHeld(t *testing.T) {
	one, two, road := fixture(t)
	onBranch(t, one, "work")
	onBranch(t, two, "work")
	commit(t, one, "work.txt", "one\n", "some work")
	if _, err := Stick(one, road, "grace"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	push(t, one, road, "work")

	if _, err := Stick(two, road, "hedy"); err == nil {
		t.Fatal("stuck a repository someone else already holds")
	} else if !strings.Contains(err.Error(), "grace") {
		t.Errorf("refusal does not name the holder: %v", err)
	}
	if _, ok, _ := Stuck(two.Dir); ok {
		t.Error("the second clone wrote a claim of its own")
	}
}

// TestStickRefusesWhenAlreadyHeldLocally covers the same clone: a
// second stick on top of one you already hold is refused too, rather
// than silently replacing the claim.
func TestStickRefusesWhenAlreadyHeldLocally(t *testing.T) {
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	if _, err := Stick(one, road, "grace"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	if _, err := Stick(one, road, "grace"); err == nil {
		t.Fatal("stuck twice in the same clone")
	}
}

// TestHolderScansEveryBranch is the per-repository rule: a claim on a
// branch other than the one checked out still answers the question,
// because the baton is not scoped to a branch.
func TestHolderScansEveryBranch(t *testing.T) {
	one, two, road := fixture(t)
	onBranch(t, one, "feature")
	if _, err := Stick(one, road, "grace"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	push(t, one, road, "feature")

	// two is on main, never feature, and still finds the claim.
	claim, held, err := Holder(two, road)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	if !held || claim.Holder != "grace" || claim.Branch != "feature" {
		t.Errorf("holder = %+v, %v", claim, held)
	}
}

// TestSyncFastForwards is the pairing verb: it takes what the other
// clone just pushed, and says whether anything moved.
func TestSyncFastForwards(t *testing.T) {
	one, two, road := fixture(t)
	commit(t, one, "work.txt", "one\n", "some work")
	push(t, one, road, "main")

	tip, moved, err := Sync(two, road, "main")
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
	if _, err := os.Stat(filepath.Join(two.Dir, "work.txt")); err != nil {
		t.Error("the merged work never reached the working tree")
	}

	// nothing new: a second sync is a no-op, not an error.
	_, moved, err = Sync(two, road, "main")
	if err != nil || moved {
		t.Errorf("second sync: moved=%v err=%v", moved, err)
	}
}

// TestSyncRefusesOnDivergedHistories is the detector the ff-only rule
// gives us: two clones that both committed since the last sync cannot
// be fast-forwarded, which is proof two people held the baton at once,
// and it is refused and reported rather than merged for them.
func TestSyncRefusesOnDivergedHistories(t *testing.T) {
	one, two, road := fixture(t)
	commit(t, one, "from-one.txt", "one\n", "one's work")
	push(t, one, road, "main")
	commit(t, two, "from-two.txt", "two\n", "two's work")

	if _, _, err := Sync(two, road, "main"); err == nil {
		t.Fatal("fast-forwarded over diverged histories")
	}
	if clean, _ := two.Clean(); !clean {
		t.Error("a refused sync left the tree dirty")
	}
}

// TestUnstickRemovesAndCommits is the pass: the file is gone, and the
// removal itself is a commit, not a dirty working tree.
func TestUnstickRemovesAndCommits(t *testing.T) {
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	if _, err := Stick(one, road, "grace"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	tip, err := Unstick(one)
	if err != nil {
		t.Fatalf("unstick: %v", err)
	}
	if _, ok, _ := Stuck(one.Dir); ok {
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
	one, _, _ := fixture(t)
	if _, err := Unstick(one); err == nil {
		t.Fatal("unstuck a repository that was never stuck")
	}
}

// TestStickRefusesADetachedHead: her review checkouts leave a tree
// detached, and a claim committed there lives on no branch, cannot be
// pushed, and blocks the next stick with "already stuck here". it is
// refused before anything is written.
func TestStickRefusesADetachedHead(t *testing.T) {
	one, _, road := fixture(t)
	if _, err := one.Git("checkout", "-q", "--detach"); err != nil {
		t.Fatal(err)
	}
	before, _ := one.Git("rev-parse", "HEAD")
	_, err := Stick(one, road, "grace")
	if err == nil {
		t.Fatal("stuck a detached head")
	}
	if !strings.Contains(err.Error(), "detached") {
		t.Errorf("the refusal does not say why: %v", err)
	}
	if _, ok, _ := Stuck(one.Dir); ok {
		t.Error(".sticky was written even though stick refused")
	}
	if after, _ := one.Git("rev-parse", "HEAD"); after != before {
		t.Error("stick committed on the detached head")
	}
}

// TestStickAndUnstickCommitOnlyTheClaim: a person mid-work has things
// staged. the claim is its own commit and leaves theirs staged, both
// on the way in and on the way out; otherwise half-done work rides
// into a commit called "stick".
func TestStickAndUnstickCommitOnlyTheClaim(t *testing.T) {
	one, _, road := fixture(t)
	onBranch(t, one, "work")
	stage := func(name string) {
		t.Helper()
		if err := os.WriteFile(
			filepath.Join(one.Dir, name), []byte("half\n"), 0o644,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := one.Git("add", name); err != nil {
			t.Fatal(err)
		}
	}
	only := func(verb string) {
		t.Helper()
		files, _ := one.Git("show", "--name-only", "--format=", "HEAD")
		if strings.TrimSpace(files) != Path {
			t.Errorf("%s committed %q, not only %s", verb, files, Path)
		}
		staged, _ := one.Git("diff", "--cached", "--name-only")
		if !strings.Contains(staged, "half-done") {
			t.Errorf("%s took the staged work with it", verb)
		}
	}

	stage("half-done-in.txt")
	if _, err := Stick(one, road, "grace"); err != nil {
		t.Fatalf("stick: %v", err)
	}
	only("stick")

	if _, err := one.Git("commit", "-q", "-m", "the work"); err != nil {
		t.Fatal(err)
	}
	stage("half-done-out.txt")
	if _, err := Unstick(one); err != nil {
		t.Fatalf("unstick: %v", err)
	}
	only("unstick")
}

// TestSyncSaysDirtyNotDiverged: the commonest state in pairing is an
// uncommitted edit to a file the other person just changed, because
// you build to try your edit. that is a dirty tree behind the road,
// not a diverged one, and the refusal says so in git's words and
// leaves the edit alone.
func TestSyncSaysDirtyNotDiverged(t *testing.T) {
	one, two, road := fixture(t)
	commit(t, one, "a.txt", "one's version\n", "one changes a")
	push(t, one, road, "main")
	mine := []byte("two's edit, not committed\n")
	if err := os.WriteFile(
		filepath.Join(two.Dir, "a.txt"), mine, 0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, _, err := Sync(two, road, "main")
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
	if body, _ := os.ReadFile(filepath.Join(two.Dir, "a.txt")); string(body) != string(mine) {
		t.Errorf("the edit was not left alone: %q", body)
	}

	// and a tree that truly diverged is still called that.
	if _, err := two.Git("checkout", "-q", "--", "a.txt"); err != nil {
		t.Fatal(err)
	}
	commit(t, two, "b.txt", "two\n", "two's own commit")
	if _, _, err := Sync(two, road, "main"); err == nil ||
		!strings.Contains(err.Error(), "diverged") {
		t.Errorf("a diverged tree was not called diverged: %v", err)
	}
}
