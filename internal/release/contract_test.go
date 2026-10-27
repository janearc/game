package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
)

func releaseGitFailure(t *testing.T, condition string) {
	t.Helper()
	actual, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	body := "#!/bin/sh\nif " + condition + "; then\n" +
		"echo 'release boundary refused' >&2; exit 1\nfi\n" +
		"exec \"$RELEASE_REAL_GIT\" \"$@\"\n"
	mode := os.FileMode(0o755)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RELEASE_REAL_GIT", actual)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// releaseGitSays stands a git in front of the real one that answers with
// the given line, and succeeds, whenever the condition holds.
func releaseGitSays(t *testing.T, condition, answer string) {
	t.Helper()
	actual, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	body := "#!/bin/sh\nif " + condition + "; then\necho " + answer + "; exit 0\nfi\n" +
		"exec \"$RELEASE_REAL_GIT\" \"$@\"\n"
	mode := os.FileMode(0o755)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RELEASE_REAL_GIT", actual)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestReleaseOmissionAndTreeIdentityAgreeWithDiff(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	options := release.For("sample", dist, "v1").Swept(sworn, nil, nil)
	options = options.Omitting([]string{"a.txt"})
	difference, err := release.Diff(work, options)
	if err != nil {
		t.Fatal(err)
	}
	result, err := release.Prepare(work, options)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := work.Tree(result.Commit())
	if err != nil || result.Tree() != actual || difference.Tree() != actual {
		t.Fatalf("prepared tree %q, diff %q, actual %q: %v",
			result.Tree(), difference.Tree(), actual, err)
	}
	if _, err := work.Show(result.Commit(), "a.txt"); err == nil {
		t.Fatal("omitted file reached the release")
	}
	if body, err := work.Show(result.Commit(), "b.txt"); err != nil || body != "world" {
		t.Fatalf("non-omitted file lost: %q %v", body, err)
	}
}

type diffRefusal string

func (condition diffRefusal) check(t *testing.T) {
	work, dist := fixture(t)
	options := release.For("sample", dist, "v1").Swept(sworn, nil, nil)
	releaseGitFailure(t, string(condition))
	if _, err := release.Diff(work, options); err == nil ||
		!strings.Contains(err.Error(), "release boundary refused") {
		t.Fatalf("diff hid a failed boundary: %v", err)
	}
}

func TestDiffPropagatesEveryGitBoundaryFailure(t *testing.T) {
	for _, condition := range []diffRefusal{
		"[ \"$1\" = hash-object ]",
		"[ \"$1\" = diff ] && [ \"$2\" = --stat ]",
		"[ \"$1\" = diff ] && [ \"$2\" != --stat ]",
	} {
		t.Run(string(condition), condition.check)
	}
}

type prepareRefusal string

func (condition prepareRefusal) check(t *testing.T) {
	work, dist := fixture(t)
	options := release.For("sample", dist, "v1").Swept(sworn, nil, nil)
	releaseGitFailure(t, string(condition))
	if _, err := release.Prepare(work, options); err == nil ||
		!strings.Contains(err.Error(), "release boundary refused") {
		t.Fatalf("prepare hid a failed boundary: %v", err)
	}
	if _, err := work.Git("rev-parse", "--verify", refOf("v1")); err == nil {
		t.Fatal("failed release left a prepared ref")
	}
}

func TestPreparePropagatesEveryGitBoundaryFailure(t *testing.T) {
	for _, condition := range []prepareRefusal{
		"[ \"$1\" = status ]", "[ \"$1\" = commit-tree ]",
		"[ \"$1\" = log ]",
	} {
		t.Run(string(condition), condition.check)
	}
}

type stackRefusal string

func (condition stackRefusal) check(t *testing.T) {
	work, dist, members := stackFixture(t)
	before := gitIn(t, work.Dir(), "rev-parse", "HEAD")
	index := gitIn(t, work.Dir(), "write-tree")
	releaseGitFailure(t, string(condition))
	if _, err := release.Stack(work, stackOptions(dist), members); err == nil ||
		!strings.Contains(err.Error(), "release boundary refused") {
		t.Fatalf("stack hid a failed boundary: %v", err)
	}
	if _, err := work.Git("show-ref", "--verify", refOf("v1")); err == nil {
		t.Fatal("failed stack left a prepared release ref")
	}
	actual := os.Getenv("RELEASE_REAL_GIT")
	command := exec.Command(actual, "-C", work.Dir(), "rev-parse", "HEAD")
	head, err := command.Output()
	if err != nil || strings.TrimSpace(string(head)) != before {
		t.Fatalf("failed stack moved HEAD: %q %v", head, err)
	}
	command = exec.Command(actual, "-C", work.Dir(), "write-tree")
	after, err := command.Output()
	if err != nil || strings.TrimSpace(string(after)) != index {
		t.Fatalf("failed stack changed the real index: %q %v", after, err)
	}
}

func TestStackPropagatesGitFailuresWithoutChangingTheSource(t *testing.T) {
	for _, condition := range []stackRefusal{
		"[ \"$1\" = ls-files ]",
		"[ \"$1\" = rev-parse ] && [ \"$2\" = 'refs/game/stack/a^{tree}' ]",
		"[ \"$1\" = read-tree ] && [ \"$2\" = --prefix=a/ ]",
		"[ \"$1\" = hash-object ] && [ \"$2\" = -w ]",
		"[ \"$1\" = update-index ]",
		"[ \"$1\" = write-tree ] && [ $# = 1 ]",
		"[ \"$1\" = write-tree ] && [ $# != 1 ]",
		"[ \"$1\" = commit-tree ]",
		"[ \"$1\" = status ]",
		"[ \"$1\" = log ]",
	} {
		t.Run(string(condition), condition.check)
	}
}

func TestStackRefusesUnreadablePatchObject(t *testing.T) {
	work, dist, members := stackFixture(t)
	body := diffOf(t, memberDir(work, "a"), "go.mod", "module example.invalid/changed\n")
	addFiles(t, work.Dir(), map[string]string{"patches/a/change.patch": body})
	releaseGitFailure(t, "[ \"$1\" = cat-file ] && [ \"$2\" = blob ]")
	_, err := release.Stack(work, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(), "patches/a/change.patch") ||
		!strings.Contains(err.Error(), "release boundary refused") {
		t.Fatalf("unreadable patch object: %v", err)
	}
}

func TestStackRefusesFailedMemberOmissions(t *testing.T) {
	work, dist, members := stackOf(t, "member", map[string]string{
		".game": "omit private\n", "private": "not for release\n",
	})
	releaseGitFailure(t, "[ \"$1\" = rm ]")
	_, err := release.Stack(work, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(), "member: its omit lines") ||
		!strings.Contains(err.Error(), "release boundary refused") {
		t.Fatalf("failed member omission: %v", err)
	}
}

func TestStackRefusesUnreadableThirdPartyIndex(t *testing.T) {
	work, dist, members := stackOf(t, "member", map[string]string{
		"notes": "third-party source\n",
	})
	members[count.First] = takenAs(work, "member", true)
	releaseGitFailure(t, "[ \"$1\" = ls-files ] && [ \"$2\" = -z ]")
	_, err := release.Stack(work, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(), "the stack's index") ||
		!strings.Contains(err.Error(), "release boundary refused") {
		t.Fatalf("failed third-party index read: %v", err)
	}
}

func TestPrepareRefusesCommitWhoseTreeChanged(t *testing.T) {
	work, dist := fixture(t)
	options := release.For("sample", dist, "v1").Swept(sworn, nil, nil)
	releaseGitFailure(t, "[ \"$1\" = rev-parse ] && [ \"$2\" != 'HEAD^{tree}' ]")
	_, err := release.Prepare(work, options)
	if err == nil || !strings.Contains(err.Error(), "flat tree does not match") {
		t.Fatalf("unverified commit tree was accepted: %v", err)
	}
	files := filesAt(t, work, "HEAD")
	if len(files) != count.Next(count.One) {
		t.Fatalf("release failure changed source tree: %v", files)
	}
}

// TestPrepareRefusesACommitWhoseTreeIsAnotherTree: git reads the commit's
// tree without an error and it is not the tree the release was made from.
// the read succeeding is no reason to trust the answer.
func TestPrepareRefusesACommitWhoseTreeIsAnotherTree(t *testing.T) {
	work, dist := fixture(t)
	options := release.For("sample", dist, "v1").Swept(sworn, nil, nil)
	other := "0123456789abcdef0123456789abcdef01234567"
	releaseGitSays(t, "[ \"$1\" = rev-parse ] && [ \"$2\" != 'HEAD^{tree}' ]", other)
	_, err := release.Prepare(work, options)
	if err == nil || !strings.Contains(err.Error(), "flat tree does not match") {
		t.Fatalf("a commit of another tree was accepted: %v", err)
	}
}

// TestPrepareAcceptsACommitWhoseTreeIsTheTree: the same release with git
// left alone passes the check, so the refusal above is the tree and not
// the check refusing everything.
func TestPrepareAcceptsACommitWhoseTreeIsTheTree(t *testing.T) {
	work, dist := fixture(t)
	options := release.For("sample", dist, "v1").Swept(sworn, nil, nil)
	if _, err := release.Prepare(work, options); err != nil {
		t.Fatalf("an honest release was refused: %v", err)
	}
}

// filesAt is every file a commit holds, by name, as git lists them.
func filesAt(t *testing.T, work repo.Repo, rev string) []string {
	t.Helper()
	listed, err := work.Git("-c", "core.quotepath=off", "ls-tree", "-r",
		"--name-only", rev)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(listed)
}
