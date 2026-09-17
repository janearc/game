package main

import "testing"

// TestGameHome: an agent has an anchor and wants its own roads and dists;
// a person in a shell wants theirs. faking HOME would work and would move
// ssh keys and gh credentials with it, so the override is its own name.
func TestGameHome(t *testing.T) {
	t.Setenv("HOME", "/hers")
	t.Setenv("GAME_HOME", "")
	if got := gameHome(); got != "/hers" {
		t.Errorf("with no GAME_HOME the shell's home stands: %q", got)
	}
	t.Setenv("GAME_HOME", "/an/agent/anchor")
	if got := gameHome(); got != "/an/agent/anchor" {
		t.Errorf("GAME_HOME wins when it is set: %q", got)
	}
}
