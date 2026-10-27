package release_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// bakedNever is a made-up never-word: a game built without any refuses
// to release, which most of these tests are not about.
func bakedNever() sweep.Never {
	body, err := sweep.Bake("a-salt", []string{"zorblax"})
	if err != nil {
		panic(err)
	}
	return sweep.Baked(fstest.MapFS{"never/hashes": {Data: []byte(body)}})
}

// sworn is the made-up never-word these releases sweep with.
var sworn = bakedNever()

// a private repository with two commits, a bare public root with none.
func fixture(t *testing.T) (repo.Repo, string) {
	t.Helper()
	dir := t.TempDir()
	priv := filepath.Join(dir, "priv")
	pub := filepath.Join(dir, "pub.git")
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(priv, openDir); err != nil {
		t.Fatal(err)
	}
	gitIn(t, priv, "init", "-q", "-b", "main")
	gitIn(t, priv, "config", "commit.gpgsign", "false")
	// a container has no git identity, and these tests commit. naming
	// one here is what makes the suite a property of the code rather
	// than of the machine it happens to run on.
	gitIn(t, priv, "config", "user.email", "test@example.invalid")
	gitIn(t, priv, "config", "user.name", "test")
	openFile := os.FileMode(0o644)
	first := filepath.Join(priv, "a.txt")
	if err := os.WriteFile(first, []byte("hello\n"), openFile); err != nil {
		t.Fatal(err)
	}
	gitIn(t, priv, "add", "a.txt")
	gitIn(t, priv, "commit", "-q", "-m",
		"one\n\nCo-Auth"+"ored-By: someone <x@y.z>")
	second := filepath.Join(priv, "b.txt")
	if err := os.WriteFile(second, []byte("world\n"), openFile); err != nil {
		t.Fatal(err)
	}
	gitIn(t, priv, "add", "b.txt")
	gitIn(t, priv, "commit", "-q", "-m", "two")
	gitIn(t, dir, "init", "-q", "--bare", pub)
	return repoAt(priv), pub
}

// aheadOf puts a commit on dist's main that the private repository has
// never seen, from a second clone, and returns it.
func aheadOf(t *testing.T, dist string) string {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	gitIn(t, filepath.Dir(other), "clone", "-q", dist, other)
	gitIn(t, other, "config", "commit.gpgsign", "false")
	gitIn(t, other, "config", "user.email", "other@example.invalid")
	gitIn(t, other, "config", "user.name", "other")
	path := filepath.Join(other, "d.txt")
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(path, []byte("fourth\n"), openFile); err != nil {
		t.Fatal(err)
	}
	gitIn(t, other, "add", "d.txt")
	gitIn(t, other, "commit", "-q", "-m", "four")
	gitIn(t, other, "push", "-q", dist, "HEAD:refs/heads/main")
	return gitIn(t, other, "rev-parse", "HEAD")
}

// released puts the private repository's tree on dist as a release.
func released(t *testing.T, priv repo.Repo, dist string) {
	t.Helper()
	options := release.For("x", dist, "v0.1.0").Swept(sworn, nil, nil)
	if _, err := release.Prepare(priv, options); err != nil {
		t.Fatal(err)
	}
	if _, err := release.Push(priv, options); err != nil {
		t.Fatal(err)
	}
}

