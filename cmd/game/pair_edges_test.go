package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sticky"
)

// refusing makes a bare road refuse every push.
func refusing(t *testing.T, road string) {
	t.Helper()
	hook := filepath.Join(road, "hooks", "pre-receive")
	write(t, hook, "#!/bin/sh\necho no >&2\nexit 1\n")
	openScript := os.FileMode(0o755)
	if err := os.Chmod(hook, openScript); err != nil {
		t.Fatal(err)
	}
}

// unborn is a repository with no commit, so no branch to name.
func unborn(t *testing.T) repo.Repo {
	t.Helper()
	return cleaned(t, "unborn", nil)
}

// has is whether err says want.
func has(err error, want string) bool {
	return err != nil && strings.Contains(err.Error(), want)
}

// stick claims, says so, and pushes the claim, saying where.
func TestStickSaysItStuckAndPushed(t *testing.T) {
	t.Parallel()
	one, _, road, _ := stickyFixture(t)
	var out bytes.Buffer
	if err := stickTo(one, cfgFor(t, "ada", road, ""), &out); err != nil {
		t.Fatal(err)
	}
	want := "stick: stuck on work by ada\npushed work to the road, " +
		road + "\n"
	if out.String() != want {
		t.Errorf("said %q, want %q", out.String(), want)
	}
}

// stick refuses with no author, no road, a branch it cannot catch up,
// main, and a road that will not take the claim, each in its own words.
func TestStickRefusesInItsOwnWords(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, "")
	if err := stickTo(one, cfgFor(t, "", road, ""),
		&bytes.Buffer{}); !has(err, "stick needs an author") {
		t.Errorf("no author: %v", err)
	}
	if err := stickTo(one, dotted(t, "author ada"),
		&bytes.Buffer{}); !has(err, "project blocks") {
		t.Errorf("no road: %v", err)
	}
	// one's work is on the road; two's own commit diverges from it
	commitFile(t, one, "a1.txt", "one\n", "one's")
	if _, err := one.Git("push", "-q", road, "work"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, two, "a2.txt", "two\n", "two's")
	if err := stickTo(two, cfg, &bytes.Buffer{}); !has(err, "diverged") {
		t.Errorf("a branch it cannot catch up: %v", err)
	}
	if _, ok, _ := sticky.Stuck(two.Dir()); ok {
		t.Error("stuck on a branch it could not catch up")
	}
	gitAt(t, one.Dir(), "checkout", "-q", "main")
	if err := stickTo(one, cfg, &bytes.Buffer{}); !has(err, "refuses main") {
		t.Errorf("main: %v", err)
	}
	gitAt(t, one.Dir(), "checkout", "-q", "work")
	refusing(t, road)
	if err := stickTo(one, cfg, &bytes.Buffer{}); !has(err,
		"stuck here, but the claim did not reach the road") {
		t.Errorf("a road that refuses: %v", err)
	}
}

// sticky follows the road's tip and says where it stuck.
func TestStickySaysWhereItStuck(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, "")
	if err := stickyTo(two, blankConfig(),
		&bytes.Buffer{}); !has(err, "project blocks") {
		t.Errorf("no road: %v", err)
	}
	if err := stickyTo(unborn(t), cfg, &bytes.Buffer{}); !has(err,
		"no branch to name") {
		t.Errorf("no branch: %v", err)
	}
	if err := stickyTo(two, cfg, &bytes.Buffer{}); err == nil {
		t.Error("stuck to a branch the road does not have")
	}
	commitFile(t, one, "more.txt", "more\n", "more")
	if _, err := one.Git("push", "-q", road, "work"); err != nil {
		t.Fatal(err)
	}
	tip := gitAt(t, one.Dir(), "rev-parse", "HEAD")
	var out bytes.Buffer
	if err := stickyTo(two, cfg, &out); err != nil ||
		out.String() != "sticky: stuck to "+repo.Short(tip)+"\n" {
		t.Errorf("%v, said %q", err, out.String())
	}
}

// unstick lets go, says so, and pushes; it refuses with no road, no
// branch, a road it cannot read, work it has not taken, no claim, and a
// road that will not take the release.
func TestUnstickSaysWhatItDidOrWhyNot(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, "")
	if err := unstickTo(one, blankConfig(),
		&bytes.Buffer{}); !has(err, "project blocks") {
		t.Errorf("no road: %v", err)
	}
	if err := unstickTo(unborn(t), cfg, &bytes.Buffer{}); !has(err,
		"no branch to name") {
		t.Errorf("no branch: %v", err)
	}
	gone := cfgFor(t, "ada", t.TempDir(), "")
	if err := unstickTo(one, gone, &bytes.Buffer{}); err == nil {
		t.Error("unstuck against a road it could not read")
	}
	if err := unstickTo(one, cfg, &bytes.Buffer{}); !has(err,
		"not stuck") {
		t.Errorf("no claim: %v", err)
	}
	if err := doStick(one, cfg); err != nil {
		t.Fatal(err)
	}
	if err := doSticky(two, cfg); err != nil {
		t.Fatal(err)
	}
	commitFile(t, one, "more.txt", "more\n", "more")
	if _, err := one.Git("push", "-q", road, "work"); err != nil {
		t.Fatal(err)
	}
	if err := unstickTo(two, cfg, &bytes.Buffer{}); !has(err,
		"pull or build first") {
		t.Errorf("work not taken: %v", err)
	}
	var out bytes.Buffer
	if err := unstickTo(one, cfg, &out); err != nil {
		t.Fatal(err)
	}
	tip := gitAt(t, one.Dir(), "rev-parse", "HEAD")
	want := "unstick: no longer sticky " + repo.Short(tip) + "; cleared for " +
		"publish\npushed work to the road, " + road + "\n"
	if out.String() != want {
		t.Errorf("said %q, want %q", out.String(), want)
	}
	if err := doStick(one, cfg); err != nil {
		t.Fatal(err)
	}
}

