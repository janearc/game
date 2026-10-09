package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// a private repository with two commits, a bare public root with none.
func fixture(t *testing.T) (repo.Repo, string) {
	t.Helper()
	dir := t.TempDir()
	priv := filepath.Join(dir, "priv")
	pub := filepath.Join(dir, "pub.git")
	run := func(d string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = append(
			os.Environ(),
			"GIT_AUTHOR_NAME=t",
			"GIT_AUTHOR_EMAIL=t@t.t",
			"GIT_COMMITTER_NAME=t",
			"GIT_COMMITTER_EMAIL=t@t.t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(priv, 0o755)
	run(priv, "init", "-q", "-b", "main")
	run(priv, "config", "commit.gpgsign", "false")
	// a container has no git identity, and these tests commit. naming
	// one here is what makes the suite a property of the code rather
	// than of the machine it happens to run on.
	run(priv, "config", "user.email", "test@example.invalid")
	run(priv, "config", "user.name", "test")
	os.WriteFile(filepath.Join(priv, "a.txt"), []byte("hello\n"), 0o644)
	run(priv, "add", "a.txt")
	run(
		priv,
		"commit",
		"-q",
		"-m",
		"one\n\nCo-Auth"+"ored-By: someone <x@y.z>",
	)
	os.WriteFile(filepath.Join(priv, "b.txt"), []byte("world\n"), 0o644)
	run(priv, "add", "b.txt")
	run(priv, "commit", "-q", "-m", "two")
	run(dir, "init", "-q", "--bare", pub)
	return repo.Repo{Dir: priv}, pub
}

// count is how many commits a revision can reach.
func count(t *testing.T, r repo.Repo, rev string) int {
	out, err := r.Git("rev-list", "--count", rev)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, c := range out {
		n = n*10 + int(c-'0')
	}
	return n
}

// preparing keeps a flat commit and pushes nothing; pushing sends it and
// its tag; the same tree again is nothing new; a changed tree is a second
// commit parented on the first; the trailer in the road never reaches
// dist, and the message is the name and the tag and nothing else.
func TestPrepareThenPush(t *testing.T) {
	r, dist := fixture(t)
	o := Options{Never: sworn, Name: "x", Dist: dist, Tag: "v0.3.0"}
	res, err := Prepare(r, o)
	if err != nil || res.Skipped || res.Commit == "" {
		t.Fatalf("prepare: %+v %v", res, err)
	}
	if main, _ := r.RemoteMain(dist); main != "" {
		t.Fatal("prepare pushed")
	}
	if _, err := Push(r, o); err != nil {
		t.Fatalf("push: %v", err)
	}
	if main, _ := r.RemoteMain(dist); main != res.Commit {
		t.Errorf("dist main is %q, want %q", main, res.Commit)
	}
	if tags, _ := r.Git("ls-remote", "--tags", dist); !strings.Contains(
		tags,
		"v0.3.0",
	) {
		t.Errorf("the tag did not land: %q", tags)
	}
	if local, _ := r.Git("tag", "-l", "v0.3.0"); local != "" {
		t.Error("the tag was left in the road")
	}
	msg, _ := r.Git("log", "-1", "--format=%B", res.Commit)
	if strings.TrimSpace(msg) != "x v0.3.0" {
		t.Errorf("message = %q, want only the name and the tag", msg)
	}
	os.WriteFile(filepath.Join(r.Dir, "c.txt"), []byte("more\n"), 0o644)
	r.Git("add", "c.txt")
	r.GitIn("", "commit", "-q", "-m", "three")
	o.Tag = "v0.3.1"
	res2, err := Prepare(r, o)
	if err != nil || res2.Skipped {
		t.Fatalf("second prepare: %+v %v", res2, err)
	}
	if _, err := Push(r, o); err != nil {
		t.Fatal(err)
	}
	if n := count(t, r, res2.Commit); n != 2 {
		t.Errorf("dist has %d commits after a change, want 2", n)
	}
	log, _ := r.Git("log", res2.Commit, "--format=%B")
	if strings.Contains(log, "Co-Auth"+"ored-By") {
		t.Error("the trailer reached dist")
	}
}

// a push with nothing prepared is refused, and so is a push after dist's
// main moved underneath the prepared release.
func TestPushRefuses(t *testing.T) {
	r, dist := fixture(t)
	if _, err := Push(r, Options{Never: sworn, Name: "x", Dist: dist, Tag: "v1"}); err == nil {
		t.Fatal("pushed a release that was never prepared")
	}
	if _, err := Prepare(r, Options{Never: sworn, Name: "x", Dist: dist, Tag: "v1"}); err != nil {
		t.Fatal(err)
	}
	other, _ := r.GitIn("elsewhere\n", "commit-tree", "HEAD^{tree}")
	if _, err := r.Git("push", dist, other+":refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if _, err := Push(r, Options{Never: sworn, Name: "x", Dist: dist, Tag: "v1"}); err == nil {
		t.Error("pushed over a dist main that moved")
	}
}

// a dirty tree, a missing tag, a machine path and a setting's value each
// refuse before anything is kept; an excluded prefix is left alone.
func TestPrepareRefuses(t *testing.T) {
	r, dist := fixture(t)
	if _, err := Prepare(r, Options{Never: sworn, Name: "x", Dist: dist}); err == nil {
		t.Error("prepared without a tag")
	}
	os.WriteFile(filepath.Join(r.Dir, "dirty.txt"), []byte("x"), 0o644)
	if _, err := Prepare(r, Options{Never: sworn, Name: "x", Dist: dist, Tag: "v1"}); err == nil {
		t.Fatal("a dirty tree prepared")
	}
	os.Remove(filepath.Join(r.Dir, "dirty.txt"))
	os.WriteFile(
		filepath.Join(r.Dir, "notes.md"),
		[]byte("see /Us"+"ers/someone/thing\n"),
		0o644,
	)
	r.Git("add", "notes.md")
	r.GitIn("", "commit", "-q", "-m", "a path")
	res, err := Prepare(r, Options{Never: sworn, Name: "x", Dist: dist, Tag: "v1"})
	if err == nil || len(res.Hits) == 0 {
		t.Fatalf("a machine path was not found: %+v %v", res, err)
	}
	if _, err := r.Git("rev-parse", "--verify", "-q", Ref("v1")); err == nil {
		t.Error("a refused release was kept")
	}
	if _, err := Prepare(r, Options{Never: sworn, Name: "x", Dist: dist, Tag: "v1", Exclude: []string{"notes.md"}}); err != nil {
		t.Fatalf("an excluded file still refused: %v", err)
	}
	os.WriteFile(
		filepath.Join(r.Dir, "conf.md"),
		[]byte("contact ada@example.invalid\n"),
		0o644,
	)
	r.Git("add", "conf.md")
	r.GitIn("", "commit", "-q", "-m", "a value")
	res, err = Prepare(
		r,
		Options{
			Never:   sworn,
			Name:    "x",
			Dist:    dist,
			Tag:     "v2",
			Exclude: []string{"notes.md"},
			Values:  []string{"ada@example.invalid"},
		},
	)
	found := false
	for _, h := range res.Hits {
		found = found || h.Kind == "a setting's value"
	}
	if err == nil || !found {
		t.Fatalf("a setting's value was not refused: %+v %v", res, err)
	}
}

// a licence is the one file whose job is to name the author; anywhere else
// in the tree the name refuses a release, and a handle is not the name.
func TestAuthor(t *testing.T) {
	r, dist := fixture(t)
	os.WriteFile(
		filepath.Join(r.Dir, "LICENSE.txt"),
		[]byte("Copyright Ada.\n"),
		0o644,
	)
	os.WriteFile(
		filepath.Join(r.Dir, "go.md"),
		[]byte("module github.com/adalovelace/x\n"),
		0o644,
	)
	r.Git("add", "LICENSE.txt", "go.md")
	r.GitIn("", "commit", "-q", "-m", "licence")
	if _, err := Prepare(r, Options{Never: sworn, Name: "x", Dist: dist, Tag: "v1", Author: "Ada"}); err != nil {
		t.Fatalf("the licence or the handle refused a release: %v", err)
	}
	os.WriteFile(
		filepath.Join(r.Dir, "README.md"),
		[]byte("Ada wrote this.\n"),
		0o644,
	)
	r.Git("add", "README.md")
	r.GitIn("", "commit", "-q", "-m", "readme")
	if res, err := Prepare(r, Options{Never: sworn, Name: "x", Dist: dist, Tag: "v2", Author: "Ada"}); err == nil ||
		len(res.Hits) == 0 {
		t.Fatalf("the author in the readme released: %+v %v", res, err)
	}
}

// a release carries the builder's one licence: the file named in their
// dotfile lands in the released tree as LICENSE.txt, replacing whatever the
// repository kept, and the road's working copy is not touched by it.
func TestPrepareCarriesTheLicense(t *testing.T) {
	r, dist := fixture(t)
	lic := filepath.Join(t.TempDir(), "the-licence")
	os.WriteFile(lic, []byte("the terms, as of today\n"), 0o644)
	o := Options{Never: sworn, Name: "x", Dist: dist, Tag: "v0.3.0", License: lic}
	res, err := Prepare(r, o)
	if err != nil || res.Commit == "" {
		t.Fatalf("prepare: %+v %v", res, err)
	}
	got, err := r.Git("show", res.Commit+":LICENSE.txt")
	if err != nil || strings.TrimSpace(got) != "the terms, as of today" {
		t.Errorf("the released tree's licence is %q (%v)", got, err)
	}
	if other, _ := r.Git("show", res.Commit+":a.txt"); !strings.Contains(
		other, "hello",
	) {
		t.Errorf("the rest of the tree did not come along: %q", other)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "LICENSE.txt")); err == nil {
		t.Error("the licence was written into the road's working copy")
	}
	if clean, _ := r.Clean(); !clean {
		t.Error("preparing a release left the road dirty")
	}
}

// a licence that cannot be read, or has nothing in it, stops the release
// and says which file: shipping with no terms is what this is here to end.
func TestPrepareRefusesAMissingOrEmptyLicense(t *testing.T) {
	r, dist := fixture(t)
	empty := filepath.Join(t.TempDir(), "empty")
	os.WriteFile(empty, nil, 0o644)
	for _, path := range []string{empty, empty + "-not-there"} {
		o := Options{Never: sworn, Name: "x", Dist: dist, Tag: "v0.3.0", License: path}
		_, err := Prepare(r, o)
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%s: got %v", path, err)
		}
	}
}

