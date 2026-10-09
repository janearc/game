package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestAMissingRoadIsSaidPlainly: a project block can name a road that
// is not there, a path typed wrong or a repository never made, and
// until now game said nothing until git failed a fetch in its own
// words. every verb that reaches for the road says so first, naming
// the path and the config file it came from.
func TestAMissingRoadIsSaidPlainly(t *testing.T) {
	one, _, road, _ := stickyFixture(t)
	// stuck first, on the real road, so that unstick has a claim to let
	// go of and is refused for the road rather than for having nothing
	// to do. each verb must refuse before it commits anything.
	if err := doStick(one, cfgFor("ada", road, "")); err != nil {
		t.Fatalf("stick: %v", err)
	}
	before, _ := one.Git("rev-parse", "HEAD")
	gone := filepath.Join(t.TempDir(), "typed-wrong-road.git")
	cfg := cfgFor("ada", gone, "")
	cfg.Dotfile = "/somewhere/.config/game/config"

	for name, try := range map[string]func() error{
		"push":    func() error { return pushRoad(one, cfg) },
		"stick":   func() error { return doStick(one, cfg) },
		"sticky":  func() error { return doSticky(one, cfg) },
		"unstick": func() error { return doUnstick(one, cfg) },
	} {
		err := try()
		if err == nil {
			t.Errorf("%s: no error for a road that is not there", name)
			continue
		}
		text := err.Error()
		for _, want := range []string{gone, cfg.Dotfile, "project x"} {
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

	// a relative road is looked for from the repository, as git would,
	// and the refusal says where it looked.
	cfg = cfgFor("ada", "../typed-wrong-road.git", "")
	err := pushRoad(one, cfg)
	if err == nil || !strings.Contains(err.Error(),
		filepath.Join(one.Dir, "..", "typed-wrong-road.git")) {
		t.Errorf("a relative road's refusal does not say where it "+
			"looked: %v", err)
	}

	// a road that is a url is not a path on this machine, and is not
	// judged by whether one exists here.
	if err := there("ssh://git@example.invalid/x.git",
		one.Dir, "x", cfg.Dotfile); err != nil {
		t.Errorf("a url road was called missing: %v", err)
	}
}

// TestABuildWhileStuckNeedsItsRoad: a stuck tree syncs before it
// builds, and a road that is not there is said so, not skipped and not
// left to git.
func TestABuildWhileStuckNeedsItsRoad(t *testing.T) {
	one, _, road, _ := stickyFixture(t)
	if err := doStick(one, cfgFor("ada", road, "")); err != nil {
		t.Fatalf("stick: %v", err)
	}
	gone := filepath.Join(t.TempDir(), "moved-away.git")
	cfg := cfgFor("ada", gone, "")
	err := syncBuild(one, cfg)
	if err == nil || !strings.Contains(err.Error(), gone) {
		t.Fatalf("a stuck build with a missing road: %v", err)
	}
}