// an unstick against a road it cannot read leaves the claim where it
// was, and a road that refuses the release still carries it.
func TestUnstickLeavesTheClaimWhenTheRoadRefuses(t *testing.T) {
	t.Parallel()
	one, _, road, _ := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, "")
	if err := doStick(one, cfg); err != nil {
		t.Fatal(err)
	}
	gone := cfgFor(t, "ada", t.TempDir(), "")
	if err := unstickTo(one, gone, &bytes.Buffer{}); err == nil ||
		has(err, "unstuck here") {
		t.Errorf("unstuck against a road it could not read: %v", err)
	}
	if _, ok, err := sticky.Stuck(one.Dir()); err != nil || !ok {
		t.Errorf("the claim went with a road it could not read: %v", err)
	}
	refusing(t, road)
	if err := unstickTo(one, cfg, &bytes.Buffer{}); !has(err,
		"unstuck here, but the road still carries the claim") {
		t.Errorf("a road that refuses: %v", err)
	}
}

// a release push on a claimed tree is refused: unstick first, or take
// the road's work first, or say the road could not be read.
func TestStuckPushSaysWhatToDoFirst(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	err := stuckPush(unborn(t), cfgFor(t, "ada", road, ""))
	if !has(err, "no branch to name") {
		t.Errorf("no branch: %v", err)
	}
	if err := stuckPush(one, cfgFor(t, "ada", "", "d.git")); !has(err,
		"unstick first, then release again") {
		t.Errorf("no road: %v", err)
	}
	nowhere := filepath.Join(t.TempDir(), "no.git")
	if err := stuckPush(one, cfgFor(t, "ada", nowhere, "")); !has(err,
		"is not there") {
		t.Errorf("a road not there: %v", err)
	}
	gone := cfgFor(t, "ada", t.TempDir(), "")
	if err := stuckPush(one, gone); !has(err,
		"stuck, and the road could not be read") {
		t.Errorf("a road it cannot read: %v", err)
	}
	commitFile(t, one, "more.txt", "more\n", "more")
	if _, err := one.Git("push", "-q", road, "work"); err != nil {
		t.Fatal(err)
	}
	if err := stuckPush(two, cfgFor(t, "ada", road, "")); !has(err,
		"pull or build first") {
		t.Errorf("behind the road: %v", err)
	}
	if err := stuckPush(one, cfgFor(t, "ada", road, "")); !has(err,
		"unstick first, then release again") {
		t.Errorf("even with the road: %v", err)
	}
}

// push sends the branch to the road and says so; it refuses a road that
// is dist, a dist it cannot guard, and a head on no branch.
func TestPushRoadSaysWhereAndRefusesWhatItMust(t *testing.T) {
	t.Parallel()
	one, _, road, _ := stickyFixture(t)
	var out bytes.Buffer
	if err := pushRoadTo(one, cfgFor(t, "ada", road, ""), &out); err != nil ||
		out.String() != "pushed work to the road, "+road+"\n" {
		t.Errorf("%v, said %q", err, out.String())
	}
	if err := pushRoadTo(one, cfgFor(t, "ada", road, road),
		&bytes.Buffer{}); !has(err, "are the same remote") {
		t.Errorf("road is dist: %v", err)
	}
	// hooks it cannot write to cannot take the guard, so nothing goes
	hooks := filepath.Join(t.TempDir(), "hooks")
	shutDir := os.FileMode(0o555)
	if err := os.Mkdir(hooks, shutDir); err != nil {
		t.Fatal(err)
	}
	gitAt(t, one.Dir(), "config", "core.hooksPath", hooks)
	out.Reset()
	if err := pushRoadTo(one, cfgFor(t, "ada", road, "d.git"),
		&out); err == nil || out.String() != "" {
		t.Errorf("unguarded: %v, said %q", err, out.String())
	}
	gitAt(t, one.Dir(), "config", "--unset", "core.hooksPath")
	gitAt(t, one.Dir(), "checkout", "-q", "--detach")
	if err := pushRoadTo(one, cfgFor(t, "ada", road, ""),
		&bytes.Buffer{}); !has(err, "not on a branch") {
		t.Errorf("detached: %v", err)
	}
}
