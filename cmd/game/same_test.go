package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/janearc/game/internal/repo"
)

// TestTheSameSourcesBuildTheSameBinary: a binary's own hash should say
// which sources made it, and nothing else. goat keys its render cache
// on that hash, so a build that differs from the last build of the
// same tree is a cache that never hits. one clean tree built twice, and
// the same commit cloned to a second path, must come out byte for byte
// the same.
func TestTheSameSourcesBuildTheSameBinary(t *testing.T) {
	one, _, road, _ := stickyFixture(t)
	cfg := cfgFor("ada", road, "")
	os.WriteFile(
		filepath.Join(one.Dir, "go.mod"),
		[]byte("module x\n\ngo 1.21\n"),
		0o644,
	)
	os.MkdirAll(filepath.Join(one.Dir, "cmd", "miami"), 0o755)
	os.WriteFile(
		filepath.Join(one.Dir, "cmd", "miami", "main.go"),
		[]byte("package main\n\nfunc main() { println(\"hi\") }\n"),
		0o644,
	)
	if _, err := one.Git("add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := one.GitIn("", "commit", "-q", "-m", "a binary"); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Git("push", "-q", road, "work"); err != nil {
		t.Fatal(err)
	}
	built := func(dir string) []byte {
		t.Helper()
		os.RemoveAll(filepath.Join(dir, "bin"))
		if err := buildBinaries(repo.Repo{Dir: dir}, cfg); err != nil {
			t.Fatalf("build in %s: %v", dir, err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "bin", "miami"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	first := built(one.Dir)
	second := built(one.Dir)
	if !bytes.Equal(first, second) {
		t.Errorf("one tree built twice differs: %d and %d bytes",
			len(first), len(second))
	}

	// the same commit at another path, as a second clone of the road.
	elsewhere := filepath.Join(t.TempDir(), "a-different-path")
	cmd := exec.Command(
		"git", "clone", "-q", "--branch", "work", road, elsewhere,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	third := built(elsewhere)
	if !bytes.Equal(first, third) {
		t.Errorf("the same commit at another path differs: %d and "+
			"%d bytes", len(first), len(third))
	}
}
