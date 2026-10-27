package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCleanLeavesTrackedBinFiles is here because a clean that removed bin/
// wholesale would have deleted flipr's deploy.sh and the whole of
// kingfisher's bin/, which are source and are tracked. build output goes;
// anything git knows about stays.
func TestCleanLeavesTrackedBinFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	work := repoAt(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		if _, err := work.Git(args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	openDir := os.FileMode(0o755)
	openScript := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), openDir); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(dir, "bin", "deploy.sh")
	if err := os.WriteFile(kept, []byte("#!/bin/sh\n"), openScript); err != nil {
		t.Fatal(err)
	}
	if _, err := work.Git("add", "bin/deploy.sh"); err != nil {
		t.Fatal(err)
	}
	if _, err := work.Git("commit", "-q", "-m", "a script"); err != nil {
		t.Fatal(err)
	}
	built := filepath.Join(dir, "bin", "thing")
	if err := os.WriteFile(built, []byte("binary"), openScript); err != nil {
		t.Fatal(err)
	}
	if err := clean(work, blankConfig(), false); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("tracked bin/deploy.sh was removed: %v", err)
	}
	if _, err := os.Stat(built); err == nil {
		t.Error("bin/thing was built output and is still there")
	}
}
