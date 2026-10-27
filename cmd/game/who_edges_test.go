package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// a repository with no commits has no branch to name, so who says no
// line about one before it refuses.
func TestWhoWithNoBranchSaysNoneBeforeItRefuses(t *testing.T) {
	t.Parallel()
	empty := filepath.Join(t.TempDir(), "empty")
	gitAt(t, filepath.Dir(empty), "init", "-q", "-b", "main", empty)
	var out bytes.Buffer
	err := who(repoAt(empty), blankConfig(), &out)
	if err == nil || strings.Contains(out.String(), "last three") {
		t.Errorf("err %v, said:\n%s", err, out.String())
	}
}

// a branch whose history git cannot walk is refused, not listed as
// nothing: a shallow clone that has lost the file saying it is shallow.
func TestWhoRefusesAHistoryGitCannotRead(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	whole := gitTree(t, filepath.Join(root, "whole"),
		map[string]string{"a.txt": "one\n"})
	gitAt(t, whole.Dir(), "commit", "-q", "--allow-empty", "-m", "two")
	cut := filepath.Join(root, "cut")
	gitAt(t, root, "clone", "-q", "--depth", "1", "file://"+whole.Dir(), cut)
	if err := os.Remove(filepath.Join(cut, ".git", "shallow")); err != nil {
		t.Fatal(err)
	}
	err := who(repoAt(cut), blankConfig(), &bytes.Buffer{})
	if err == nil {
		t.Error("a history git could not read was listed")
	}
}

// a dist with nothing on it yet says so.
func TestWhoSaysADistWithNothingOnIt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tree := gitTree(t, filepath.Join(root, "own"),
		map[string]string{"a.txt": "one\n"})
	dist := filepath.Join(root, "dist.git")
	gitAt(t, root, "init", "-q", "--bare", dist)
	cfg := loadedAs(t, "x", block("x", "", dist), nil)
	var out bytes.Buffer
	if err := who(tree, cfg, &out); err != nil {
		t.Fatal(err)
	}
	want := "released: nothing on " + dist + " yet\n"
	if !strings.HasPrefix(out.String(), want) {
		t.Errorf("want %q first:\n%s", want, out.String())
	}
}

// a commit whose signature git verifies is vouched for by the key's
// holder, read from the place git puts it, whatever the subject.
func TestDescribeNamesTheHolderOfAVerifiedKey(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	key := filepath.Join(root, "key")
	made, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519",
		"-N", "", "-C", "holder", "-f", key).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, made)
	}
	public, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(root, "allowed")
	line := `holder@example.invalid namespaces="git" ` + string(public)
	openFile := os.FileMode(0o600)
	if err := os.WriteFile(allowed, []byte(line), openFile); err != nil {
		t.Fatal(err)
	}
	tree := gitTree(t, filepath.Join(root, "own"),
		map[string]string{"a.txt": "one\n"})
	gitAt(t, tree.Dir(), "config", "gpg.ssh.allowedSignersFile", allowed)
	// the key is this test's own, and signs this one commit
	gitAt(t, tree.Dir(), "-c", "gpg.format=ssh", "-c", "user.signingkey="+key,
		"-c", "gpg.ssh.program=ssh-keygen", "commit", "-q", "-S",
		"--allow-empty", "-m", "the subject")
	said, err := describe(tree, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(said,
		" signed by holder@example.invalid  the subject") {
		t.Errorf("said %q", said)
	}
}
