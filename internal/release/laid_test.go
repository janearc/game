package release_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/release"
)

// diffOf is the patch that changes one file of a repository from what is
// committed to body, every byte of it, and leaves the repository as it was.
func diffOf(t *testing.T, dir, file, body string) string {
	t.Helper()
	addFiles(t, dir, map[string]string{file: body})
	cmd := exec.Command("git", "diff", "HEAD~1", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "reset", "-q", "--hard", "HEAD~1")
	return string(out)
}

// the stack's own patches for a member are laid on it in name order, the
// second on what the first made. a patch ending in a blank line of
// context lands whole, and the count of patches laid is kept.
func TestStackLaysItsOwnPatchesInNameOrder(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	at := memberDir(own, "a")
	addFiles(t, at, map[string]string{"spaced.txt": "one\n\n"})
	first := diffOf(t, at, "spaced.txt", "two\n\n")
	if !strings.HasSuffix(first, "\n \n") {
		t.Fatalf("the first patch does not end in blank context:\n%q",
			first)
	}
	addFiles(t, at, map[string]string{"spaced.txt": "two\n\n"})
	second := diffOf(t, at, "spaced.txt", "three\n\n")
	gitIn(t, at, "reset", "-q", "--hard", "HEAD~1")
	addFiles(t, own.Dir(), map[string]string{
		"patches/a/0001-two.patch":   first,
		"patches/a/0002-three.patch": second,
		"patches/a/notes.md":         "not a patch\n",
		// below the member's own directory, so never laid: it would fail
		"patches/a/old/0001-gone.patch": "diff --git a/gone b/gone\n" +
			"--- a/gone\n+++ b/gone\n@@ -1 +1 @@\n-x\n+y\n",
	})
	out, err := release.Stack(own, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	object, _ := own.Git("rev-parse", out.Commit()+":a/spaced.txt")
	if body, _ := own.Blob(object); body != "three\n\n" {
		t.Errorf("a/spaced.txt is %q, want the two patches laid", body)
	}
	// two patches, the second on the first, and no more
	if out.Patched("a") != count.Next(count.One) {
		t.Errorf("patched %d, want the two", out.Patched("a"))
	}
}

// a patch that does not apply refuses the stack, naming the member and
// the patch, and leaves no release ref.
func TestStackRefusesAPatchThatDoesNotApply(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	addFiles(t, own.Dir(), map[string]string{
		"patches/b/0001-nowhere.patch": "diff --git a/gone.go b/gone.go\n" +
			"--- a/gone.go\n+++ b/gone.go\n@@ -1 +1 @@\n-x\n+y\n",
	})
	_, err := release.Stack(own, stackOptions(dist), members)
	want := "b: patches/b/0001-nowhere.patch does not apply"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err %v, want %q", err, want)
	}
	if kept, _ := own.Git("rev-parse", "--verify", "-q",
		refOf("v1")); kept != "" {
		t.Errorf("a refused stack kept %s", kept)
	}
}

// a member marked theirs that comes first, before any allowance is
// known, is allowed its own paths all the same.
func TestStackAllowsTheirsFirst(t *testing.T) {
	t.Parallel()
	own, dist, _ := stackFixture(t)
	addFiles(t, memberDir(own, "b"), map[string]string{
		"leak.txt": stackLeak + "\n",
	})
	theirs := takenAs(own, "b", true)
	if _, err := release.Stack(own, stackOptions(dist),
		[]release.Member{theirs}); err != nil {
		t.Errorf("theirs, laid first: %v", err)
	}
}

