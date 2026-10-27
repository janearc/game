package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// a diff says when dist already has the tree; else its two ends, short,
// the stat, what the sweep would refuse, and the patch.
func TestTellDiffSaysEachPart(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	tellDiff(&out, "x v1", &differing{same: true})
	if want := "release diff x v1: dist already has this tree, so a " +
		"release would change nothing\n"; out.String() != want {
		t.Errorf("the same: %q", out.String())
	}
	tree := fakeObject
	parent := strings.ReplaceAll(fakeObject, "ab", "cd")
	for from, want := range map[string]string{
		"":     "an empty dist",
		parent: repo.Short(parent),
	} {
		out.Reset()
		tellDiff(&out, "x", &differing{tree: tree, parent: from,
			stat: "a | 1 +", patch: "the patch\n",
			hits: []sweep.Hit{sweep.Found("a never-word", "a")}})
		said := "release diff x: " + want + " -> " + repo.Short(tree) + "\n\n" +
			" a | 1 +\nwould be refused: a never-word in a\n\nthe patch\n"
		if out.String() != said {
			t.Errorf("from %q: said %q, want %q", from, out.String(), said)
		}
	}
}

// release diff shows what a release now would send, named with its tag
// when given one, and says when no never-words were looked for.
func TestReleaseDiffSaysWhatWouldGo(t *testing.T) {
	t.Parallel()
	one, _, road, dist := stickyFixture(t)
	cfg := cfgFor(t, "ada", road, dist)
	for _, each := range []struct {
		tag   string
		never sweep.Never
		first string
	}{
		{"v1", words(t), "release diff x v1: an empty dist -> "},
		{"", words(t), "release diff x: an empty dist -> "},
		{"", sweep.Baked(fstest.MapFS{}), "release diff x: this game was built " +
			"without never-words"},
	} {
		var out bytes.Buffer
		if err := releaseDiffTo(one, cfg, each.tag, each.never,
			&out); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.String(), each.first) {
			t.Errorf("%q: said %q", each.tag, out.String())
		}
	}
	if err := releaseDiffTo(one, cfgFor(t, "ada", road, ""), "", words(t),
		&bytes.Buffer{}); err == nil ||
		!strings.Contains(err.Error(), "nothing to diff against") {
		t.Errorf("a diff with no dist: %v", err)
	}
	if err := releaseDiffTo(cleaned(t, "x", nil), cfg, "", words(t),
		&bytes.Buffer{}); err == nil {
		t.Error("a diff of a tree with no commit was made")
	}
}

// guard writes game's pre-push hook where git keeps hooks, executable
// and naming dist; it leaves a hook that is not game's, and says so.
func TestGuardWritesTheHookOrSaysWhyNot(t *testing.T) {
	t.Parallel()
	work := freshTree(t, "x", map[string]string{"a": "a\n"})
	gitAt(t, work.Dir(), "config", "core.hooksPath", "own-hooks")
	var out bytes.Buffer
	if err := guard(work, "", &out); err != nil ||
		count.Positive(out.Len()) {
		t.Errorf("no dist: %v, said %q", err, out.String())
	}
	hook := work.Path("own-hooks", "pre-push")
	if _, err := os.Stat(hook); err == nil {
		t.Error("a hook was written for no dist")
	}
	if err := guard(work, "it's/dist.git", &out); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(hook)
	if err != nil || !strings.Contains(string(written), hookMark) ||
		!strings.Contains(string(written), `dist='it'\''s/dist.git'`) {
		t.Errorf("wrote %q, %v", written, err)
	}
	openScript := os.FileMode(0o755)
	for _, at := range []string{filepath.Dir(hook), hook} {
		if info, err := os.Stat(at); err != nil ||
			info.Mode().Perm() != openScript {
			t.Errorf("%s: mode %v, %v", at, info.Mode(), err)
		}
	}
	if err := guard(work, "other.git", &out); err != nil {
		t.Fatal(err)
	}
	if written, _ := os.ReadFile(hook); !strings.Contains(string(written),
		"dist='other.git'") {
		t.Errorf("game's own hook was not rewritten: %q", written)
	}
	write(t, hook, "#!/bin/sh\necho mine\n")
	out.Reset()
	if err := guard(work, "dist.git", &out); err != nil ||
		out.String() != "guard: "+hook+" is not game's; dist is not "+
			"guarded here\n" {
		t.Errorf("someone's hook: %v, said %q", err, out.String())
	}
	if kept, _ := os.ReadFile(hook); string(kept) != "#!/bin/sh\necho mine\n" {
		t.Errorf("someone's hook was changed: %q", kept)
	}
}

// guard outside git, or where the hooks cannot go, is the error.
func TestGuardRefusesWhereItCannotWrite(t *testing.T) {
	t.Parallel()
	outside := repoAt(t.TempDir())
	if err := guard(outside, "dist.git", &bytes.Buffer{}); err == nil {
		t.Error("a hook was guarded outside git")
	}
	work := freshTree(t, "x",
		map[string]string{"a": "a\n", "blocked": "a file"})
	gitAt(t, work.Dir(), "config", "core.hooksPath", "blocked/hooks")
	if err := guard(work, "dist.git", &bytes.Buffer{}); err == nil {
		t.Error("a hook was written under a file")
	}
}

// trees says the block's road and dist, or that there is no block, and
// guards the dist.
func TestTreesSaysTheBlock(t *testing.T) {
	t.Parallel()
	work := freshTree(t, "x", map[string]string{"a": "a\n"})
	var out bytes.Buffer
	cfg := loadedAs(t, "", nil, nil)
	if err := treesTo(work, cfg, &out); err != nil {
		t.Fatal(err)
	}
	want := "a local build: " + cfg.NoBlock().Error() + "\nroad: (none; " +
		"add it to " + cfg.Dotfile() + ")\ndist: (none; add it to " +
		cfg.Dotfile() + ")\n"
	if out.String() != want {
		t.Errorf("no block: said %q, want %q", out.String(), want)
	}
	out.Reset()
	if err := treesTo(work, cfgFor(t, "ada", "road.git", "dist.git"),
		&out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "road: road.git\ndist: dist.git\n" {
		t.Errorf("a block: said %q", out.String())
	}
	gitAt(t, work.Dir(), "config", "core.hooksPath", "a/hooks")
	if err := treesTo(work, cfgFor(t, "ada", "road.git", "dist.git"),
		&bytes.Buffer{}); err == nil {
		t.Error("a dist that could not be guarded was not an error")
	}
}

// differing is a stand-in difference for tellDiff: what a release would
// change, as a test says it.
type differing struct {
	same                      bool
	tree, parent, stat, patch string
	hits                      []sweep.Hit
}

func (diff *differing) Same() bool        { return diff.same }
func (diff *differing) Parent() string    { return diff.parent }
func (diff *differing) Tree() string      { return diff.tree }
func (diff *differing) Stat() string      { return diff.stat }
func (diff *differing) Hits() []sweep.Hit { return diff.hits }
func (diff *differing) Patch() string     { return diff.patch }
