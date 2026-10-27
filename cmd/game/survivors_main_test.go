package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
)

// the exit codes the shell sees, and the tries a member gets, as the
// docs give them.
const (
	shellOK     = 0 // defined here: the shell's success
	shellFailed = 1 // defined here: a verb that failed
	shellUsage  = 2 // defined here: a word game cannot start on
	threeTries  = 3 // defined here: the first try and two more
	exitedThree = 3 // defined here: the code a refusing archive exits with
	exitedFive  = 5 // defined here: the code a failing tar exits with
)

// shelled is a process factory whose git and tar are the shell scripts
// given, so an archive and an unpacking can each be made to fail.
type shelled struct{ archive, untar string }

// command is the factory: tar is the unpacking script and anything else
// the archive.
func (scripts shelled) command(name string, _ ...string) *exec.Cmd {
	if name == "tar" {
		return exec.Command("sh", "-c", scripts.untar)
	}
	return exec.Command("sh", "-c", scripts.archive)
}

// noTar is a process factory whose tar is not there and whose archive
// ends well.
type noTar struct{ missing string }

// command is the factory.
func (without noTar) command(name string, _ ...string) *exec.Cmd {
	if name == "tar" {
		return exec.Command(without.missing)
	}
	return exec.Command("true")
}

// failingTar is the script of a tar that says something and exits five.
const failingTar = "cat >/dev/null; echo broken >&2; exit 5"

// TestExportGivesThePipeError: a pipe that cannot be made is the error
// given, not a later tar failure.
func TestExportGivesThePipeError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	here := repoAt(dir)
	err := exportUsing(here, "HEAD", dir, occupiedCommand{}.command)
	if err == nil || !strings.Contains(err.Error(), "Stdout already set") ||
		strings.HasPrefix(err.Error(), "tar:") {
		t.Errorf("an archive with its stdout taken: %v", err)
	}
}

// TestExportGivesTheErrorOfStartingTar: tar that cannot start is the
// error of starting it, and the archive's success does not hide it.
func TestExportGivesTheErrorOfStartingTar(t *testing.T) {
	t.Parallel()
	here := repoAt(t.TempDir())
	missing := filepath.Join(t.TempDir(), "no-tar")
	err := exportUsing(here, "HEAD", t.TempDir(), noTar{missing}.command)
	if !errors.Is(err, fs.ErrNotExist) || strings.HasPrefix(err.Error(), "tar:") {
		t.Errorf("tar that is not there: %v", err)
	}
}

// TestExportGivesTheArchiveItsRefusalBeforeTars: when git archive exits
// with a code, that is the error, whatever tar made of the short input.
func TestExportGivesTheArchiveItsRefusalBeforeTars(t *testing.T) {
	t.Parallel()
	here := repoAt(t.TempDir())
	scripts := shelled{"exit 3", failingTar}
	err := exportUsing(here, "HEAD", t.TempDir(), scripts.command)
	var refused *exec.ExitError
	if !errors.As(err, &refused) || refused.ExitCode() != exitedThree {
		t.Errorf("an archive that exits 3: %v", err)
	}
}

// TestExportGivesTarsErrorWhenTheArchiveIsWhole: the anti-test: an
// archive that ends well and a tar that does not is tar's error, with
// what tar said.
func TestExportGivesTarsErrorWhenTheArchiveIsWhole(t *testing.T) {
	t.Parallel()
	here := repoAt(t.TempDir())
	scripts := shelled{"true", failingTar}
	err := exportUsing(here, "HEAD", t.TempDir(), scripts.command)
	var refused *exec.ExitError
	if !errors.As(err, &refused) || refused.ExitCode() != exitedFive ||
		!strings.HasPrefix(err.Error(), "tar: broken: ") {
		t.Errorf("a tar that exits 5: %v", err)
	}
}

// TestSweepBinGivesTheErrorOfReadingTheBin: a bin that is a file stats
// and cannot be listed, and that error comes back whole.
func TestSweepBinGivesTheErrorOfReadingTheBin(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	bin := filepath.Join(parent, "bin")
	write(t, bin, "not a folder\n")
	gone, err := sweepBin(repoAt(parent), bin)
	if !errors.Is(err, syscall.ENOTDIR) || strings.Contains(err.Error(), "swept") ||
		gone != count.Nothing {
		t.Errorf("a bin that is a file: %d, %#v", gone, err)
	}
}

// TestPlayGivesTheNumbersTheShellSees: exit codes are the numbers, not
// the names of them.
func TestPlayGivesTheNumbersTheShellSees(t *testing.T) {
	if exitOK != shellOK || exitFailed != shellFailed || exitUsage != shellUsage {
		t.Errorf("exit codes %d, %d, %d; want %d, %d, %d", exitOK, exitFailed,
			exitUsage, shellOK, shellFailed, shellUsage)
	}
}

// heldStderr is os.Stderr held in a pipe until it is released. while it
// is held a test does not run beside others.
type heldStderr struct {
	t           *testing.T
	was         *os.File
	read, write *os.File
}

