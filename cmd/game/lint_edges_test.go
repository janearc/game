package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
)

// linted is a repository holding a go file with one wide comment, to
// fix, and two wide lines of code, to look at, under a width of 30.
func linted(t *testing.T) (string, config.Config) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "x")
	work := gitTree(t, dir, map[string]string{
		"a.go": "package a\n\n// a comment that is wider than thirty " +
			"columns\nvar Wide = \"a line of code wider than thirty\"\n" +
			"var Wider = \"another line of code wider than thirty\"\n",
	})
	return work.Dir(), gamed(t, "lint width 30")
}

// lint lists each finding, tallies those to look at by rule, and is
// the verdict; --count leaves the list out.
func TestLintListsTalliesAndJudges(t *testing.T) {
	t.Parallel()
	dir, cfg := linted(t)
	for _, each := range []struct {
		args   []string
		listed bool
	}{{nil, true}, {[]string{"--count"}, false}} {
		var out, errs bytes.Buffer
		err := lintTo(repoAt(dir), cfg,
			each.args, &out, &errs)
		if err == nil || err.Error() != "lint: 1 to fix, 2 to look at" {
			t.Errorf("%v: %v", each.args, err)
		}
		said := out.String()
		if strings.Contains(said, "lint: a.go:3: width") != each.listed ||
			strings.Contains(said, "look: a.go:4: width") != each.listed {
			t.Errorf("%v: listed %v:\n%s", each.args, each.listed, said)
		}
		if !strings.HasSuffix(said, "look: 2 width\n") ||
			strings.Contains(said, "lint: clean") {
			t.Errorf("%v: said:\n%s", each.args, said)
		}
	}
}

// with nothing to find, lint says so.
func TestLintSaysClean(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "x")
	work := gitTree(t, dir, map[string]string{"a.go": "package a\n"})
	cfg := gamed(t, "lint width 30")
	var out bytes.Buffer
	if err := lintTo(work, cfg, nil, &out, &bytes.Buffer{}); err != nil ||
		out.String() != "lint: clean\n" {
		t.Errorf("%v, said %q", err, out.String())
	}
}

// with no rules lint asks for them and passes; rules it cannot read and
// a tree it cannot walk are the error.
func TestLintSaysWhatItCannotDo(t *testing.T) {
	t.Parallel()
	var out, errs bytes.Buffer
	if err := lintTo(cleaned(t, "x", nil), blankConfig(), nil, &out,
		&errs); err != nil || count.Positive(out.Len()) ||
		!strings.HasPrefix(errs.String(), "lint: hey buddy") {
		t.Errorf("no rules: %v, said %q, %q", err, out.String(),
			errs.String())
	}
	bad := gamed(t, "lint width wide")
	if err := lintTo(cleaned(t, "x", nil), bad, nil, &bytes.Buffer{},
		&bytes.Buffer{}); err == nil {
		t.Error("a width that is no number was taken")
	}
	_, cfg := linted(t)
	missing := filepath.Join(cleaned(t, "x", nil).Dir(), "nowhere")
	gone := repoAt(missing)
	if err := lintTo(gone, cfg, nil, &bytes.Buffer{},
		&bytes.Buffer{}); err == nil {
		t.Error("a tree that is not there was linted")
	}
}
