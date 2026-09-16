package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/repo"
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
	o := Options{Name: "x", Dist: dist, Tag: "v0.3.0"}
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
	if _, err := Push(r, Options{Name: "x", Dist: dist, Tag: "v1"}); err == nil {
		t.Fatal("pushed a release that was never prepared")
	}
	if _, err := Prepare(r, Options{Name: "x", Dist: dist, Tag: "v1"}); err != nil {
		t.Fatal(err)
	}
	other, _ := r.GitIn("elsewhere\n", "commit-tree", "HEAD^{tree}")
	if _, err := r.Git("push", dist, other+":refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	if _, err := Push(r, Options{Name: "x", Dist: dist, Tag: "v1"}); err == nil {
		t.Error("pushed over a dist main that moved")
	}
}

// a dirty tree, a missing tag, a machine path and a setting's value each
// refuse before anything is kept; an excluded prefix is left alone.
func TestPrepareRefuses(t *testing.T) {
	r, dist := fixture(t)
	if _, err := Prepare(r, Options{Name: "x", Dist: dist}); err == nil {
		t.Error("prepared without a tag")
	}
	os.WriteFile(filepath.Join(r.Dir, "dirty.txt"), []byte("x"), 0o644)
	if _, err := Prepare(r, Options{Name: "x", Dist: dist, Tag: "v1"}); err == nil {
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
	res, err := Prepare(r, Options{Name: "x", Dist: dist, Tag: "v1"})
	if err == nil || len(res.Hits) == 0 {
		t.Fatalf("a machine path was not found: %+v %v", res, err)
	}
	if _, err := r.Git("rev-parse", "--verify", "-q", Ref("v1")); err == nil {
		t.Error("a refused release was kept")
	}
	if _, err := Prepare(r, Options{Name: "x", Dist: dist, Tag: "v1", Exclude: []string{"notes.md"}}); err != nil {
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
	if _, err := Prepare(r, Options{Name: "x", Dist: dist, Tag: "v1", Author: "Ada"}); err != nil {
		t.Fatalf("the licence or the handle refused a release: %v", err)
	}
	os.WriteFile(
		filepath.Join(r.Dir, "README.md"),
		[]byte("Ada wrote this.\n"),
		0o644,
	)
	r.Git("add", "README.md")
	r.GitIn("", "commit", "-q", "-m", "readme")
	if res, err := Prepare(r, Options{Name: "x", Dist: dist, Tag: "v2", Author: "Ada"}); err == nil ||
		len(res.Hits) == 0 {
		t.Fatalf("the author in the readme released: %+v %v", res, err)
	}
}
