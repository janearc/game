package main

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/stack"
)

// the patches the tests lay on a member's f, a b c: one that lands, and
// one that lands nowhere.
const (
	lands    = "--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"
	misfires = "--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n a\n-zzz\n+yyy\n c\n"
)

// stackLines is what a dotfile and a .game say of a stack, in lines, until
// they are read as a config.
type stackLines struct {
	blocks map[string][]string
	refs   map[string]string
}

// load reads the lines as Load would, the members in name order.
func (lines stackLines) load(t *testing.T) config.Config {
	t.Helper()
	var dotfile, game []string
	for _, name := range slices.Sorted(maps.Keys(lines.blocks)) {
		dotfile = append(dotfile, lines.blocks[name]...)
	}
	for _, name := range slices.Sorted(maps.Keys(lines.refs)) {
		game = append(game, "stack "+name+" "+lines.refs[name])
	}
	return loadedAs(t, "", dotfile, game)
}

// patched is a stack root and its config: members m and n, each a road
// with f at v1, and the stack's patches as given, by member and name.
func patched(t *testing.T, patches map[string]string) (repo.Repo,
	config.Config) {
	t.Helper()
	work, lines := patchedStack(t, patches)
	return work, lines.load(t)
}

// patchedStack is patched before its lines are read.
func patchedStack(t *testing.T, patches map[string]string) (repo.Repo,
	stackLines) {
	t.Helper()
	root := t.TempDir()
	lines := stackLines{blocks: map[string][]string{}, refs: map[string]string{}}
	for _, name := range []string{"m", "n"} {
		at := filepath.Join(root, name+"-work")
		work := gitTree(t, at,
			map[string]string{"f": "a\nb\nc\n"})
		gitAt(t, work.Dir(), "branch", "v1")
		bare := filepath.Join(root, name+".git")
		gitAt(t, root, "clone", "-q", "--bare", work.Dir(), bare)
		lines.refs[name] = "v1"
		lines.blocks[name] = block(name, bare, bare)
	}
	stackRoot := repoAt(filepath.Join(root, "stack"))
	for path, body := range patches {
		write(t, stackRoot.Path("patches", path), body)
	}
	return stackRoot, lines
}

// cloned is f in a member's clone under bin/patch.
func cloned(t *testing.T, work repo.Repo, name string) string {
	t.Helper()
	got, err := os.ReadFile(work.Path("bin", "patch", name, "f"))
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

// patch lays each member's patches on a fresh clone and says each that
// landed, and of a member with none that it has none; --dry says the
// same and leaves the clone as it was.
func TestPatchSaysWhatLandedAndWhatHadNone(t *testing.T) {
	t.Parallel()
	for _, each := range []struct {
		args       []string
		said, left string
	}{
		{nil, "patch m: 01.patch lands\n", "a\nB\nc\n"},
		{[]string{"--dry"}, "patch m: 01.patch would land\n", "a\nb\nc\n"},
		{[]string{"--from", "road"}, "patch m: 01.patch lands\n",
			"a\nB\nc\n"},
	} {
		work, cfg := patched(t, map[string]string{"m/01.patch": lands})
		var out bytes.Buffer
		if err := verbTo(t, "patch", patchVerb, work, cfg, each.args, &out); err != nil {
			t.Fatalf("%v: %v", each.args, err)
		}
		want := each.said + "patch n: no patches\n"
		if out.String() != want {
			t.Errorf("%v: said %q, want %q", each.args, out.String(), want)
		}
		if got := cloned(t, work, "m"); got != each.left {
			t.Errorf("%v: f is %q, want %q", each.args, got, each.left)
		}
	}
}

// each member whose patches did not land is said and counted, and a
// patch that landed in a series that did not is said to be taken back.
func TestPatchCountsTheMembersThatDidNotLand(t *testing.T) {
	t.Parallel()
	work, cfg := patched(t, map[string]string{"m/01.patch": lands,
		"m/02.patch": misfires, "n/01.patch": misfires})
	var out bytes.Buffer
	err := verbTo(t, "patch", patchVerb, work, cfg, nil, &out)
	if err == nil ||
		err.Error() != "patch: 2 member's patches did not land" {
		t.Errorf("both: %v", err)
	}
	for _, want := range []string{
		"patch m: 01.patch landed, then taken back with the series\n",
		"patch: 02.patch does not apply",
		"patch: 01.patch does not apply",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("want %q in:\n%s", want, out.String())
		}
	}
	err = verbTo(t, "patch", patchVerb, work, cfg, []string{"n"}, &bytes.Buffer{})
	if err == nil ||
		err.Error() != "patch: 1 member's patches did not land" {
		t.Errorf("one: %v", err)
	}
}

