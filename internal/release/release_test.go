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
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(priv, 0o755)
	run(priv, "init", "-q", "-b", "main")
	run(priv, "config", "commit.gpgsign", "false")
	os.WriteFile(filepath.Join(priv, "a.txt"), []byte("hello\n"), 0o644)
	run(priv, "add", "a.txt")
	run(priv, "commit", "-q", "-m", "one\n\nCo-Auth"+"ored-By: someone <x@y.z>")
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

// the first release is one commit with no parent and HEAD's tree; the
// same tree again is nothing to release; a changed tree is a second
// commit parented on the first, and the trailer in the private history
// never reaches the public one.
func TestReleaseAccumulates(t *testing.T) {
	r, pub := fixture(t)
	o := Options{Public: pub, Message: "as it stands"}
	res, err := Run(r, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped || res.Commit == "" {
		t.Fatalf("first release: %+v", res)
	}
	if n := count(t, r, "flat"); n != 1 {
		t.Errorf("flat has %d commits, want 1", n)
	}
	if tree, _ := r.Tree("HEAD"); tree != res.Tree {
		t.Errorf("tree mismatch")
	}
	res2, err := Run(r, o)
	if err != nil || !res2.Skipped {
		t.Fatalf("second release should skip: %+v %v", res2, err)
	}
	os.WriteFile(filepath.Join(r.Dir, "c.txt"), []byte("more\n"), 0o644)
	r.Git("add", "c.txt")
	r.GitIn("", "commit", "-q", "-m", "three")
	res3, err := Run(r, o)
	if err != nil || res3.Skipped {
		t.Fatalf("third release: %+v %v", res3, err)
	}
	if n := count(t, r, "flat"); n != 2 {
		t.Errorf("flat has %d commits after a change, want 2", n)
	}
	log, _ := r.Git("log", "flat", "--format=%B")
	if strings.Contains(log, "Co-Auth"+"ored-By") {
		t.Error("the trailer reached the public history")
	}
}

// a dirty tree is refused before anything is built; a planted machine
// path is found and the push does not happen; an excluded prefix is
// left alone.
func TestReleaseRefuses(t *testing.T) {
	r, pub := fixture(t)
	os.WriteFile(filepath.Join(r.Dir, "dirty.txt"), []byte("x"), 0o644)
	if _, err := Run(r, Options{Public: pub, Message: "m"}); err == nil {
		t.Fatal("a dirty tree released")
	}
	os.Remove(filepath.Join(r.Dir, "dirty.txt"))
	os.WriteFile(filepath.Join(r.Dir, "notes.md"), []byte("see /Us"+"ers/someone/thing\n"), 0o644)
	r.Git("add", "notes.md")
	r.GitIn("", "commit", "-q", "-m", "a path")
	res, err := Run(r, Options{Public: pub, Message: "m"})
	if err == nil || len(res.Hits) == 0 {
		t.Fatalf("a machine path was not found: %+v %v", res, err)
	}
	if main, _ := r.RemoteMain(pub); main != "" {
		t.Error("a refused release was pushed")
	}
	res, err = Run(r, Options{Public: pub, Message: "m", Exclude: []string{"notes.md"}})
	if err != nil {
		t.Fatalf("an excluded file still refused: %v", err)
	}
	if main, _ := r.RemoteMain(pub); main != res.Commit {
		t.Error("the release did not land")
	}
}

// after a release the mark is set, so the next release's notes are the
// subjects since; a subject that names the author refuses the notes;
// a tag lands on the public root at the release.
func TestNotesMarkAndTag(t *testing.T) {
	r, pub := fixture(t)
	if notes, _, _ := Notes(r, pub, nil, "Ada"); len(notes) != 0 {
		t.Fatalf("notes before any release: %v", notes)
	}
	if _, err := Run(r, Options{Public: pub, Message: "first", Tag: "v0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if tags, _ := r.Git("ls-remote", "--tags", pub); !strings.Contains(tags, "v0.0.1") {
		t.Errorf("the tag did not land: %q", tags)
	}
	os.WriteFile(filepath.Join(r.Dir, "c.txt"), []byte("more\n"), 0o644)
	r.Git("add", "c.txt")
	r.GitIn("", "commit", "-q", "-m", "the corner follows the phone")
	notes, hits, err := Notes(r, pub, nil, "Ada")
	if err != nil || len(notes) != 1 || notes[0] != "the corner follows the phone" || len(hits) != 0 {
		t.Fatalf("notes: %v %v %v", notes, hits, err)
	}
	if other, _, _ := Notes(r, filepath.Join(filepath.Dir(pub), "other-root.git"), nil, "Ada"); len(other) != 0 {
		t.Errorf("another root should have its own mark, and none yet: %v", other)
	}
	os.WriteFile(filepath.Join(r.Dir, "d.txt"), []byte("x\n"), 0o644)
	r.Git("add", "d.txt")
	r.GitIn("", "commit", "-q", "-m", "Ada wanted this one")
	_, hits, _ = Notes(r, pub, nil, "Ada")
	if len(hits) == 0 {
		t.Error("a subject naming the author was not bounced")
	}
}

// a repository that kept the first releases' single mark releases
// cleanly: the old ref is retired and the per-root mark takes its place.
func TestOldMarkRetired(t *testing.T) {
	r, pub := fixture(t)
	if _, err := r.Git("update-ref", "refs/game/released", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(r, Options{Public: pub, Message: "m"}); err != nil {
		t.Fatalf("release with the old mark present: %v", err)
	}
	if _, err := r.Git("rev-parse", "--verify", "-q", Mark(pub)); err != nil {
		t.Error("the per-root mark was not set")
	}
	if _, err := r.Git("rev-parse", "--verify", "-q", "refs/game/released"); err == nil {
		t.Error("the old mark is still there")
	}
}

// the author's name in the tree refuses a release, the way the bounce
// refuses it in a change; the handle in a module path is not the name.
func TestAuthorInTheTree(t *testing.T) {
	r, pub := fixture(t)
	os.WriteFile(filepath.Join(r.Dir, "notes.md"), []byte("Ada asked for this.\nmodule github.com/adalovelace/x\n"), 0o644)
	r.Git("add", "notes.md")
	r.GitIn("", "commit", "-q", "-m", "notes")
	res, err := Run(r, Options{Public: pub, Message: "m", Author: "Ada"})
	if err == nil || len(res.Hits) == 0 {
		t.Fatalf("the author's name in the tree released: %+v %v", res, err)
	}
	os.WriteFile(filepath.Join(r.Dir, "notes.md"), []byte("the operation as asked for.\nmodule github.com/adalovelace/x\n"), 0o644)
	r.Git("add", "notes.md")
	r.GitIn("", "commit", "-q", "-m", "first person")
	if _, err := Run(r, Options{Public: pub, Message: "m", Author: "Ada"}); err != nil {
		t.Fatalf("the handle was taken for the name: %v", err)
	}
}
