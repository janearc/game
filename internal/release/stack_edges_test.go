package release_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
)

// stackOf is a stack's own repository, an empty bare dist, and one
// member made of the files given, named name.
func stackOf(
	t *testing.T,
	name string,
	files map[string]string,
) (repo.Repo, string, []release.Member) {
	t.Helper()
	root := t.TempDir()
	own := filepath.Join(root, "own")
	gitRepo(t, own, map[string]string{"README.md": "the stack\n"})
	dist := filepath.Join(root, "dist.git")
	gitIn(t, root, "init", "-q", "--bare", dist)
	at := filepath.Join(root, name)
	gitRepo(t, at, files)
	members := []release.Member{release.Taken(name, "main", at, false)}
	return repoAt(own), dist, members
}

// submoduled gives the member at url a submodule named vendored.
func submoduled(t *testing.T, url string) {
	t.Helper()
	head := gitIn(t, url, "rev-parse", "HEAD")
	gitIn(t, url, "update-index", "--add", "--cacheinfo",
		"160000,"+head+",vendored")
	gitIn(t, url, "commit", "-q", "-m", "a submodule")
}

// TestAWorkspaceIsWrittenOnlyForModules: members with no go.mod make no
// go.work; one module makes one; a file that only ends in go.mod is not
// a module.
func TestAWorkspaceIsWrittenOnlyForModules(t *testing.T) {
	t.Parallel()
	work, dist, members := stackOf(t, "plain", map[string]string{
		"notes.txt": "no module here\n",
	})
	out, err := release.Stack(work, stackOptions(dist), members)
	if err != nil || out.Work() != "" {
		t.Fatalf("no module: work %q, %v", out.Work(), err)
	}
	if files := filesAt(t, work, out.Commit()); slices.Contains(files,
		"go.work") {
		t.Error("a go.work with nothing to use")
	}
	work, dist, members = stackOf(t, "m", map[string]string{
		"go.mod":     "module example.com/m\n\ngo 1.25\n",
		"m.go":       "package m\n",
		"old-go.mod": "module example.com/old\n",
	})
	out, err = release.Stack(work, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Work(), "./m\n") ||
		strings.Contains(out.Work(), "old-") {
		t.Errorf("one module, and a file named like one: %q", out.Work())
	}
}

// TestTheCallersAllowancesStandAlone: an allowance the caller gives
// holds when no member has any of its own.
func TestTheCallersAllowancesStandAlone(t *testing.T) {
	t.Parallel()
	work, dist, members := stackOf(t, "c", map[string]string{
		"README.md": "c: " + stackLeak + "\n",
	})
	options := stackOptions(dist).Swept(sworn, nil,
		map[string][]string{"c/README.md": {"paths"}})
	if _, err := release.Stack(work, options, members); err != nil {
		t.Errorf("allowed by the caller: %v", err)
	}
}

// TestAMembersSubmoduleIsRefused: a submodule in a member stops the
// stack, whether the sweep meets it or, excluded from the sweep, the
// look for setting values does.
func TestAMembersSubmoduleIsRefused(t *testing.T) {
	t.Parallel()
	work, dist, members := stackOf(t, "s", map[string]string{
		"README.md": "s\n",
	})
	submoduled(t, memberDir(work, "s"))
	if _, err := release.Stack(work, stackOptions(dist),
		members); err == nil ||
		!strings.Contains(err.Error(), "vendored") {
		t.Errorf("swept: %v, want a refusal naming vendored", err)
	}
	work, dist, members = stackOf(t, "e", map[string]string{
		".game": "name e\nexclude vendored\n",
	})
	submoduled(t, memberDir(work, "e"))
	options := stackOptions(dist).Refusing([]string{"a-value"})
	if _, err := release.Stack(work, options, members); err == nil ||
		!strings.Contains(err.Error(), "vendored") {
		t.Errorf("excluded, values sought: %v, want vendored named", err)
	}
}

// TestAStackRefusesWhatItCannotStandOn: a dist that is not there, a
// member named for a directory the root already has, and a root with no
// commit each stop the stack before a ref is kept.
func TestAStackRefusesWhatItCannotStandOn(t *testing.T) {
	t.Parallel()
	work, _, members := stackOf(t, "x", map[string]string{"x.txt": "x\n"})
	gone := filepath.Join(t.TempDir(), "gone.git")
	if _, err := release.Stack(work, stackOptions(gone),
		members); err == nil ||
		!strings.Contains(err.Error(), "ls-remote") {
		t.Errorf("no dist: %v, want ls-remote's refusal", err)
	}
	work, dist, members := stackOf(t, "taken", map[string]string{
		"t.txt": "t\n",
	})
	// a root directory of the member's name is refused even with no path
	// in common, where git would merge the two without a word
	addFiles(t, work.Dir(), map[string]string{"taken/here.txt": "the root's\n"})
	if _, err := release.Stack(work, stackOptions(dist),
		members); err == nil ||
		!strings.HasPrefix(err.Error(), "taken: the stack already holds") {
		t.Errorf("a name taken: %v, want it refused naming taken", err)
	}
	twice := append(members, members[count.First])
	work, dist, _ = stackOf(t, "x", map[string]string{"x.txt": "x\n"})
	if _, err := release.Stack(work, stackOptions(dist),
		twice); err == nil ||
		!strings.HasPrefix(err.Error(), "taken: the stack already holds") {
		t.Errorf("one member twice: %v, want the second refused", err)
	}
	unborn := repoAt(t.TempDir())
	if _, err := unborn.Git("init", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := release.Stack(unborn, stackOptions(dist),
		members); err == nil {
		t.Error("a root with no commit was stacked")
	}
	if kept, _ := work.Git("rev-parse", "--verify", "-q",
		refOf("v1")); kept != "" {
		t.Errorf("a refused stack kept %s", kept)
	}
}

// TestAStackNeedsATemporaryDirectory: the scratch index is made under
// TMPDIR, and one that is not there is an error. it sets TMPDIR, so it
// runs alone.
func TestAStackNeedsATemporaryDirectory(t *testing.T) {
	work, dist, members := stackOf(t, "x", map[string]string{"x.txt": "x\n"})
	notHere := filepath.Join(t.TempDir(), "not-here")
	t.Setenv("TMPDIR", notHere)
	if _, err := release.Stack(work, stackOptions(dist),
		members); err == nil {
		t.Error("a stack made its index with nowhere to put it")
	}
}

// memberDir is where the fixture made the member called name: beside
// the stack's own repository.
func memberDir(work repo.Repo, name string) string {
	beside := filepath.Dir(work.Dir())
	return filepath.Join(beside, name)
}

// takenAs is the fixture's member called name, taken from main, theirs or
// not.
func takenAs(work repo.Repo, name string, theirs bool) release.Member {
	return release.Taken(name, "main", memberDir(work, name), theirs)
}