// holdStderr takes os.Stderr into a pipe.
func holdStderr(t *testing.T) *heldStderr {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	held := &heldStderr{t: t, was: os.Stderr, read: read, write: write}
	os.Stderr = write
	return held
}

// release puts os.Stderr back and gives what was written to it.
func (held *heldStderr) release() string {
	held.t.Helper()
	os.Stderr = held.was
	held.write.Close()
	body, err := io.ReadAll(held.read)
	held.read.Close()
	if err != nil {
		held.t.Fatal(err)
	}
	return string(body)
}

// TestPlayWithNoWordSaysUsageAndNothingOfTheConfig: no word stops at
// the usage, before any config is read.
func TestPlayWithNoWordSaysUsageAndNothingOfTheConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".game"), "a line that is no directive\n")
	if _, err := config.Load(t.TempDir(), t.TempDir(), dir); err == nil {
		t.Fatal("the fixture's .game reads; it should not")
	}
	held := holdStderr(t)
	code := play(nil, dir, t.TempDir(), t.TempDir())
	said := held.release()
	if code != shellUsage || !strings.HasPrefix(said, "game check") ||
		strings.Contains(said, "game:") {
		t.Errorf("no word: %d, %q", code, said)
	}
}

// resolvedCase is a step's program and what resolved makes of it.
type resolvedCase struct{ step, want string }

// TestResolvedJoinsAProgramWithASlashToItsDirectory: a relative program
// with a slash is the directory's, and one without is left to PATH.
func TestResolvedJoinsAProgramWithASlashToItsDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, each := range []resolvedCase{
		{"run r :: bin/run one", filepath.Join(root, "bin", "run")},
		{"run s :: tool two", "tool"},
		{"run a :: /abs/tool", "/abs/tool"},
	} {
		run := gamed(t, each.step).Runs()[count.First]
		dir, prog, _ := resolved(root, run.Steps()[count.First])
		if prog != each.want || dir != root {
			t.Errorf("%q: %q in %q, want %q in %q", each.step, prog, dir,
				each.want, root)
		}
	}
}

// paintRecord is what a paint function was asked to show.
type paintRecord struct{ shown []string }

// paint notes the directory and name it was asked for.
func (record *paintRecord) paint(dir, name string) bool {
	record.shown = append(record.shown, dir+" "+name)
	return true
}

// TestPaintOkShowsOnlyAMemberThatBuilt: one that did not build shows
// nothing.
func TestPaintOkShowsOnlyAMemberThatBuilt(t *testing.T) {
	t.Parallel()
	for result, shown := range map[string]bool{"ok": true, "failed": false, "flaky": false} {
		var record paintRecord
		paintOk("/work", "m", result, record.paint)
		if shown != (len(record.shown) == count.One) ||
			shown && record.shown[count.First] != "/work/m m" {
			t.Errorf("%s painted %q", result, record.shown)
		}
	}
}

// noisyChild is a member's child that notes each try in a file and fails
// with the event it is given.
type noisyChild struct{ tries, event string }

// child is the child as buildStack asks for one.
func (noisy noisyChild) child(string) *exec.Cmd {
	return exec.Command("sh", "-c", "echo x >> '"+noisy.tries+"'; echo '"+
		noisy.event+"'; exit 1")
}

// triesOf is how many times the child noted a try.
func triesOf(path string) int {
	body, _ := os.ReadFile(path)
	return strings.Count(string(body), "x")
}

// TestBuildStackTriesAFailureOnTheNetworkThreeTimes: a member whose
// child keeps failing on the network is tried three times and no more.
// the waits are the real ones, up to a second and then two.
func TestBuildStackTriesAFailureOnTheNetworkThreeTimes(t *testing.T) {
	t.Parallel()
	dir, tries := t.TempDir(), filepath.Join(t.TempDir(), "tries")
	write(t, filepath.Join(dir, "a", ".game"), "name a\n")
	noisy := noisyChild{tries, `{"step":"check","state":"fail","kind":"transient"}`}
	err := buildStack(repoAt(dir), gamed(t, "stack a main"),
		[]string{"--here"}, io.Discard, noisy.child)
	if err == nil {
		t.Fatal("a member that always fails built clean")
	}
	if got := triesOf(tries); got != threeTries {
		t.Errorf("tries %d; want %d", got, threeTries)
	}
}

// TestBuildStackDoesNotRetryAFailureThatIsNotTheNetworks: the
// anti-test: a failure of the member's own is one try.
func TestBuildStackDoesNotRetryAFailureThatIsNotTheNetworks(t *testing.T) {
	t.Parallel()
	dir, tries := t.TempDir(), filepath.Join(t.TempDir(), "tries")
	write(t, filepath.Join(dir, "a", ".game"), "name a\n")
	noisy := noisyChild{tries, `{"step":"check","state":"fail"}`}
	_ = buildStack(repoAt(dir), gamed(t, "stack a main"),
		[]string{"--here"}, io.Discard, noisy.child)
	if got := triesOf(tries); got != count.One {
		t.Errorf("tries %d; want %d", got, count.One)
	}
}
