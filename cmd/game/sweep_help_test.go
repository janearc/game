package main

import (
	"bytes"
	"strings"
	"testing"
)

// sweepRun runs game through pzm in dir and gives the exit code and what
// it said on each stream.
func sweepRun(t *testing.T, args ...string) (code int, out, errs string) {
	t.Helper()
	var said, complained bytes.Buffer
	code = runPzm(args, t.TempDir(), t.TempDir(), t.TempDir(),
		strings.NewReader(""), &said, &complained)
	return code, said.String(), complained.String()
}

// sweep --help and -h print sweep's own help, exit 0, and never reach git.
func TestSweepHelpIsSweepsOwn(t *testing.T) {
	t.Parallel()
	for _, ask := range []string{"--help", "-h"} {
		code, out, errs := sweepRun(t, "sweep", ask)
		if code != exitOK || errs != "" ||
			!strings.Contains(out, "game sweep") ||
			!strings.Contains(out, "checks a tree for what must not ship") {
			t.Errorf("sweep %s: exit %d, said %q, complained %q", ask, code,
				out, errs)
		}
	}
}

// anti-test: a revision that begins with a hyphen is not handed to git
// log, where it would be read as an option. after the lone -- pzm leaves
// it as a word, and sweep refuses it, naming where its help is.
func TestSweepRefusesARevisionThatLooksLikeAFlag(t *testing.T) {
	t.Parallel()
	for _, rev := range []string{"--help", "-n", "--format=%B"} {
		code, out, errs := sweepRun(t, "sweep", "--", rev)
		if code == exitOK || out != "" ||
			!strings.Contains(errs, "begins with a hyphen") ||
			!strings.Contains(errs, "game sweep --help") ||
			strings.Contains(errs, "git log") {
			t.Errorf("sweep -- %s: exit %d, said %q, complained %q", rev,
				code, out, errs)
		}
	}
}
