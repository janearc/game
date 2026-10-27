package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
)

// TestTheSameSourcesBuildTheSameBinary: a binary's own hash should say
// which sources made it and who built it, and nothing else. goat keys
// its render cache on that hash, so a build that differs from the last
// build of the same tree is a cache that never hits. one clean tree
// built twice, and the same commit cloned to a second path, built by
// the same builder, must come out byte for byte the same.
//
// the builder is stamped in for --who: -X writes the string into the
// binary itself, so another builder makes other bytes.
// sameTree is a clone on branch work with the miami binary committed,
// its road, and its config.
func sameTree(t *testing.T) (repo.Repo, string, config.Config) {
	t.Helper()
	one, _, road, _ := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, "")
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(one.Path("go.mod"),
		[]byte("module x\n\ngo 1.21\n"), openFile); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(one.Path("cmd", "miami"), openDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(one.Path("cmd", "miami", "main.go"),
		[]byte("package main\n\nfunc main() { println(\"hi\") }\n"),
		openFile); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Git("add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := one.GitIn("", "commit", "-q", "-m", "a binary"); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Git("push", "-q", road, "work"); err != nil {
		t.Fatal(err)
	}
	return one, road, cfg
}

// builds builds miami from a tree, with the config.
type builds struct {
	t   *testing.T
	cfg config.Config
}

// binary builds the tree at dir and reads the binary it made.
func (builds builds) binary(dir string) []byte {
	builds.t.Helper()
	os.RemoveAll(filepath.Join(dir, "bin"))
	if err := buildBinariesBy(repoAt(dir), builds.cfg,
		"jm/4b732342 on loft", run, io.Discard, false); err != nil {
		builds.t.Fatalf("build in %s: %v", dir, err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "bin", "miami"))
	if err != nil {
		builds.t.Fatal(err)
	}
	return body
}

func TestTheSameSourcesBuildTheSameBinary(t *testing.T) {
	t.Parallel()
	one, _, cfg := sameTree(t)
	built := builds{t: t, cfg: cfg}

	first := built.binary(one.Dir())
	second := built.binary(one.Dir())
	if !bytes.Equal(first, second) {
		t.Errorf("one tree built twice differs: %d and %d bytes",
			len(first), len(second))
	}
}

// the same commit cloned to a second path, built by the same builder,
// comes out byte for byte the same.
func TestTheSameCommitAtAnotherPathBuildsTheSame(t *testing.T) {
	t.Parallel()
	one, road, cfg := sameTree(t)
	built := builds{t: t, cfg: cfg}
	first := built.binary(one.Dir())

	// the same commit at another path, as a second clone of the road.
	elsewhere := filepath.Join(t.TempDir(), "a-different-path")
	cmd := exec.Command(
		"git", "clone", "-q", "--branch", "work", road, elsewhere,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	third := built.binary(elsewhere)
	if !bytes.Equal(first, third) {
		t.Errorf("the same commit at another path differs: %d and "+
			"%d bytes", len(first), len(third))
	}
}
