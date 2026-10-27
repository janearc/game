package sticky_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/sticky"
)

// TestStickOnARoadWithNoClaimStands: a road with no branches holds no
// Claim, so nobody is in the way.
func TestStickOnARoadWithNoClaimStands(t *testing.T) {
	t.Parallel()
	one, _, _ := fixture(t)
	onBranch(t, one, "work")
	empty := filepath.Join(t.TempDir(), "empty.git")
	git(t, one.Dir(), "init", "-q", "--bare", empty)
	made, err := sticky.Stick(one, empty, "ada")
	if err != nil || made.Holder() != "ada" || made.Branch() != "work" {
		t.Fatalf("stick on an empty road: %+v, %v", made, err)
	}
}

func TestStickRefusesUnreadableClaimWithoutCommitting(t *testing.T) {
	t.Parallel()
	work, _, road := fixture(t)
	onBranch(t, work, "work")
	before := git(t, work.Dir(), "rev-parse", "HEAD")
	mkdirClaim(t, work)
	_, err := sticky.Stick(work, road, "ada")
	var pathError *os.PathError
	if !errors.As(err, &pathError) || pathError.Op != "read" {
		t.Fatalf("claim was not refused at the read: %v", err)
	}
	if after := git(t, work.Dir(), "rev-parse", "HEAD"); after != before {
		t.Fatal("failed claim advanced HEAD")
	}
	info, err := os.Stat(work.Path(stickyFile))
	if err != nil || !info.IsDir() {
		t.Fatalf("failed claim replaced its directory: %v", err)
	}
}

func TestStickRefusesUnwritableTreeWithoutCommitting(t *testing.T) {
	t.Parallel()
	work, _, road := fixture(t)
	onBranch(t, work, "work")
	before := git(t, work.Dir(), "rev-parse", "HEAD")
	readOnly(t, work)
	if _, err := sticky.Stick(work, road, "ada"); !says(err, "permission denied") {
		t.Fatalf("unwritable claim: %v", err)
	}
	if after := git(t, work.Dir(), "rev-parse", "HEAD"); after != before {
		t.Fatal("failed write advanced HEAD")
	}
	if _, err := os.Stat(work.Path(stickyFile)); !os.IsNotExist(err) {
		t.Fatalf("failed write left a claim: %v", err)
	}
}

func TestStickRefusesLockedIndexWithoutCommitting(t *testing.T) {
	t.Parallel()
	work, _, road := fixture(t)
	onBranch(t, work, "work")
	before := git(t, work.Dir(), "rev-parse", "HEAD")
	lockIndex(t, work)
	_, err := sticky.Stick(work, road, "ada")
	if !says(err, "git add") || !says(err, "index.lock") {
		t.Fatalf("locked index: %v", err)
	}
	if after := git(t, work.Dir(), "rev-parse", "HEAD"); after != before {
		t.Fatal("failed staging advanced HEAD")
	}
	body, err := os.ReadFile(work.Path(stickyFile))
	if err != nil || string(body) != "ada\n" {
		t.Fatalf("unstaged claim %q: %v", body, err)
	}
}

func TestStickReportsFailedHeadLookupAfterCommit(t *testing.T) {
	work, _, road := fixture(t)
	onBranch(t, work, "work")
	before := git(t, work.Dir(), "rev-parse", "HEAD")
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	body := "#!/bin/sh\nif [ \"$*\" = 'rev-parse HEAD' ]; then\n" +
		"echo 'head lookup refused' >&2; exit 1\nfi\n" +
		"exec \"$REAL_GIT\" \"$@\"\n"
	mode := os.FileMode(0o755)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REAL_GIT", gitPath)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	claim, err := sticky.Stick(work, road, "ada")
	if !says(err, "head lookup refused") || claim.Holder() != "" {
		t.Fatalf("failed lookup returned a claim: %+v %v", claim, err)
	}
	command := exec.Command(gitPath, "-C", work.Dir(), "rev-parse", "HEAD")
	after, err := command.Output()
	if err != nil || strings.TrimSpace(string(after)) == strings.TrimSpace(before) {
		t.Fatalf("fixture did not reach the post-commit lookup: %v", err)
	}
	if body := git(t, work.Dir(), "show", "HEAD:"+stickyFile); body != "ada\n" {
		t.Fatalf("claim was not committed before lookup failed: %q", body)
	}
}
