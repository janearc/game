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

// rootUID is the superuser's user id, as unix has defined it.
const rootUID = 0

// committed adds one file to the private repository and commits it.
func committed(t *testing.T, work repo.Repo, name, body string) {
	t.Helper()
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(work.Path(name), []byte(body),
		openFile); err != nil {
		t.Fatal(err)
	}
	gitOn(t, work, "add", name)
	gitIn(t, work.Dir(), "commit", "-q", "-m", name)
}

// refusing gives dist a pre-receive hook that refuses any ref matching
// the shell pattern, so a push of that ref fails at dist.
func refusing(t *testing.T, dist, pattern string) {
	t.Helper()
	hook := "#!/bin/sh\nwhile read old new ref; do\n\tcase \"$ref\" in\n" +
		"\t" + pattern + ") echo \"dist refuses $ref\" >&2; exit 1;;\n" +
		"\tesac\ndone\n"
	openScript := os.FileMode(0o755)
	hooks := filepath.Join(dist, "hooks", "pre-receive")
	if err := os.WriteFile(hooks, []byte(hook), openScript); err != nil {
		t.Fatal(err)
	}
}

// TestPushRefusesADistThatWentBack: when dist's main is set back to an
// older release than the one this was prepared on, the push would be a
// fast-forward git accepts, and it is refused anyway, because the work
// set back is someone's.
func TestPushRefusesADistThatWentBack(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	first := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	one, err := release.Prepare(work, first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := release.Push(work, first); err != nil {
		t.Fatal(err)
	}
	committed(t, work, "c.txt", "third\n")
	second := release.For("x", dist, "v2").Swept(sworn, nil, nil)
	if _, err := release.Prepare(work, second); err != nil {
		t.Fatal(err)
	}
	if _, err := release.Push(work, second); err != nil {
		t.Fatal(err)
	}
	committed(t, work, "d.txt", "fourth\n")
	third := release.For("x", dist, "v3").Swept(sworn, nil, nil)
	if _, err := release.Prepare(work, third); err != nil {
		t.Fatal(err)
	}
	if _, err := work.Git("push", "-f", dist,
		one.Commit()+":refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	_, err = release.Push(work, third)
	if err == nil || !strings.Contains(err.Error(), "dist's main moved") {
		t.Errorf("a dist set back: %v, want the moved refusal", err)
	}
	if main, _ := work.RemoteMain(dist); main != one.Commit() {
		t.Errorf("dist's main is %s, want it left at %s", main,
			one.Commit())
	}
}

// TestPushOfWhatIsThereSkipsTheMain: pushing a release whose commit dist
// already has as its main sends no main, and still sends the tag.
func TestPushOfWhatIsThereSkipsTheMain(t *testing.T) {
	work, dist := fixture(t)
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	prepared, err := release.Prepare(work, options)
	if err != nil {
		t.Fatal(err)
	}
	pushes := recordingGit(t)
	main := "push --atomic " + dist + " " + prepared.Commit() + ":refs/heads/main"
	if _, err := release.Push(work, options); err != nil {
		t.Fatalf("the first push: %v", err)
	}
	if log := pushes(); !strings.Contains(log, main) {
		t.Fatalf("the first push sent no main:\n%s", log)
	}
	if _, err := work.Git("push", dist, ":refs/tags/v1"); err != nil {
		t.Fatal(err)
	}
	before := pushes()
	if _, err := release.Push(work, options); err != nil {
		t.Errorf("again: %v", err)
	}
	if log := strings.TrimPrefix(pushes(), before); strings.Contains(log, main) {
		t.Errorf("again, the main was sent:\n%s", log)
	}
	if tags, _ := work.Git("ls-remote", "--tags", dist); !strings.Contains(
		tags, "refs/tags/v1") {
		t.Errorf("the tag was not sent again: %q", tags)
	}
}

// refusalCase is one ref dist refuses, by name.
type refusalCase struct {
	name    string
	pattern string
}

// check is the refusal as a sub-test: the push fails with dist's words.
func (each refusalCase) check(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	if _, err := release.Prepare(work, options); err != nil {
		t.Fatal(err)
	}
	refusing(t, dist, each.pattern)
	if _, err := release.Push(work, options); err == nil ||
		!strings.Contains(err.Error(), "dist refuses") {
		t.Errorf("%v, want dist's refusal", err)
	}
}

// TestPushRefusesWhatDistRefuses: a dist that will not take the main, or
// will not take the tag, is an error, never a release half sent and
// called done.
func TestPushRefusesWhatDistRefuses(t *testing.T) {
	t.Parallel()
	cases := []refusalCase{
		{"the main", "refs/heads/main"},
		{"the tag", "refs/tags/*"},
	}
	for _, each := range cases {
		t.Run(each.name, each.check)
	}
}

// TestPushRefusesADistThatIsNotThere: with no dist to ask, the push says
// so in git's words about asking, before it tries to send anything.
func TestPushRefusesADistThatIsNotThere(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	if _, err := release.Prepare(work, options); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(t.TempDir(), "gone.git")
	missing := release.For("x", gone, "v1").Swept(sworn, nil, nil)
	if _, err := release.Push(work, missing); err == nil ||
		!strings.Contains(err.Error(), "ls-remote") {
		t.Errorf("%v, want the refusal to ask a dist that is not there", err)
	}
}

// TestDiffReportsWhatTheSweepFinds: where Prepare would refuse, Diff
// shows the patch and names the hit.
func TestDiffReportsWhatTheSweepFinds(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	committed(t, work, "z.txt", "zorblax\n")
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	drawn, err := release.Diff(work, options)
	if err != nil {
		t.Fatal(err)
	}
	hits := drawn.Hits()
	if count.None(len(hits)) ||
		!strings.Contains(drawn.Patch(), "zorblax") ||
		!strings.Contains(drawn.Stat(), "z.txt") {
		t.Errorf("hits %v, stat %q", hits, drawn.Stat())
	}
}

// TestPrepareNamesAMissingTag: a release with no tag is refused in words
// that say what a tag looks like.
func TestPrepareNamesAMissingTag(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	options := release.For("x", dist, "").Swept(sworn, nil, nil)
	_, err := release.Prepare(work, options)
	if err == nil || !strings.Contains(err.Error(), "needs a tag") {
		t.Errorf("%v, want the missing tag named", err)
	}
}

// TestPushWithNothingPreparedSaysSo: a push of a tag never prepared is
// refused in those words, and dist's main is left as it was; a push of
// nothing at all must never reach dist.
func TestPushWithNothingPreparedSaysSo(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	released(t, work, dist)
	before, _ := work.RemoteMain(dist)
	options := release.For("x", dist, "v9").Swept(sworn, nil, nil)
	if _, err := release.Push(work, options); err == nil ||
		!strings.Contains(err.Error(), "no prepared release v9") {
		t.Errorf("%v, want the unprepared release named", err)
	}
	if after, _ := work.RemoteMain(dist); after != before {
		t.Errorf("dist's main went from %s to %q", before, after)
	}
}

// shutDir is a directory shut until the test ends, and the mode that
// opens it again.
type shutDir struct {
	dir  string
	open os.FileMode
}

// reopen opens the directory again, so the tree can be removed.
func (shut shutDir) reopen() {
	_ = os.Chmod(shut.dir, shut.open)
}

// TestPushRefusesATagItCannotMake: a tag this repository cannot write is
// git's error about the tag, not a later one about sending it.
func TestPushRefusesATagItCannotMake(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == rootUID {
		t.Skip("root writes through a read-only directory")
	}
	work, dist := fixture(t)
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	if _, err := release.Prepare(work, options); err != nil {
		t.Fatal(err)
	}
	tags := work.Path(".git", "refs", "tags")
	shut := os.FileMode(0o555)
	if err := os.Chmod(tags, shut); err != nil {
		t.Fatal(err)
	}
	openDir := os.FileMode(0o755)
	t.Cleanup(shutDir{dir: tags, open: openDir}.reopen)
	if _, err := release.Push(work, options); err == nil ||
		!strings.Contains(err.Error(), "git tag") {
		t.Errorf("%v, want git's refusal to make the tag", err)
	}
}

// TestPrepareOfWhatDistHasIsSkipped: with nothing changed since the last
// release, the release is dist's own main, not a new commit beside it.
func TestPrepareOfWhatDistHasIsSkipped(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	released(t, work, dist)
	main, _ := work.RemoteMain(dist)
	options := release.For("x", dist, "v0.1.1").Swept(sworn, nil, nil)
	res, err := release.Prepare(work, options)
	if err != nil || res.Commit() != main {
		t.Errorf("nothing changed: %+v, %v, want dist's main %s", res, err,
			main)
	}
}

// TestASubmoduleTheSweepCannotReadIsRefused: a submodule the sweep is
// not told to leave alone has no body to read, so Diff and Prepare are
// each refused, naming it, even with no setting values to look for.
func TestASubmoduleTheSweepCannotReadIsRefused(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	head, err := work.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := work.Git("update-index", "--add", "--cacheinfo",
		"160000,"+head+",vendored"); err != nil {
		t.Fatal(err)
	}
	if _, err := work.GitIn("", "commit", "-q", "-m", "a submodule"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(work.Path("vendored"), 0o755); err != nil {
		t.Fatal(err)
	}
	// the directory is empty, which is how a submodule not checked out looks
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	if _, err := release.Diff(work, options); err == nil ||
		!strings.Contains(err.Error(), "vendored") {
		t.Errorf("diff: %v, want a refusal naming vendored", err)
	}
	if _, err := release.Prepare(work, options); err == nil ||
		!strings.Contains(err.Error(), "vendored") {
		t.Errorf("prepare: %v, want a refusal naming vendored", err)
	}
}

// recordingGit puts a git ahead of the real one that notes each call's
// words in a file and passes it on, and gives a function that reads what
// has been noted. it sets PATH, so a test that uses it runs alone.
func recordingGit(t *testing.T) func() string {
	t.Helper()
	actual, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(t.TempDir(), "calls")
	body := "#!/bin/sh\necho \"$@\" >> '" + log + "'\nexec '" + actual +
		"' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return noting{log}.read
}

// noting is the file a recording git notes its calls in.
type noting struct{ log string }

// read is what has been noted.
func (note noting) read() string {
	noted, _ := os.ReadFile(note.log)
	return string(noted)
}

// preparedRelease is a fixture with a release v1 prepared and not sent.
func preparedRelease(t *testing.T) (repo.Repo, string, release.Options) {
	t.Helper()
	tree, dist := fixture(t)
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	if _, err := release.Prepare(tree, options); err != nil {
		t.Fatal(err)
	}
	return tree, dist, options
}

// nothingSent fails the test if dist has a main or a tag.
func nothingSent(t *testing.T, tree repo.Repo, dist string) {
	t.Helper()
	main, err := tree.RemoteMain(dist)
	if err != nil || main != "" {
		t.Errorf("dist has main %q (%v)", main, err)
	}
	if tags, _ := tree.Git("ls-remote", "--tags", dist); tags != "" {
		t.Errorf("dist has tags: %s", tags)
	}
}

// TestPushOfATagDistRefusesLeavesDistsMainAlone: the main and the tag go
// together. When dist refuses the tag it takes neither. The push can
// then be made again.
func TestPushOfATagDistRefusesLeavesDistsMainAlone(t *testing.T) {
	t.Parallel()
	tree, dist, options := preparedRelease(t)
	refusing(t, dist, "refs/tags/*")
	if _, err := release.Push(tree, options); err == nil {
		t.Fatal("a dist that refused the tag took the push")
	}
	nothingSent(t, tree, dist)
	if kept, _ := tree.Git("tag", "-l"); kept != "" {
		t.Errorf("a local tag was left: %q", kept)
	}
	hook := filepath.Join(dist, "hooks", "pre-receive")
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if _, err := release.Push(tree, options); err != nil {
		t.Errorf("the push made again: %v", err)
	}
}

// TestPushKeepsALocalTagOfTheSameName: a tag this repository already
// has under the release's name is not its own. The push is refused with
// the tag as it was, and nothing is sent.
func TestPushKeepsALocalTagOfTheSameName(t *testing.T) {
	t.Parallel()
	tree, dist, options := preparedRelease(t)
	if _, err := tree.Git("tag", "-m", "mine", "v1", "HEAD"); err != nil {
		t.Fatal(err)
	}
	before, _ := tree.Git("rev-parse", "refs/tags/v1")
	_, err := release.Push(tree, options)
	if err == nil || !strings.Contains(err.Error(), "tag v1 is already") {
		t.Errorf("%v, want the tag named as already there", err)
	}
	if after, _ := tree.Git("rev-parse", "refs/tags/v1"); after != before {
		t.Errorf("the tag was %s, is now %q", before, after)
	}
	nothingSent(t, tree, dist)
}

// TestPushOfATagDistHasLeavesTheMainAlone: a tag that dist already holds
// is refused by dist, and the main does not move for it.
func TestPushOfATagDistHasLeavesTheMainAlone(t *testing.T) {
	t.Parallel()
	tree, dist, options := preparedRelease(t)
	if _, err := tree.Git("push", dist, "HEAD:refs/tags/v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := release.Push(tree, options); err == nil {
		t.Fatal("a tag dist had was sent again")
	}
	if main, _ := tree.RemoteMain(dist); main != "" {
		t.Errorf("dist's main moved to %s", main)
	}
}

// TestPushNeedsADistThatTakesBothAtOnce: a dist that cannot take two refs
// in one push is refused. The two are not sent one after the other.
func TestPushNeedsADistThatTakesBothAtOnce(t *testing.T) {
	t.Parallel()
	tree, dist, options := preparedRelease(t)
	if _, err := tree.Git("-C", dist, "config", "receive.advertiseAtomic",
		"false"); err != nil {
		t.Fatal(err)
	}
	_, err := release.Push(tree, options)
	if err == nil || !strings.Contains(err.Error(), "atomic") {
		t.Errorf("%v, want the refusal to push both at once", err)
	}
	nothingSent(t, tree, dist)
}
