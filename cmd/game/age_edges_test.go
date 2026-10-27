package main

import (
	"testing"
	"time"
)

// age says the commit and how long ago it was made, and says plainly
// what a binary not built by game lacks.
func TestAgeSaysWhatTheStampHolds(t *testing.T) {
	t.Parallel()
	base, err := time.Parse(time.RFC3339, "2026-10-16T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(time.Hour)
	for _, each := range []struct{ build, built, want string }{
		{"dev", "", "game, built from a tree by go rather than by game: " +
			"no commit and no commit time in it"},
		{"abc1234", "", "game abc1234, with no commit time in it"},
		{"dev", "2026-10-16T11:00:00Z", "game dev, committed 1h0m0s ago"},
		{"abc1234", "yesterday", "game abc1234, committed at yesterday"},
		{"abc1234", "2026-10-16T11:59:30Z",
			"game abc1234, committed 30s ago"},
	} {
		if got := ageOf(each.build, each.built, now); got != each.want {
			t.Errorf("%q %q: %q, want %q", each.build, each.built, got,
				each.want)
		}
	}
}

// a remote is itself, and none is said to be none, with where to add it.
func TestOrNoneSaysWhereToAddOne(t *testing.T) {
	t.Parallel()
	if got := orNone("road.git", ".game"); got != "road.git" {
		t.Errorf("a remote: %q", got)
	}
	if got := orNone("", ".game"); got != "(none; add it to .game)" {
		t.Errorf("none: %q", got)
	}
}
