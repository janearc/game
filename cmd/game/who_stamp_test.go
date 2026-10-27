package main

import (
	"path/filepath"
	"testing"
)

// a build with a session id says the session's line, as the commit hook
// makes it from git's user.name and the id's first field, and the
// machine's short name. with no session id in the environment it says no
// session. an id or a host not of the right shape, a quote in it say,
// cannot reach the stamp.
func TestBuilderIsTheSessionAndItsMachine(t *testing.T) {
	t.Parallel()
	work := commands(t)
	gitAt(t, work.Dir(), "config", "user.name", "ada-mae king")
	for _, each := range []struct{ sid, host, want string }{
		{"4b732342-1f2e-4d3c", "loft.local", "am/4b732342 on loft"},
		{"4b732342", "loft", "am/4b732342 on loft"},
		{"", "loft.local", "no session on loft"},
		{"", "", "no session on an unnamed machine"},
		{"4b732342", "", "am/4b732342 on an unnamed machine"},
		{"4b73'2342", "loft", "no session on loft"},
		{"4b732342-and-the-rest", "loft", "no session on loft"},
		{"4B732342", "loft", "no session on loft"},
		{"4b732342", "lo'ft", "am/4b732342 on an unnamed machine"},
		{"4b732342", "-loft", "am/4b732342 on an unnamed machine"},
	} {
		if got := builder(work, each.sid, each.host); got != each.want {
			t.Errorf("%q on %q: %q, want %q", each.sid, each.host, got,
				each.want)
		}
	}
	gitAt(t, work.Dir(), "config", "user.name", "")
	if got := builder(work, "4b732342", "loft"); got != "4b732342 on loft" {
		t.Errorf("no user.name: %q", got)
	}
}

// initials are a letter for each part of the given names, never the
// family name, as the commit hook makes them.
func TestInitialsAreTheGivenNames(t *testing.T) {
	t.Parallel()
	for full, want := range map[string]string{
		"ada-mae king":  "am",
		"Ada Mae King":  "am",
		"o'brien smith": "ob",
		"ada":           "a",
		"Émile zola":    "é",
		"":              "",
		"  stack  ":     "s",
	} {
		if got := initials(full); got != want {
			t.Errorf("%q: %q, want %q", full, got, want)
		}
	}
}

// the sessions behind a build are the last three different session lines
// that end a commit message, newest first; a line anywhere but the end,
// or none, does not count. these are what the messages say, and no
// signature is read.
func TestSessionsAreTheLastThreeLines(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "x")
	work := gitTree(t, dir, map[string]string{"a": "a\n"})
	if got := sessions(work); got != "" {
		t.Errorf("no message ends in a session line: %q", got)
	}
	for _, message := range []string{
		"oldest\n\njm/eeeeeeee",
		"one\n\njm/aaaaaaaa",
		"two\n\np/bbbbbbbb",
		"three, with no session line",
		"four\n\njm/aaaaaaaa",
		"five\n\njm/ffffffff\nand a line after it",
		"six\n\njm/cccccccc  ",
	} {
		gitAt(t, work.Dir(), "commit", "-q", "--allow-empty", "-m", message)
	}
	want := "jm/cccccccc,jm/aaaaaaaa,p/bbbbbbbb"
	if got := sessions(work); got != want {
		t.Errorf("sessions %q, want %q", got, want)
	}
}
