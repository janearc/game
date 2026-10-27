package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/stack"
)

// stubTool writes a shell script that may be run.
func stubTool(t *testing.T, path, body string) {
	t.Helper()
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
		t.Fatal(err)
	}
	openScript := os.FileMode(0o755)
	if err := os.WriteFile(path, []byte(body), openScript); err != nil {
		t.Fatal(err)
	}
}

// laid is what build stack lays a member's patches with: the stack's
// patches/NAME, through patch.Lay, on the member's clone.
func TestLaidLaysTheStacksPatches(t *testing.T) {
	for _, each := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(each+"_NAME", "game")
		t.Setenv(each+"_EMAIL", "game@test")
	}
	clone, root := t.TempDir(), t.TempDir()
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(filepath.Join(clone, "f"), []byte("a\n"),
		openFile); err != nil {
		t.Fatal(err)
	}
	for _, step := range [][]string{{"init", "-q"}, {"add", "f"},
		{"commit", "-q", "-m", "the tag"}} {
		if out, err := exec.Command("git", append([]string{"-C", clone},
			step...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", step, err, out)
		}
	}
	series := filepath.Join(root, "patches", "member")
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(series, openDir); err != nil {
		t.Fatal(err)
	}
	body := "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+laid\n"
	if err := os.WriteFile(filepath.Join(series, "01.patch"),
		[]byte(body), openFile); err != nil {
		t.Fatal(err)
	}
	laidCount, err := laid(root, nil)(stack.Named("member", ""), clone)
	if err != nil || laidCount != count.One {
		t.Fatalf("laid %d, %v; want the one patch", laidCount, err)
	}
	got, err := os.ReadFile(filepath.Join(clone, "f"))
	if err != nil || string(got) != "laid\n" {
		t.Errorf("f is %q, %v; want the patch laid", got, err)
	}
}

// game patch says plainly what happened to a patch that applied.
func TestLandedSaysWhatHappened(t *testing.T) {
	for _, want := range []struct {
		dry, whole bool
		says       string
	}{
		{false, true, "lands"},
		{true, true, "would land"},
		{false, false, "landed, then taken back with the series"},
		{true, false, "would land, but the series would not"},
	} {
		if got := landed(want.dry, want.whole); got != want.says {
			t.Errorf("dry %v whole %v: %q, want %q", want.dry,
				want.whole, got, want.says)
		}
	}
}

// check formats with the gofmt of the toolchain that builds the module,
// which go may have switched to, not whatever gofmt is on PATH: a newer
// go.mod's syntax is unreadable to an older gofmt.
//
// a stub go on PATH answers GOROOT with a switched toolchain's root, and
// PATH holds a gofmt of its own: the toolchain's must be the one chosen.
func TestGofmtIsTheModulesOwn(t *testing.T) {
	path, switched := t.TempDir(), t.TempDir()
	stubTool(t, filepath.Join(path, "go"),
		"#!/bin/sh\necho "+switched+"\n")
	stubTool(t, filepath.Join(path, "gofmt"), "#!/bin/sh\n")
	own := filepath.Join(switched, "bin", "gofmt")
	stubTool(t, own, "#!/bin/sh\n")
	t.Setenv("PATH", path)
	if got := gofmtFor(t.TempDir(), run, io.Discard); got != own {
		t.Errorf("gofmt is %q, want the switched toolchain's %q", got, own)
	}
	// a toolchain root with no gofmt in it leaves the one on PATH.
	if err := os.Remove(own); err != nil {
		t.Fatal(err)
	}
	if got := gofmtFor(t.TempDir(), run, io.Discard); got != "gofmt" {
		t.Errorf("with no toolchain gofmt: %q, want PATH's", got)
	}
}

// laid reads a member's patches from a directory .game names with a
// patches line, as carrie keeps charm's beside them.
func TestLaidLaysFromANamedDirectory(t *testing.T) {
	for _, each := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(each+"_NAME", "game")
		t.Setenv(each+"_EMAIL", "game@test")
	}
	clone, root := t.TempDir(), t.TempDir()
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(filepath.Join(clone, "f"), []byte("a\n"),
		openFile); err != nil {
		t.Fatal(err)
	}
	for _, step := range [][]string{{"init", "-q"}, {"add", "f"},
		{"commit", "-q", "-m", "the tag"}} {
		if out, err := exec.Command("git", append([]string{"-C", clone},
			step...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", step, err, out)
		}
	}
	series := filepath.Join(root, "charm", "patches", "member")
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(series, openDir); err != nil {
		t.Fatal(err)
	}
	body := "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-a\n+laid\n"
	if err := os.WriteFile(filepath.Join(series, "01.patch"),
		[]byte(body), openFile); err != nil {
		t.Fatal(err)
	}
	laidCount, err := laid(root, []string{"charm/patches"})(
		stack.Named("member", ""), clone)
	if err != nil || laidCount != count.One {
		t.Fatalf("laid %d, %v; want the one patch", laidCount, err)
	}
	got, err := os.ReadFile(filepath.Join(clone, "f"))
	if err != nil || string(got) != "laid\n" {
		t.Errorf("f is %q, %v; want the patch laid", got, err)
	}
}
