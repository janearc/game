package config

import "testing"

// TestWhose: an agent has an anchor and wants its own roads and dists;
// a person in a shell wants theirs. faking HOME would work and would move
// ssh keys and gh credentials with it, so the override is its own name.
func TestWhose(t *testing.T) {
	t.Setenv("HOME", "/hers")
	t.Setenv("GAME_HOME", "")
	if got := Whose(); got != "/hers" {
		t.Errorf("with no GAME_HOME the shell's home stands: %q", got)
	}
	t.Setenv("GAME_HOME", "/an/agent/anchor")
	if got := Whose(); got != "/an/agent/anchor" {
		t.Errorf("GAME_HOME wins when it is set: %q", got)
	}
}
