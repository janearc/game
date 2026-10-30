package release_test

import (
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/release"
)

// a stack that keeps a member's patches under a directory its .game names
// has them laid from the commit there, as from patches/NAME.
func TestStackLaysPatchesFromANamedDirectory(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	at := memberDir(own, "a")
	addFiles(t, at, map[string]string{"named.txt": "one\n"})
	change := diffOf(t, at, "named.txt", "laid\n")
	addFiles(t, own.Dir(), map[string]string{
		"shared/patches/a/0001-laid.patch": change,
		"shared/patches/a/0001-laid.md":    "what it's for\n",
	})
	options := stackOptions(dist).PatchedFrom([]string{"shared/patches"})
	out, err := release.Stack(own, options, members)
	if err != nil {
		t.Fatal(err)
	}
	object, _ := own.Git("rev-parse", out.Commit()+":a/named.txt")
	if body, _ := own.Blob(object); body != "laid\n" {
		t.Errorf("a/named.txt is %q, want the patch laid", body)
	}
	if out.Patched("a") != count.One {
		t.Errorf("patched %d, want the one", out.Patched("a"))
	}
}

// without the directory named, what's under it is only files: nothing is
// laid from it.
func TestStackLaysNothingFromAnUnnamedDirectory(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	at := memberDir(own, "a")
	addFiles(t, at, map[string]string{"named.txt": "one\n"})
	change := diffOf(t, at, "named.txt", "laid\n")
	addFiles(t, own.Dir(), map[string]string{
		"shared/patches/a/0001-laid.patch": change,
	})
	out, err := release.Stack(own, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	if !patchedNone(out) {
		t.Errorf("patched from a directory .game didn't name")
	}
}

// patchedNone is whether the stack laid no patches anywhere.
func patchedNone(out *release.Stacked) bool {
	for _, name := range []string{"a", "b"} {
		if count.Positive(out.Patched(name)) {
			return false
		}
	}
	return true
}

// a member with patches in both patches/NAME and a named directory is
// refused, naming both, and leaves no release ref.
func TestStackRefusesPatchesInTwoPlaces(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	at := memberDir(own, "a")
	addFiles(t, at, map[string]string{"named.txt": "one\n"})
	change := diffOf(t, at, "named.txt", "laid\n")
	addFiles(t, own.Dir(), map[string]string{
		"patches/a/0001-laid.patch":        change,
		"shared/patches/a/0001-laid.patch": change,
	})
	options := stackOptions(dist).PatchedFrom([]string{"shared/patches"})
	_, err := release.Stack(own, options, members)
	want := "a's patches are in both patches/a and shared/patches/a"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err %v, want %q", err, want)
	}
	if kept, _ := own.Git("rev-parse", "--verify", "-q",
		refOf("v1")); kept != "" {
		t.Errorf("a refused stack kept %s", kept)
	}
}

// a stack with no commit to read refuses its patches, naming the commit,
// instead of reading as a member with none.
func TestSeriesSaysWhenTheCommitCantBeRead(t *testing.T) {
	work, dist, members := stackFixture(t)
	releaseGitFailure(t, "[ \"$1\" = ls-tree ] && [ \"$4\" = HEAD ]")
	_, err := release.Stack(work, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(), "the stack's commit") {
		t.Errorf("err %v, want the commit named", err)
	}
}

// the first probe: a place that is the stack's root reads nothing of a
// member's own, though the member, laid under its name, has a patch at
// its top. only the stack's own commit holds the stack's patches.
func TestStackNeverReadsAMembersTopAsItsPatches(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	at := memberDir(own, "a")
	addFiles(t, at, map[string]string{"named.txt": "one\n"})
	change := diffOf(t, at, "named.txt", "laid\n")
	addFiles(t, at, map[string]string{"0001-own.patch": change})
	options := stackOptions(dist).PatchedFrom([]string{"."})
	out, err := release.Stack(own, options, members)
	if err != nil {
		t.Fatal(err)
	}
	if !patchedNone(out) {
		t.Error("patched from a member's own files")
	}
}

// the second probe: a place inside member a reads nothing of a's own
// patches/b for member b, laid after it.
func TestStackNeverReadsOneMembersPatchesForAnother(t *testing.T) {
	t.Parallel()
	own, dist, members := stackFixture(t)
	at := memberDir(own, "b")
	addFiles(t, at, map[string]string{"named.txt": "one\n"})
	change := diffOf(t, at, "named.txt", "laid\n")
	addFiles(t, memberDir(own, "a"), map[string]string{
		"patches/b/0001-x.patch": change,
	})
	options := stackOptions(dist).PatchedFrom([]string{"a/patches"})
	out, err := release.Stack(own, options, members)
	if err != nil {
		t.Fatal(err)
	}
	if !patchedNone(out) {
		t.Error("patched from member a's own tree")
	}
}
