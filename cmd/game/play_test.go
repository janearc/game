package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
)

// TestPlayGivesEachEndItsCode: no word and an unknown word are 2, a
// verb's --help is 0, and a verb's refusal is 1.
func TestPlayGivesEachEndItsCode(t *testing.T) {
	t.Parallel()
	dir, whose, home := t.TempDir(), t.TempDir(), t.TempDir()
	for _, words := range []struct {
		args []string
		want int
	}{
		{nil, exitUsage},
		{[]string{"no-such-verb"}, exitUsage},
		{[]string{"stick", "--help"}, exitOK},
		{[]string{"stick", "a-word"}, exitFailed},
		{[]string{"version"}, exitOK},
	} {
		if got := play(words.args, dir, whose, home); got != words.want {
			t.Errorf("game %q: exit %d, want %d", words.args, got,
				words.want)
		}
	}
}

// TestSettleRefusesBuildAndRunWithoutTheirSettings: build and run each
// say what is missing and refuse; any other verb goes ahead.
func TestSettleRefusesBuildAndRunWithoutTheirSettings(t *testing.T) {
	t.Parallel()
	cfg := gamed(t, "needs TOKEN a test")
	for word, refused := range map[string]bool{
		"build": true, "run": true, "lint": false,
	} {
		var said bytes.Buffer
		err := settle(cfg, word, &said)
		if refused != (err != nil) {
			t.Errorf("%s: %v, refused should be %v", word, err, refused)
		}
		if named := strings.Contains(said.String(),
			"TOKEN is needed: a test"); named != refused {
			t.Errorf("%s said %q", word, said.String())
		}
	}
}

// TestCatchUpRefusesADivergedBranch: a road ahead of a branch that has a
// commit of its own cannot be caught up to, and that is an error, not a
// tip to print.
func TestCatchUpRefusesADivergedBranch(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	commitFile(t, two, "theirs.txt", "theirs\n", "theirs")
	if _, err := two.Git("push", road, "work"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, one, "mine.txt", "mine\n", "mine")
	if err := catchUp(one, road, io.Discard); err == nil ||
		!strings.Contains(err.Error(), "diverged") {
		t.Errorf("a diverged branch: %v, want the diverged refusal", err)
	}
}

// TestShowEndsArtWithOneNewline: art that ends its own line is written
// as it is; art that does not is given one newline; nothing is nothing.
func TestShowEndsArtWithOneNewline(t *testing.T) {
	t.Parallel()
	for art, want := range map[string]string{
		"x\n": "x\n", "x": "x\n", "": "", "\n": "\n",
	} {
		var out bytes.Buffer
		show(&out, []byte(art))
		if out.String() != want {
			t.Errorf("%q shown as %q, want %q", art, out.String(), want)
		}
	}
}

// TestArtForPrefersTheProjectsOwn: a project's own art before the
// header, the header for any other, and none from a directory with no
// art.
func TestArtForPrefersTheProjectsOwn(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"p.ansi": "own", "header.ansi": "header",
	} {
		write(t, filepath.Join(dir, "art", name), body)
	}
	for project, want := range map[string]string{"p": "own", "q": "header"} {
		if art, found := artFor(dir, project); !found || string(art) != want {
			t.Errorf("%s: %q, %v, want %q", project, art, found, want)
		}
	}
	if art, found := artFor(t.TempDir(), "p"); found || art != nil {
		t.Errorf("no art: %q, %v", art, found)
	}
}

// TestPlayRefusesWhatItCannotStartOn: a config that cannot be read, and
// a build missing a setting the repository needs, are each 2.
func TestPlayRefusesWhatItCannotStartOn(t *testing.T) {
	t.Parallel()
	unreadable := t.TempDir()
	write(t, filepath.Join(unreadable, ".config", "game", "config", "x"),
		"a directory where the config goes")
	if got := play([]string{"version"}, t.TempDir(), unreadable,
		t.TempDir()); got != exitUsage {
		t.Errorf("an unreadable config: exit %d, want %d", got, exitUsage)
	}
	needy := t.TempDir()
	write(t, filepath.Join(needy, ".game"), "needs TOKEN a test\n")
	if got := play([]string{"build"}, needy, t.TempDir(),
		t.TempDir()); got != exitUsage {
		t.Errorf("a build missing TOKEN: exit %d, want %d", got, exitUsage)
	}
}

