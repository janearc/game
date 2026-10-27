package patch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/patch"
)

// the three patches the tests lay: two that land in turn, and one that
// lands nowhere.
const (
	first = "--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"
	then  = "--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n a\n B\n-c\n+C\n"
	wrong = "--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n a\n-zzz\n+yyy\n c\n"
)

// adds makes two new files: one git tracks once added, and one the
// member's .gitignore ignores.
const adds = "diff --git a/added b/added\nnew file mode 100644\n" +
	"--- /dev/null\n+++ b/added\n@@ -0,0 +1 @@\n+x\n" +
	"diff --git a/added.gen b/added.gen\nnew file mode 100644\n" +
	"--- /dev/null\n+++ b/added.gen\n@@ -0,0 +1 @@\n+x\n"

// member is a clone with a committed file, f, and a .gitignore of *.gen,
// and a stack root beside it whose patches/member holds the patches.
func member(t *testing.T, patches map[string]string) (string, string) {
	t.Helper()
	for _, name := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(name, "game")
	}
	for _, email := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(email, "game@test")
	}
	dir, root := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "f"), "a\nb\nc\n")
	write(t, filepath.Join(dir, ".gitignore"), "*.gen\n")
	for _, step := range [][]string{{"init", "-q"}, {"add", "f", ".gitignore"},
		{"commit", "-q", "-m", "the tag"}} {
		run(t, dir, step...)
	}
	for name, body := range patches {
		writeIn(t, filepath.Join(root, "patches", "member"), name, body)
	}
	return dir, root
}

func write(t *testing.T, path, body string) {
	t.Helper()
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
		t.Fatal(err)
	}
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(path, []byte(body), openFile); err != nil {
		t.Fatal(err)
	}
}

// writeIn writes body to name under dir, making the directories.
func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()
	write(t, filepath.Join(dir, name), body)
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git",
		append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func file(t *testing.T, dir string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "f"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(body), "\n", " ")
}

func lay(t *testing.T, dir, root string, dry bool) ([]patch.Result, error) {
	t.Helper()
	series, err := patch.Series(root, nil, "member")
	if err != nil {
		t.Fatal(err)
	}
	return patch.Lay(dir, series, dry)
}

// a series lands in name order, each patch on the one before it.
func TestASeriesLandsInOrder(t *testing.T) {
	patches := map[string]string{"02-then.patch": then,
		"01-first.patch": first}
	dir, root := member(t, patches)
	results, err := lay(t, dir, root, false)
	if err != nil || len(results) != len(patches) ||
		file(t, dir) != "a B C " {
		t.Errorf("results %v, err %v, f is %q", results, err, file(t, dir))
	}
}

// when a later patch does not land, the earlier ones are taken back too,
// and the error names the one that failed.
func TestASeriesLandsWholeOrNotAtAll(t *testing.T) {
	patches := map[string]string{"01-first.patch": first,
		"02-then.patch": then, "03-wrong.patch": wrong}
	dir, root := member(t, patches)
	results, err := lay(t, dir, root, false)
	if err == nil || !strings.Contains(err.Error(), "03-wrong.patch") {
		t.Errorf("err %v, want it to name 03-wrong.patch", err)
	}
	last := count.Last(len(results))
	if file(t, dir) != "a b c " || len(results) != len(patches) ||
		results[last].Landed() {
		t.Errorf("results %v, f is %q; want the clone as it was",
			results, file(t, dir))
	}
}

// dry says each patch would land and leaves the clone as it was.
func TestDryLeavesTheCloneAsItWas(t *testing.T) {
	patches := map[string]string{"01-first.patch": first,
		"02-then.patch": then}
	dir, root := member(t, patches)
	results, err := lay(t, dir, root, true)
	second := count.Next(count.First)
	if err != nil || len(results) != len(patches) ||
		!results[count.First].Landed() || !results[second].Landed() ||
		file(t, dir) != "a b c " {
		t.Errorf("results %v, err %v, f is %q", results, err, file(t, dir))
	}
}

// a tree with changes in it is refused before anything is laid, so a
// reset never takes anyone's work.
func TestATreeWithChangesIsRefused(t *testing.T) {
	dir, root := member(t, map[string]string{"01-first.patch": first})
	write(t, filepath.Join(dir, "f"), "mine\n")
	_, err := lay(t, dir, root, false)
	if err == nil || !strings.Contains(err.Error(), "has changes") ||
		file(t, dir) != "mine " {
		t.Errorf("err %v, f is %q; want it refused and untouched",
			err, file(t, dir))
	}
}

// a series that fails takes back the files its earlier patches added,
// ignored ones too, under dry and not. the review asked for it.
func TestAFailedSeriesTakesBackWhatItAdded(t *testing.T) {
	for _, dry := range []bool{false, true} {
		dir, root := member(t, map[string]string{
			"01-adds.patch": adds, "02-wrong.patch": wrong})
		if _, err := lay(t, dir, root, dry); err == nil {
			t.Errorf("dry %v: the wrong patch was let through", dry)
		}
		for _, name := range []string{"added", "added.gen"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				t.Errorf("dry %v: %s was left behind", dry, name)
			}
		}
	}
}

// a directory inside another tree is refused, so a reset never lands on
// the tree that holds it.
func TestADirectoryInsideATreeIsRefused(t *testing.T) {
	dir, root := member(t, map[string]string{"01-first.patch": first})
	inside := filepath.Join(dir, "inside")
	write(t, filepath.Join(inside, "f"), "a\nb\nc\n")
	run(t, dir, "add", "inside")
	run(t, dir, "commit", "-q", "-m", "a directory inside")
	_, err := lay(t, inside, root, false)
	if err == nil || !strings.Contains(err.Error(), "not a clone") {
		t.Errorf("err %v, want it refused as not a clone of its own", err)
	}
	if file(t, inside) != "a b c " || file(t, dir) != "a b c " {
		t.Error("something was laid or reset")
	}
}

// a member with no patches is laid with nothing, and that is fine.
func TestNoPatchesIsNothingToDo(t *testing.T) {
	dir, root := member(t, nil)
	results, err := lay(t, dir, root, false)
	if err != nil || count.Positive(len(results)) ||
		file(t, dir) != "a b c " {
		t.Errorf("results %v, err %v", results, err)
	}
}