// a plan asks dist for its main and fetches it, so a diff can be drawn
// against a commit this repository has never seen; a dist that is not
// there, or one that lists a main it cannot send, refuses the plan in
// git's own words about the fetch.
func TestPlanAsksDistAndFetchesItsMain(t *testing.T) {
	t.Parallel()
	priv, dist := fixture(t)
	options := release.For("x", dist, "v0.2.0").Swept(sworn, nil, nil)
	nowhere := filepath.Join(t.TempDir(), "nowhere.git")
	missing := release.For("x", nowhere, "v0.2.0").Swept(sworn, nil, nil)
	if _, err := release.Diff(priv, missing); err == nil {
		t.Error("a dist that is not there was planned against")
	}
	released(t, priv, dist)
	ahead := aheadOf(t, dist)
	drawn, err := release.Diff(priv, options)
	if err != nil {
		t.Fatalf("diff against a dist that is ahead: %v", err)
	}
	if drawn.Parent() != ahead || drawn.Same() {
		t.Errorf("parent %q, want %q; same %v",
			drawn.Parent(), ahead, drawn.Same())
	}
	if _, err := priv.Git("cat-file", "-e", ahead); err != nil {
		t.Error("dist's main was not fetched")
	}

	// a second dist, whose new main loses its object before this
	// repository can fetch it: listed, but not to be had. the refusal
	// is the fetch's, not a later command's, so it says what happened.
	priv, dist = fixture(t)
	released(t, priv, dist)
	ahead = aheadOf(t, dist)
	object := filepath.Join(dist, "objects", ahead[:fanout],
		ahead[fanout:])
	if err := os.Remove(object); err != nil {
		t.Fatal(err)
	}
	broken := release.For("x", dist, "v0.2.0").Swept(sworn, nil, nil)
	_, err = release.Diff(priv, broken)
	if err == nil || !strings.Contains(err.Error(), "not our ref") {
		t.Errorf("a dist that cannot send its main: %v", err)
	}
}

