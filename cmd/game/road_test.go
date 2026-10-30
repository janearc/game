package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
)

// roadVerb is the road verbs over one clone and one config, so the
// table of them holds named methods.
type roadVerb struct {
	one repo.Repo
	cfg config.Config
}

// push is game push.
func (verb roadVerb) push() error { return pushRoad(verb.one, verb.cfg, quiet) }

// stick is game stick.
func (verb roadVerb) stick() error { return stick(verb.one, verb.cfg, quiet) }

// sticky is game sticky.
func (verb roadVerb) sticky() error { return follow(verb.one, verb.cfg, quiet) }

// unstick is game unstick.
func (verb roadVerb) unstick() error { return unstick(verb.one, verb.cfg, quiet) }

// TestAMissingRoadIsSaidPlainly: a project block can name a road that
// is not there, a path typed wrong or a repository never made, and
// until now game said nothing until git failed a fetch in its own
// words. every verb that reaches for the road says so first, naming
// the path and the config file it came from.
func TestAMissingRoadIsSaidPlainly(t *testing.T) {
	t.Parallel()
	one, _, road, _ := stickyFixture(t)
	// stuck first, on the real road, so that unstick has a claim to let
	// go of and is refused for the road rather than for having nothing
	// to do. each verb must refuse before it commits anything.
	if err := stick(one, cfgFor(t, "ada", road, ""), quiet); err != nil {
		t.Fatalf("stick: %v", err)
	}
	before, _ := one.Git("rev-parse", "HEAD")
	gone := filepath.Join(t.TempDir(), "typed-wrong-road.git")
	cfg := cfgFor(t, "ada", gone, "")

	verb := roadVerb{one: one, cfg: cfg}
	tries := map[string]func() error{
		"push":    verb.push,
		"stick":   verb.stick,
		"sticky":  verb.sticky,
		"unstick": verb.unstick,
	}
	for name, try := range tries {
		err := try()
		if err == nil {
			t.Errorf("%s: no error for a road that is not there", name)
			continue
		}
		text := err.Error()
		for _, want := range []string{gone, cfg.Dotfile(), "project x"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the refusal does not say %q: %v",
					name, want, err)
			}
		}
		if strings.Contains(text, "fatal:") ||
			strings.Contains(text, "does not appear") {
			t.Errorf("%s: that is git's error, not game's: %v",
				name, err)
		}
	}
	if after, _ := one.Git("rev-parse", "HEAD"); after != before {
		t.Error("a verb refused for its road still made a commit")
	}
}

// a relative road is looked for from the repository, as git would, and
// the refusal says where it looked; a url is not a path on this machine
// and is not judged by whether one exists here.
func TestAMissingRoadSaysWhereItLooked(t *testing.T) {
	t.Parallel()
	one, _, _, _ := stickyFixture(t)
	gone := filepath.Join(t.TempDir(), "typed-wrong-road.git")
	cfg := cfgFor(t, "ada", "../typed-wrong-road.git", "")
	err := pushRoad(one, cfg, quiet)
	if err == nil || !strings.Contains(err.Error(),
		one.Path("..", "typed-wrong-road.git")) {
		t.Errorf("a relative road's refusal does not say where it "+
			"looked: %v", err)
	}
	cfg = cfgFor(t, "ada", gone, "")
	if err := there("ssh://git@example.invalid/x.git",
		one.Dir(), "x", cfg.Dotfile()); err != nil {
		t.Errorf("a url road was called missing: %v", err)
	}
}

// TestABuildWhileStuckNeedsItsRoad: a stuck tree syncs before it
// builds, and a road that is not there is said so, not skipped and not
// left to git.
func TestABuildWhileStuckNeedsItsRoad(t *testing.T) {
	t.Parallel()
	one, _, road, _ := stickyFixture(t)
	if err := stick(one, cfgFor(t, "ada", road, ""), quiet); err != nil {
		t.Fatalf("stick: %v", err)
	}
	gone := filepath.Join(t.TempDir(), "moved-away.git")
	cfg := cfgFor(t, "ada", gone, "")
	err := syncBuild(one, cfg, io.Discard)
	if err == nil || !strings.Contains(err.Error(), gone) {
		t.Fatalf("a stuck build with a missing road: %v", err)
	}
}