// a member marked theirs may carry its own addresses and paths. a
// never-word or the author in it is still refused, and so is one a patch
// of the stack's own puts there.
func TestStackHoldsTheirsToNeverWordsAndTheAuthorOnly(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	second := count.Next(count.First)
	members[second] = takenAs(own, "b", true)
	addFiles(t, memberDir(own, "b"), map[string]string{
		"leak.txt": stackLeak + "\n",
		// assembled here, so this file spells no address of its own
		"owners.md": "write to maintainers" + "@" + "example.com\n",
	})
	if _, err := release.Stack(own, stackOptions(dist), members); err != nil {
		t.Fatalf("theirs, with its own path and address: %v", err)
	}
	own.Git("update-ref", "-d", refOf("v1"))
	for name, body := range map[string]string{
		"thanks.md": "with thanks to Zorblax\n",
		"AUTHORS":   "by ada lovelace\n",
	} {
		addFiles(t, memberDir(own, "b"), map[string]string{name: body})
		_, err := release.Stack(own, stackOptions(dist), members)
		if err == nil || !strings.Contains(err.Error(), "b/"+name) {
			t.Errorf("%s in theirs: %v, want it refused", name, err)
		}
		gitIn(t, memberDir(own, "b"), "reset", "-q", "--hard", "HEAD~1")
	}
	patched := diffOf(t, memberDir(own, "b"), "b.go", "package b // Zorblax\n")
	addFiles(t, own.Dir(), map[string]string{
		filepath.Join("patches", "b", "0001-word.patch"): patched,
	})
	_, err := release.Stack(own, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(),
		"a never-word in b/b.go") {
		t.Errorf("a patched never-word in theirs: %v, want it refused", err)
	}
}

// two members with example modules of one path make a go.work go
// refuses: the stack is refused, naming both. marked theirs, each gives
// its own top module alone, and the workspace holds just the two.
func TestAModuleTwiceIsRefusedAndTheirsGiveTheirTopsAlone(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	for _, name := range []string{"a", "b"} {
		addFiles(t, memberDir(own, name), map[string]string{
			"examples/go.mod":  "module examples\n\ngo 1.25\n",
			"examples/main.go": "package main\n\nfunc main() {}\n",
		})
	}
	_, err := release.Stack(own, stackOptions(dist), members)
	want := "examples in a/examples and b/examples"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err %v, want %q", err, want)
	}
	theirs := []release.Member{takenAs(own, "a", true), takenAs(own, "b", true)}
	out, err := release.Stack(own, stackOptions(dist), theirs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Work(), "examples") ||
		!strings.Contains(out.Work(), "\t./a\n\t./b\n") {
		t.Errorf("go.work:\n%s", out.Work())
	}
}

// what a member's own .game omits from its releases, a file or a
// directory, is not in the stack either; the rest of the member is.
func TestStackOmitsWhatAMemberOmits(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	addFiles(t, memberDir(own, "b"), map[string]string{
		".game":            "name b\nomit NOTES.md\nomit = drafts\n",
		"NOTES.md":         "with thanks to Zorblax\n",
		"drafts/first.md":  "by ada lovelace\n",
		"drafts/second.md": "by ada lovelace\n",
	})
	out, err := release.Stack(own, stackOptions(dist), members)
	if err != nil {
		t.Fatalf("a member's omitted notes refused the stack: %v", err)
	}
	files := filesAt(t, own, out.Commit())
	got := strings.Join(files, " ")
	if strings.Contains(got, "b/NOTES.md") ||
		strings.Contains(got, "b/drafts") || !strings.Contains(got, "b/b.go") {
		t.Errorf("files %v", files)
	}
}

// a patch whose name git would quote, with an accent in it, is laid
// like any other: game patch and game build stack lay it, so the release
// does too.
func TestStackLaysAPatchWhoseNameGitQuotes(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	created := "diff --git a/new.txt b/new.txt\nnew file mode 100644\n" +
		"--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+made\n"
	addFiles(t, own.Dir(), map[string]string{
		"patches/a/0001-café.patch": created,
	})
	out, err := release.Stack(own, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := own.Show(out.Commit(), "a/new.txt"); body != "made" ||
		out.Patched("a") != count.One {
		t.Errorf("a/new.txt %q, patched %d", body, out.Patched("a"))
	}
}