// commits is how many commits a revision can reach.
func commits(t *testing.T, work repo.Repo, rev string) int {
	t.Helper()
	out, err := work.Git("rev-list", "--count", rev)
	if err != nil {
		t.Fatal(err)
	}
	total, err := strconv.Atoi(out)
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// preparing keeps a flat commit and pushes nothing; pushing sends it and
// its tag; the same tree again is nothing new; a changed tree is a second
// commit parented on the first; the trailer in the road never reaches
// dist, and the message is the name and the tag and nothing else.
func TestPrepareThenPush(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	options := release.For("x", dist, "v0.3.0").Swept(sworn, nil, nil)
	res, err := release.Prepare(work, options)
	if err != nil || res.Commit() == "" {
		t.Fatalf("prepare: %+v %v", res, err)
	}
	if main, _ := work.RemoteMain(dist); main != "" {
		t.Fatal("prepare pushed")
	}
	if _, err := release.Push(work, options); err != nil {
		t.Fatalf("push: %v", err)
	}
	if main, _ := work.RemoteMain(dist); main != res.Commit() {
		t.Errorf("dist main is %q, want %q", main, res.Commit())
	}
	if tags, _ := work.Git("ls-remote", "--tags", dist); !strings.Contains(
		tags,
		"v0.3.0",
	) {
		t.Errorf("the tag did not land: %q", tags)
	}
	if local, _ := work.Git("tag", "-l", "v0.3.0"); local != "" {
		t.Error("the tag was left in the road")
	}
	msg, _ := work.Git("log", "-1", "--format=%B", res.Commit())
	if strings.TrimSpace(msg) != "x v0.3.0" {
		t.Errorf("message = %q, want only the name and the tag", msg)
	}
	openFile := os.FileMode(0o644)
	third := work.Path("c.txt")
	if err := os.WriteFile(third, []byte("more\n"), openFile); err != nil {
		t.Fatal(err)
	}
}

// a changed tree is a second commit parented on the first, and the
// trailer in the road never reaches dist.
func TestASecondReleaseParentsOnTheFirst(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	options := release.For("x", dist, "v0.3.0").Swept(sworn, nil, nil)
	if _, err := release.Prepare(work, options); err != nil {
		t.Fatal(err)
	}
	if _, err := release.Push(work, options); err != nil {
		t.Fatal(err)
	}
	openFile := os.FileMode(0o644)
	third := work.Path("c.txt")
	if err := os.WriteFile(third, []byte("more\n"), openFile); err != nil {
		t.Fatal(err)
	}
	gitOn(t, work, "add", "c.txt")
	gitIn(t, work.Dir(), "commit", "-q", "-m", "three")
	again := release.For("x", dist, "v0.3.1").Swept(sworn, nil, nil)
	res, err := release.Prepare(work, again)
	if err != nil {
		t.Fatalf("second prepare: %+v %v", res, err)
	}
	if _, err := release.Push(work, again); err != nil {
		t.Fatal(err)
	}
	want := int(held(t, "second-release-commits"))
	if total := commits(t, work, res.Commit()); total != want {
		t.Errorf("dist has %d commits after a change, want %d", total,
			want)
	}
	log, _ := work.Git("log", res.Commit(), "--format=%B")
	if strings.Contains(log, "Co-Auth"+"ored-By") {
		t.Error("the trailer reached dist")
	}
}

// gitOn runs git in the repository, stopping the test on an error.
func gitOn(t *testing.T, work repo.Repo, args ...string) {
	t.Helper()
	if _, err := work.Git(args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

// a push with nothing prepared is refused, and so is a push after dist's
// main moved underneath the prepared release.
func TestPushRefuses(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	if _, err := release.Push(work, options); err == nil {
		t.Fatal("pushed a release that was never prepared")
	}
	if _, err := release.Prepare(work, options); err != nil {
		t.Fatal(err)
	}
	other, _ := work.GitIn("elsewhere\n", "commit-tree", "HEAD^{tree}")
	if _, err := work.Git("push", dist, other+":refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if _, err := release.Push(work, options); err == nil {
		t.Error("pushed over a dist main that moved")
	}
}

// a dirty tree or a missing tag refuses before anything is kept.
func TestPrepareRefusesADirtyTreeOrNoTag(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	noTag := release.For("x", dist, "").Swept(sworn, nil, nil)
	if _, err := release.Prepare(work, noTag); err == nil {
		t.Error("prepared without a tag")
	}
	openFile := os.FileMode(0o644)
	dirty := work.Path("dirty.txt")
	if err := os.WriteFile(dirty, []byte("x"), openFile); err != nil {
		t.Fatal(err)
	}
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	if _, err := release.Prepare(work, options); err == nil {
		t.Fatal("a dirty tree prepared")
	}
	if _, err := work.Git("rev-parse", "--verify", "-q",
		refOf("v1")); err == nil {
		t.Error("a refused release was kept")
	}
}

// a machine path and a setting's value each refuse before anything is
// kept; an excluded prefix is left alone.
func TestPrepareRefusesWhatTheSweepFinds(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	openFile := os.FileMode(0o644)
	notes := work.Path("notes.md")
	if err := os.WriteFile(notes,
		[]byte("see /Us"+"ers/someone/thing\n"), openFile); err != nil {
		t.Fatal(err)
	}
	gitOn(t, work, "add", "notes.md")
	gitIn(t, work.Dir(), "commit", "-q", "-m", "a path")
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	res, err := release.Prepare(work, options)
	if err == nil || !strings.Contains(err.Error(), "machine paths in notes.md") {
		t.Fatalf("a machine path was not found: %+v %v", res, err)
	}
	excluded := release.For("x", dist, "v1").
		Swept(sworn, []string{"notes.md"}, nil)
	if _, err := release.Prepare(work, excluded); err != nil {
		t.Fatalf("an excluded file still refused: %v", err)
	}
	conf := work.Path("conf.md")
	if err := os.WriteFile(conf,
		[]byte("contact ada@example.invalid\n"), openFile); err != nil {
		t.Fatal(err)
	}
	gitOn(t, work, "add", "conf.md")
	gitIn(t, work.Dir(), "commit", "-q", "-m", "a value")
	valued := release.For("x", dist, "v2").
		Swept(sworn, []string{"notes.md"}, nil).
		Refusing([]string{"ada@example.invalid"})
	res, err = release.Prepare(work, valued)
	if err == nil ||
		!strings.Contains(err.Error(), "a setting's value in conf.md") {
		t.Fatalf("a setting's value was not refused: %+v %v", res, err)
	}
}

// a licence is the one file whose job is to name the author; anywhere else
// in the tree the name refuses a release, and a handle is not the name.
func TestAuthor(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	openFile := os.FileMode(0o644)
	licence := work.Path("LICENSE.txt")
	if err := os.WriteFile(licence, []byte("Copyright Ada.\n"),
		openFile); err != nil {
		t.Fatal(err)
	}
	module := work.Path("go.md")
	if err := os.WriteFile(module,
		[]byte("module github.com/adalovelace/x\n"), openFile); err != nil {
		t.Fatal(err)
	}
	gitOn(t, work, "add", "LICENSE.txt", "go.md")
	gitIn(t, work.Dir(), "commit", "-q", "-m", "licence")
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil).
		By("Ada")
	if _, err := release.Prepare(work, options); err != nil {
		t.Fatalf("the licence or the handle refused a release: %v", err)
	}
	readme := work.Path("README.md")
	if err := os.WriteFile(readme, []byte("Ada wrote this.\n"),
		openFile); err != nil {
		t.Fatal(err)
	}
	gitOn(t, work, "add", "README.md")
	gitIn(t, work.Dir(), "commit", "-q", "-m", "readme")
	named := release.For("x", dist, "v2").Swept(sworn, nil, nil).By("Ada")
	res, err := release.Prepare(work, named)
	if err == nil ||
		!strings.Contains(err.Error(), "the author Ada in README.md") {
		t.Fatalf("the author in the readme released: %+v %v", res, err)
	}
}

// a release carries the builder's one licence: the file named in their
// dotfile lands in the released tree as LICENSE.txt, replacing whatever the
// repository kept, and the road's working copy is not touched by it.
func TestPrepareCarriesTheLicense(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	licence := filepath.Join(t.TempDir(), "the-licence")
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(licence, []byte("the terms, as of today\n"),
		openFile); err != nil {
		t.Fatal(err)
	}
	options := release.For("x", dist, "v0.3.0").Swept(sworn, nil, nil).
		Licensed(licence)
	res, err := release.Prepare(work, options)
	if err != nil || res.Commit() == "" {
		t.Fatalf("prepare: %+v %v", res, err)
	}
	got, err := work.Git("show", res.Commit()+":LICENSE.txt")
	if err != nil || strings.TrimSpace(got) != "the terms, as of today" {
		t.Errorf("the released tree's licence is %q (%v)", got, err)
	}
	if other, _ := work.Git("show", res.Commit()+":a.txt"); !strings.Contains(
		other, "hello",
	) {
		t.Errorf("the rest of the tree did not come along: %q", other)
	}
	if _, err := os.Stat(work.Path("LICENSE.txt")); err == nil {
		t.Error("the licence was written into the road's working copy")
	}
	if clean, _ := work.Clean(); !clean {
		t.Error("preparing a release left the road dirty")
	}
}

// a licence that cannot be read, or has nothing in it, stops the release
// and says which file: shipping with no terms is what this is here to end.
func TestPrepareRefusesAMissingOrEmptyLicense(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	empty := filepath.Join(t.TempDir(), "empty")
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(empty, nil, openFile); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{empty, empty + "-not-there"} {
		options := release.For("x", dist, "v0.3.0").
			Swept(sworn, nil, nil).Licensed(path)
		_, err := release.Prepare(work, options)
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%s: got %v", path, err)
		}
	}
}

// a diff against an empty dist is the whole tree.
func TestDiffAgainstAnEmptyDistIsTheWholeTree(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	options := release.For("x", dist, "v0.3.0").Swept(sworn, nil, nil)
	drawn, err := release.Diff(work, options)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if drawn.Same() || drawn.Parent() != "" {
		t.Fatalf("empty dist: %+v", drawn)
	}
	if !strings.Contains(drawn.Patch(), "hello") ||
		!strings.Contains(drawn.Stat(), "a.txt") {
		t.Errorf("first diff missed the tree: %q", drawn.Stat())
	}
}

// after a push with nothing changed the diff says so rather than
// showing an empty patch; with one file changed the diff names it, the
// parent is dist, and a diff publishes nothing.
func TestDiffAfterAPushSaysWhatChanged(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	options := release.For("x", dist, "v0.3.0").Swept(sworn, nil, nil)
	if _, err := release.Prepare(work, options); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := release.Push(work, options); err != nil {
		t.Fatalf("push: %v", err)
	}
	drawn, err := release.Diff(work, options)
	if err != nil {
		t.Fatalf("diff after push: %v", err)
	}
	if !drawn.Same() {
		t.Errorf("after a push with no change, Same is false: %+v", drawn)
	}
	openFile := os.FileMode(0o644)
	third := work.Path("c.txt")
	if err := os.WriteFile(third, []byte("third\n"), openFile); err != nil {
		t.Fatal(err)
	}
	gitOn(t, work, "add", "c.txt")
	gitIn(t, work.Dir(), "commit", "-q", "-m", "three")
	changed := release.For("x", dist, "v0.3.1").Swept(sworn, nil, nil)
	drawn, err = release.Diff(work, changed)
	if err != nil {
		t.Fatalf("diff after a change: %v", err)
	}
	if drawn.Same() {
		t.Fatal("a changed tree reported as the same")
	}
	if !strings.Contains(drawn.Stat(), "c.txt") ||
		!strings.Contains(drawn.Patch(), "third") {
		t.Errorf("the changed file is not in the diff: %q", drawn.Stat())
	}
	if main, _ := work.RemoteMain(dist); drawn.Parent() != main {
		t.Errorf("parent is %q, dist's main is %q", drawn.Parent(), main)
	}
	// and a diff publishes nothing.
	if ref, _ := work.Git("rev-parse", "--verify", "-q",
		refOf("v0.3.1")); ref != "" {
		t.Error("diff prepared a release")
	}
}

// a game built without never-words refuses a release and a stack before
// it touches anything: it cannot sweep for names it was never given.
func TestNoNeverWordsNoRelease(t *testing.T) {
	t.Parallel()
	nowhere := repoAt(t.TempDir())
	if _, err := release.Prepare(nowhere,
		release.For("", "", "v1")); err == nil ||
		!strings.Contains(err.Error(), "built without never-words") {
		t.Errorf("a release without never-words was not refused: %v", err)
	}
	members := []release.Member{release.Taken("m", "v1", "", false)}
	nowhere = repoAt(t.TempDir())
	_, err := release.Stack(nowhere,
		release.For("", "", "v1"), members)
	if err == nil || !strings.Contains(err.Error(), "built without never-words") {
		t.Errorf("a stack without never-words was not refused: %v", err)
	}
}

// a never-word in the tree refuses the release, broken across a line or
// not, and the hit says a never-word was there without saying which.
func TestANeverWordInTheTreeRefuses(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	openFile := os.FileMode(0o644)
	thanks := work.Path("thanks.md")
	if err := os.WriteFile(thanks,
		[]byte("with thanks to\nZorblax, as ever\n"), openFile); err != nil {
		t.Fatal(err)
	}
	gitOn(t, work, "add", "thanks.md")
	gitIn(t, work.Dir(), "commit", "-q", "-m", "thanks")
	options := release.For("x", dist, "v1").Swept(sworn, nil, nil)
	_, err := release.Prepare(work, options)
	if err == nil {
		t.Fatal("a tree with a never-word in it was prepared")
	}
	said := err.Error()
	if !strings.Contains(said, "a never-word in thanks.md") {
		t.Errorf("no never-word hit in thanks.md: %v", err)
	}
	if strings.Contains(strings.ToLower(said), "zorblax") {
		t.Errorf("a hit names the word: %q", said)
	}
}

// a submodule has no body to look for a value in, so a release that
// carries one is refused, naming it, even where the sweep is told to
// leave its directory alone: values reads the whole tree.
func TestASubmoduleIsRefusedWhereValuesAreSought(t *testing.T) {
	t.Parallel()
	work, dist := fixture(t)
	head, err := work.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := work.Git("update-index", "--add", "--cacheinfo",
		"160000,"+head+",vendor/sub"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work.Dir(), "commit", "-q", "-m", "a submodule")
	if err := os.MkdirAll(work.Path("vendor", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	// the directory is empty, which is how a submodule not checked out
	// looks, and leaves the tree clean
	options := release.For("x", dist, "v1").
		Swept(sworn, []string{"vendor/"}, nil).
		Refusing([]string{"hunter2"})
	_, err = release.Prepare(work, options)
	if err == nil || !strings.Contains(err.Error(), "vendor/sub") {
		t.Errorf("a submodule: %v, want a refusal naming vendor/sub", err)
	}
}

// fanout is where a loose object's directory name ends in its hash: two
// characters, defined by git's object store.
const fanout = 2
