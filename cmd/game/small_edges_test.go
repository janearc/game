package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/lint"
)

// a failed command's error is what it said on stderr, or on stdout when
// it said nothing there.
func TestRunSaysWhatTheCommandSaid(t *testing.T) {
	t.Parallel()
	for script, want := range map[string]string{
		"echo out-words; echo err-words >&2; exit 1": "err-words",
		"echo out-words; exit 1":                     "out-words",
	} {
		_, err := run(t.TempDir(), "sh", "-c", script)
		if err == nil || !strings.HasSuffix(err.Error(), ": "+want) {
			t.Errorf("%q: %v, want it to end %q", script, err, want)
		}
	}
}

// Git reads some roads as another machine. Those are not looked for on
// disk. Paths with a colon or an at in them alone are on this machine
// and are looked for.
func TestThereLooksOnlyForPathsOnThisMachine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for dist, elsewhere := range map[string]bool{
		"https://example.invalid/x.git": true,
		"git@example.invalid:x.git":     true,
		"/dists/x.git":                  false,
		"dists/at:noon.git":             false,
		"dists/me@example.invalid":      false,
		"example.invalid:x.git":         true,
		"ftp://example.invalid/x.git":   true,
	} {
		err := there(dist, dir, "x", "config")
		if (err == nil) != elsewhere {
			t.Errorf("%q: looked for on disk %v, want %v", dist,
				err != nil, !elsewhere)
		}
	}
}

// a block with no road has no road for it, said so.
func TestRoadForABlockWithNone(t *testing.T) {
	t.Parallel()
	_, err := roadFor(cleaned(t, "x", nil), cfgFor(t, "ada", "", "d.git"))
	if err == nil || !strings.HasPrefix(err.Error(), "no road for x;") {
		t.Errorf("%v", err)
	}
}

// a member's allow lines move under its directory, one word or more;
// one with no words, and every other rule, is kept as it is.
func TestPrefixedMovesAllowsUnderTheMember(t *testing.T) {
	t.Parallel()
	got := prefixed("m", []config.Rule{
		config.Ruled("allow", "a.go"),
		config.Ruled("allow", "b.go", "width"),
		config.Ruled("allow"),
		config.Ruled("width", "80"),
	})
	want := []config.Rule{
		config.Ruled("allow", "m/a.go"),
		config.Ruled("allow", "m/b.go", "width"),
		config.Ruled("allow"),
		config.Ruled("width", "80"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v", got)
	}
}

// a member's .game that does not parse adds none of its rules, not
// even those before the line it stopped at.
func TestMemberRulesLeaveOutAGameThatDoesNotParse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wide := strings.Repeat("x", countHeld(t, "past-width"))
	write(t, filepath.Join(dir, "a.md"), wide+"\n")
	for game, want := range map[string]int{
		"lint width 40\n":                        count.One,
		"lint width 40\nnot a line game reads\n": count.Nothing,
	} {
		rules, err := memberRules(blankConfig(),
			map[string]string{"m": game})
		if err != nil {
			t.Fatal(err)
		}
		found, err := rules.Run(dir)
		if err != nil || len(found) != want {
			t.Errorf("%q: found %v, want %d (%v)", game, found, want, err)
		}
	}
}

// a commit is linted where it is exported, and where there is nowhere
// to export it, that is the error.
func TestLintExportedNeedsSomewhereToExport(t *testing.T) {
	wide := strings.Repeat("x", countHeld(t, "past-width"))
	work := freshTree(t, "x", map[string]string{"a.md": wide + "\n"})
	forty := gamed(t, "lint width 40")
	rules, err := lint.Describe(forty.Lint())
	if err != nil {
		t.Fatal(err)
	}
	found, err := lintExported(work, "HEAD", rules)
	if err != nil || len(found) != count.One {
		t.Errorf("found %v, %v", found, err)
	}
	nowhere := filepath.Join(t.TempDir(), "nowhere")
	t.Setenv("TMPDIR", nowhere)
	if _, err := lintExported(work, "HEAD", rules); err == nil {
		t.Error("exported with nowhere to export to")
	}
}

// export with no tar to unpack it is the error.
func TestExportNeedsTar(t *testing.T) {
	work := freshTree(t, "x", map[string]string{"a": "a\n"})
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if err := export(work, "HEAD", t.TempDir(), exec.Command); err == nil {
		t.Error("exported with no tar")
	}
}

// verify stops at a test target that fails, a build that fails, and lint
// rules it cannot read or run, telling that step failed.
func TestVerifyStopsWhereAStepFails(t *testing.T) {
	t.Parallel()
	failing := failingTest(t)
	bad := gamed(t, "lint width wide")
	comments := gamed(t, "lint comments")
	for name, each := range map[string]struct {
		files map[string]string
		cfg   config.Config
		last  string
	}{
		"a test target": {nil, failing, "test fail"},
		"the build": {map[string]string{"cmd/a/main.go": "package main\n"},
			blankConfig(), "build fail"},
		"lint it cannot read": {nil, bad, "lint fail"},
		"lint it cannot run": {map[string]string{"a.go": "package"},
			comments, "lint fail"},
	} {
		dir := t.TempDir()
		for file, body := range each.files {
			write(t, filepath.Join(dir, file), body)
		}
		var out bytes.Buffer
		err := verify(repoAt(dir), each.cfg, &out)
		got := events(out.String())
		if count.None(len(got)) {
			t.Errorf("%s: %v, no events", name, err)
			continue
		}
		last := got[count.Last(len(got))]
		if err == nil || !strings.HasPrefix(last, each.last) {
			t.Errorf("%s: %v, events %v", name, err, got)
		}
	}
}

// Failures of tar carry its own words after its name. The error is not
// the bare exit status. Directories that are not there are named.
func TestExportSaysWhatTarSaid(t *testing.T) {
	t.Parallel()
	work := freshTree(t, "x", map[string]string{"a": "a\n"})
	nowhere := filepath.Join(t.TempDir(), "nowhere")
	err := export(work, "HEAD", nowhere, exec.Command)
	if err == nil || !strings.HasPrefix(err.Error(), "tar: ") ||
		!strings.Contains(err.Error(), nowhere) {
		t.Errorf("export to a directory that is not there: %v", err)
	}
}

// when git cannot archive the commit, the error is git's own exit and
// not tar's complaint about the empty archive that came after it.
func TestExportSaysWhatGitRefused(t *testing.T) {
	t.Parallel()
	work := freshTree(t, "x", map[string]string{"a": "a\n"})
	err := export(work, "no-such-commit", t.TempDir(), exec.Command)
	if err == nil || strings.HasPrefix(err.Error(), "tar: ") {
		t.Errorf("a commit that is not there: %v, want git's refusal", err)
	}
}
