package main

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
)

// TestSweepNamesTheHistoryItCouldNotRead: the error says the history.
func TestSweepNamesTheHistoryItCouldNotRead(t *testing.T) {
	t.Parallel()
	work := repoAt(swept(t))
	err := verbTo(t, "sweep", sweepVerb, work, blankConfig(), []string{"nowhere"},
		&bytes.Buffer{})

	if err == nil || !strings.HasPrefix(err.Error(),
		"the history of nowhere: ") {
		t.Errorf("a revision that is not there: %v", err)
	}
}

// TestSweepSaysNothingOfHistoryForARevisionThatIsThere: HEAD reads.
func TestSweepSaysNothingOfHistoryForARevisionThatIsThere(t *testing.T) {
	t.Parallel()
	work := repoAt(swept(t))
	err := verbTo(t, "sweep", sweepVerb, work, blankConfig(), nil, &bytes.Buffer{})
	if err != nil && strings.Contains(err.Error(), "the history of") {
		t.Errorf("HEAD: %v", err)
	}
}

// TestCatchUpLeavesADetachedHeadAlone: the road has a branch named HEAD.
func TestCatchUpLeavesADetachedHeadAlone(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	commitFile(t, two, "theirs.txt", "theirs\n", "theirs")
	if _, err := two.Git("push", road, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := two.Git("push", road, "work:refs/heads/HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, err := two.Git("push", road, "work:main"); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Git("checkout", "-q", "--detach"); err != nil {
		t.Fatal(err)
	}
	was, err := one.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var said bytes.Buffer
	if err := catchUp(one, road, &said); err != nil || said.Len() != count.Nothing {
		t.Errorf("detached: %v, said %q", err, said.String())
	}
	now, err := one.Git("rev-parse", "HEAD")
	if err != nil || now != was {
		t.Errorf("the head moved from %s to %s: %v", was, now, err)
	}
}

// TestCatchUpGivesTheErrorOfReadingTheBranch: no road is asked.
func TestCatchUpGivesTheErrorOfReadingTheBranch(t *testing.T) {
	t.Parallel()
	_, _, road, _ := stickyFixture(t)
	fresh := repoAt(t.TempDir())
	_, want := fresh.Branch()
	if want == nil {
		t.Fatal("a folder that is no tree has a branch to read")
	}
	if err := catchUp(fresh, road, io.Discard); err == nil ||
		err.Error() != want.Error() {
		t.Errorf("catchUp: %v, want the error of the branch: %v", err, want)
	}
}

// TestLintExportedSaysTheScratchFolderItCouldNotMake: not the export.
func TestLintExportedSaysTheScratchFolderItCouldNotMake(t *testing.T) {
	work := freshTree(t, "x", map[string]string{"a.md": "x\n"})
	rules, err := lint.Describe(gamed(t, "lint width 40").Lint())
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("TMPDIR", filepath.Join(home, "nowhere"))
	_, err = lintExported(work, "HEAD", rules)
	var made *fs.PathError
	if !errors.As(err, &made) || !strings.HasSuffix(made.Path, "nowhere") {
		t.Errorf("no scratch folder: %v", err)
	}
}

// TestLintExportedMakesItsScratchFolder: no such error comes.
func TestLintExportedMakesItsScratchFolder(t *testing.T) {
	work := freshTree(t, "x", map[string]string{"a.md": "x\n"})
	rules, err := lint.Describe(gamed(t, "lint width 40").Lint())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := lintExported(work, "HEAD", rules); err != nil {
		t.Errorf("a scratch folder to make: %v", err)
	}
}