// names limit patch to those members, and a name that is none of them
// is refused.
func TestPatchIsLimitedToTheMembersNamed(t *testing.T) {
	t.Parallel()
	work, cfg := patched(t, map[string]string{"m/01.patch": lands})
	var out bytes.Buffer
	if err := verbTo(t, "patch", patchVerb, work, cfg, []string{"n"}, &out); err != nil ||
		out.String() != "patch n: no patches\n" {
		t.Errorf("%v, said %q", err, out.String())
	}
	err := verbTo(t, "patch", patchVerb, work, cfg, []string{"zzz"}, &bytes.Buffer{})
	if err == nil ||
		err.Error() != "patch: zzz is not a member of this stack" {
		t.Errorf("a stranger: %v", err)
	}
}

// with no stack, or --from that is neither, there is nothing to start.
func TestPatchRefusesWhatItCannotStartOn(t *testing.T) {
	t.Parallel()
	work, cfg := patched(t, nil)
	err := verbTo(t, "patch", patchVerb, work, blankConfig(), nil, &bytes.Buffer{})
	if err == nil || !strings.HasPrefix(err.Error(), "no stack described") {
		t.Errorf("no stack: %v", err)
	}
	err = verbTo(t, "patch", patchVerb, work, cfg, []string{"--from", "x"}, &bytes.Buffer{})
	if err == nil || err.Error() != `--from is road or dist, not "x"` {
		t.Errorf("--from x: %v", err)
	}
}

// spoiler spoils one member of a patched stack.
type spoiler func(*testing.T, repo.Repo, *stackLines)

// noDistForM takes m's block away.
func noDistForM(_ *testing.T, _ repo.Repo, lines *stackLines) {
	delete(lines.blocks, "m")
}

// cloneAtV9 asks for m at a ref its road lacks.
func cloneAtV9(_ *testing.T, _ repo.Repo, lines *stackLines) {
	lines.refs["m"] = "v9"
}

// patchesAFile makes m's patches a file.
func patchesAFile(t *testing.T, work repo.Repo, _ *stackLines) {
	_ = os.RemoveAll(work.Path("patches", "m"))
	write(t, work.Path("patches", "m"), "a file")
}

// cloneStuck leaves a clone of m from before that cannot be taken away.
func cloneStuck(t *testing.T, work repo.Repo, _ *stackLines) {
	stuck := work.Path("bin", "patch", "m", "sub")
	write(t, filepath.Join(stuck, "x"), "x")
	shut := os.FileMode(0o555)
	if err := os.Chmod(stuck, shut); err != nil {
		t.Fatal(err)
	}
	open := os.FileMode(0o755)
	t.Cleanup(shutDir{dir: stuck, open: open}.reopen)
}

// a member with no dist, a ref its road lacks, patches that are a file,
// or a clone that will not go is said and counted, and the rest go on.
func TestPatchSaysWhyAMemberCouldNotBeHad(t *testing.T) {
	t.Parallel()
	// modes refuse nothing to root, so that case needs another user
	spoilsByMode := !asRoot()
	spoils := map[string]spoiler{
		"no dist for m in":  noDistForM,
		"clone at v9":       cloneAtV9,
		"patches":           patchesAFile,
		"permission denied": cloneStuck,
	}
	for name, spoil := range spoils {
		if name == "permission denied" && !spoilsByMode {
			t.Log("a mode refuses nothing to root; the case is left out")
			continue
		}
		work, lines := patchedStack(t, map[string]string{
			"m/01.patch": lands, "n/01.patch": lands})
		spoil(t, work, &lines)
		cfg := lines.load(t)
		var out bytes.Buffer
		err := verbTo(t, "patch", patchVerb, work, cfg, nil, &out)
		if err == nil ||
			err.Error() != "patch: 1 member's patches did not land" ||
			!strings.Contains(out.String(), name) ||
			!strings.Contains(out.String(), "patch n: 01.patch lands") {
			t.Errorf("%s: %v, said:\n%s", name, err, out.String())
		}
	}
}

// a clone left from before is taken away, and the member cloned fresh.
func TestPatchClonesFresh(t *testing.T) {
	t.Parallel()
	work, cfg := patched(t, map[string]string{"m/01.patch": lands})
	old := work.Path("bin", "patch", "m", "old")
	write(t, old, "from before")
	if err := verbTo(t, "patch", patchVerb, work, cfg, []string{"m"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("the clone from before is still there")
	}
}

// laid refuses patches it cannot read, and a series that does not land.
func TestLaidRefusesWhatDoesNotLand(t *testing.T) {
	t.Parallel()
	member := stack.Named("m", "")
	for name, patches := range map[string]map[string]string{
		"a file":   {"m": "a file, not a directory"},
		"misfires": {"m/01.patch": misfires},
	} {
		work, _ := patched(t, patches)
		clone := freshTree(t, "clone",
			map[string]string{"f": "a\nb\nc\n"})
		lay := laid(work.Dir(), nil)
		if laidCount, err := lay(member, clone.Dir()); err == nil {
			t.Errorf("%s: laid %d", name, laidCount)
		}
	}
}
