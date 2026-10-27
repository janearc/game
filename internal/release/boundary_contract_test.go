package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/release"
)

// Stacks whose git cannot list a member's go.mod files are refused. They
// do not go on with no module to put in a workspace.
func TestAStackWhoseModulesCannotBeListedIsRefused(t *testing.T) {
	work, dist, members := stackFixture(t)
	releaseGitFailure(t,
		"[ \"$1\" = ls-tree ] && [ \"$2\" = -r ] && [ \"$3\" = --name-only ]")
	_, err := release.Stack(work, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(), "its modules") {
		t.Fatalf("modules unlisted: %v, want the member's modules named", err)
	}
}

// a module git lists and cannot show contributes nothing, and the rest
// of the stack is made.
func TestAStackSkipsAModuleItCannotRead(t *testing.T) {
	work, dist, members := stackFixture(t)
	releaseGitFailure(t,
		"[ \"$1\" = show ] && [ \"${2##*:}\" = go.mod ]")
	out, err := release.Stack(work, stackOptions(dist), members)
	if err != nil || out.Work() != "" ||
		len(out.Members()) != count.Next(count.One) {
		t.Fatalf("modules unread: work %q, %v", out.Work(), err)
	}
}

// with dist holding a main, the stack fetches it before it commits, and
// a fetch that fails is the stack's failure, no ref left.
func TestAStackRefusesWhenDistsMainCannotBeFetched(t *testing.T) {
	work, dist, members := stackFixture(t)
	first, err := release.Stack(work, stackOptions(dist), members)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, work.Dir(), "push", "-q", dist, first.Commit()+":refs/heads/main")
	gitIn(t, work.Dir(), "update-ref", "-d", refOf("v1"))
	releaseGitFailure(t,
		"[ \"$1\" = fetch ] && [ \"$3\" != --no-tags ]")
	_, err = release.Stack(work, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(), "release boundary refused") {
		t.Fatalf("a failed fetch of dist's main: %v", err)
	}
	if _, err := work.Git("show-ref", "--verify", refOf("v1")); err == nil {
		t.Error("a failed stack left a ref")
	}
}

// the stack's scratch index is removed when the read of the stack's own
// tree into it fails.
func TestAStackRemovesItsIndexWhenReadingIntoItFails(t *testing.T) {
	work, dist, members := stackFixture(t)
	log := filepath.Join(t.TempDir(), "index-name")
	actual, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	body := "#!/bin/sh\nif [ \"$1\" = read-tree ] && " +
		"[ \"${2#--prefix}\" = \"$2\" ]; then\n" +
		"printf '%s' \"$GIT_INDEX_FILE\" > '" + log + "'\n" +
		"echo 'release boundary refused' >&2; exit 1\nfi\n" +
		"exec '" + actual + "' \"$@\"\n"
	mode := os.FileMode(0o755)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err = release.Stack(work, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(), "release boundary refused") {
		t.Fatalf("a failed read into the index: %v", err)
	}
	noted, readErr := os.ReadFile(log)
	if readErr != nil || len(noted) == count.Nothing {
		t.Fatalf("the index was never named: %q, %v", noted, readErr)
	}
	if _, statErr := os.Stat(string(noted)); !os.IsNotExist(statErr) {
		t.Fatalf("failed index remains at %q: %v", noted, statErr)
	}
}