// a diff is the two ends of the release that has not happened yet: what
// dist holds now against the flat tree this branch would send. after a
// push with nothing changed it says so rather than showing an empty
// patch, and it names the file that changed rather than the commit.
func TestDiffShowsWhatAPushWouldSend(t *testing.T) {
	r, dist := fixture(t)
	o := Options{Never: sworn, Name: "x", Dist: dist, Tag: "v0.3.0"}

	// nothing on dist yet: the whole tree is the change.
	d, err := Diff(r, o)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if d.Same || d.Parent != "" {
		t.Fatalf("empty dist: %+v", d)
	}
	if !strings.Contains(d.Patch, "hello") ||
		!strings.Contains(d.Stat, "a.txt") {
		t.Errorf("first diff missed the tree: %q", d.Stat)
	}

	if _, err := Prepare(r, o); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := Push(r, o); err != nil {
		t.Fatalf("push: %v", err)
	}

	// pushed and unchanged: a release would be a no-op, and saying so
	// is not the same as showing an empty patch.
	d, err = Diff(r, o)
	if err != nil {
		t.Fatalf("diff after push: %v", err)
	}
	if !d.Same {
		t.Errorf("after a push with no change, Same is false: %+v", d)
	}

	// one file changed: the diff names it, and the parent is dist.
	if err := os.WriteFile(
		filepath.Join(r.Dir, "c.txt"), []byte("third\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		if _, err := r.Git(args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	git("add", "c.txt")
	git("commit", "-q", "-m", "three")
	d, err = Diff(r, Options{Never: sworn, Name: "x", Dist: dist, Tag: "v0.3.1"})
	if err != nil {
		t.Fatalf("diff after a change: %v", err)
	}
	if d.Same {
		t.Fatal("a changed tree reported as the same")
	}
	if !strings.Contains(d.Stat, "c.txt") ||
		!strings.Contains(d.Patch, "third") {
		t.Errorf("the changed file is not in the diff: %q", d.Stat)
	}
	if main, _ := r.RemoteMain(dist); d.Parent != main {
		t.Errorf("parent is %q, dist's main is %q", d.Parent, main)
	}
	// and a diff publishes nothing.
	if ref, _ := r.Git(
		"rev-parse", "--verify", "-q", Ref("v0.3.1"),
	); ref != "" {
		t.Error("diff prepared a release")
	}
}

// sworn is a made-up never-word for these releases to sweep with: a game
// built without any refuses to release, which most of these tests are
// not about.
var sworn = func() sweep.Never {
	body, err := sweep.Bake("a-salt", []string{"zorblax"})
	if err != nil {
		panic(err)
	}
	return sweep.ParseNever(body)
}()

// a game built without never-words refuses a release and a stack before
// it touches anything: it cannot sweep for names it was never given.
func TestNoNeverWordsNoRelease(t *testing.T) {
	if _, err := Prepare(repo.Repo{}, Options{Tag: "v1"}); err != errNoNeverWords {
		t.Errorf("a release without never-words was not refused: %v", err)
	}
	if _, err := Stack(repo.Repo{}, Options{Tag: "v1"},
		[]Member{{Name: "m", Ref: "v1"}}); err != errNoNeverWords {
		t.Errorf("a stack without never-words was not refused: %v", err)
	}
}

// a never-word in the tree refuses the release, broken across a line or
// not, and the hit says a never-word was there without saying which.
func TestANeverWordInTheTreeRefuses(t *testing.T) {
	r, dist := fixture(t)
	os.WriteFile(filepath.Join(r.Dir, "thanks.md"),
		[]byte("with thanks to\nZorblax, as ever\n"), 0o644)
	r.Git("add", "thanks.md")
	r.GitIn("", "commit", "-q", "-m", "thanks")
	res, err := Prepare(r, Options{Never: sworn, Name: "x", Dist: dist,
		Tag: "v1"})
	if err == nil {
		t.Fatal("a tree with a never-word in it was prepared")
	}
	found := false
	for _, hit := range res.Hits {
		if hit.Kind == sweep.NeverWord && hit.File == "thanks.md" {
			found = true
		}
		if strings.Contains(strings.ToLower(hit.Kind), "zorblax") {
			t.Errorf("a hit names the word: %q", hit.Kind)
		}
	}
	if !found {
		t.Errorf("no never-word hit in thanks.md: %+v", res.Hits)
	}
}