// TestSettleLetsABuildWithNothingMissingGo: with every setting there, or
// none needed, a build goes ahead and says nothing.
func TestSettleLetsABuildWithNothingMissingGo(t *testing.T) {
	t.Parallel()
	var said bytes.Buffer
	if err := settle(blankConfig(), "build", &said); err != nil ||
		count.Positive(said.Len()) {
		t.Errorf("nothing needed: %v, said %q", err, said.String())
	}
}

// TestCatchUpSaysWhereItCaughtUpTo: behind the road, the branch is
// fast-forwarded and the tip named by its first seven; with no commit
// at all there is no branch, and that is an error.
func TestCatchUpSaysWhereItCaughtUpTo(t *testing.T) {
	t.Parallel()
	one, two, road, _ := stickyFixture(t)
	commitFile(t, two, "theirs.txt", "theirs\n", "theirs")
	if _, err := two.Git("push", road, "work"); err != nil {
		t.Fatal(err)
	}
	tip, err := two.Git("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var said bytes.Buffer
	if err := catchUp(one, road, &said); err != nil ||
		said.String() != "stick: caught up to the road at "+repo.Short(tip)+
			" first\n" {
		t.Errorf("behind the road: %v, said %q", err, said.String())
	}
	fresh := repoAt(t.TempDir())
	if _, err := fresh.Git("init", "-q", "-b", "work"); err != nil {
		t.Fatal(err)
	}
	if err := catchUp(fresh, road, io.Discard); err == nil {
		t.Error("a tree with no commit was caught up")
	}
}

// TestColourIsForATerminalOnly: art goes to a character device unless
// NO_COLOR is set, and never to a plain file or past an error.
func TestColourIsForATerminalOnly(t *testing.T) {
	t.Parallel()
	device, err := os.Stat(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := os.Stat("play_test.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []struct {
		name    string
		noColor string
		info    os.FileInfo
		err     error
		want    bool
	}{
		{"a terminal", "", device, nil, true},
		{"NO_COLOR", "1", device, nil, false},
		{"a file", "", plain, nil, false},
		{"an error", "", device, os.ErrClosed, false},
	} {
		if got := colour(out.noColor, out.info, out.err); got != out.want {
			t.Errorf("%s: %v, want %v", out.name, got, out.want)
		}
	}
}

// TestPaintedPaintsNothingWhereNoTerminalIs: a test's stdout is a pipe,
// so art that is there is not painted, and paintTo, given a writer,
// writes it.
func TestPaintedPaintsNothingWhereNoTerminalIs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "art", "header.ansi"), "art\n")
	if painted(dir, "p") {
		t.Error("art was painted to a pipe")
	}
	var out bytes.Buffer
	if !paintTo(&out, dir, "p") || out.String() != "art\n" {
		t.Errorf("paintTo wrote %q", out.String())
	}
	if paintTo(&out, t.TempDir(), "p") {
		t.Error("paintTo found art where there was none")
	}
}

// TestMainRunsTheWordsAfterItsName: this binary, run as game, answers
// version with 0 and nothing with 2, so main hands play the words after
// its own name.
func TestMainRunsTheWordsAfterItsName(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	for _, words := range []struct {
		args []string
		want int
	}{
		{[]string{"version"}, exitOK},
		{nil, exitUsage},
	} {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := exec.Command(self, words.args...)
		child.Dir = t.TempDir()
		child.Env = append(os.Environ(), asMain+"=1", "GAME_HOME="+home,
			"HOME="+home)
		out, err := child.Output()
		code := exitOK
		if exited, ok := err.(*exec.ExitError); ok {
			code = exited.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != words.want {
			t.Errorf("game %q: exit %d, want %d", words.args, code,
				words.want)
		}
		if words.want == exitOK && !regexp.MustCompile(`^game`).Match(out) {
			t.Errorf("game version said %q", out)
		}
	}
}
