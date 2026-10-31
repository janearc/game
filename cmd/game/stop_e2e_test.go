package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
)

// the waits and sizes of the stop test, defined here.
const (
	notedRuns   = 3                      // defined here: the suite, one mutant, and the mutant that waits
	stopLooks   = 600                    // defined here: how many looks for the note, at stopEvery
	stopEvery   = 100 * time.Millisecond // defined here: a look
	stopGrace   = 60 * time.Second       // defined here: how long a stopped run has to end
	goneLooks   = 50                     // defined here: looks at stopEvery, five seconds, for a killed process to be gone
	lowestGroup = 2                      // defined here: pid 1 is the system's, and group 0 is this test's own
	cacheMode   = 0o755                  // defined here: a folder the owner writes and all enter
	exitNever   = -1                     // defined here: the exit code of a run that did not end
)

// removal removes a folder when a test is over.
type removal struct {
	path string
}

// remove removes the folder and what is in it.
func (gone removal) remove() {
	os.RemoveAll(gone.path)
}

// stopPlace is the cache folder the stop test makes its run's copies in: not
// under the system's per-user temp folder, which Xcode does not measure.
func stopPlace(t *testing.T) string {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cache, cacheMode); err != nil {
		t.Fatal(err)
	}
	place, err := os.MkdirTemp(cache, "game-stop-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(removal{path: place}.remove)
	return place
}

// stoppable is a run of game mutants over the stoptiny module. it is
// started as a child. tests stop it with a signal while a mutant is under
// test.
type stoppable struct {
	t      *testing.T
	cmd    *exec.Cmd
	out    *bytes.Buffer
	note   string
	report string
}

// end ends the run's process and its group when the test is over. it also
// ends the test binaries its fixture noted. whatever the test found
// nothing it started is left. pids below lowestGroup lead no group of the
// test's. nothing is signalled to them.
func (run stoppable) end() {
	pid := run.cmd.Process.Pid
	if pid >= lowestGroup {
		syscall.Kill(-pid, syscall.SIGKILL)
		run.cmd.Wait()
	}
	endNoted(run.note)
}

// endNoted ends the test binaries the fixture's tests noted if any is
// still there. the run is meant to have ended them. this is for the day
// it has not. pids are signalled only when they are running a stoptiny
// test. numbers noted long ago may be other processes' now.
func endNoted(note string) {
	body, _ := os.ReadFile(note)
	for _, line := range strings.Split(string(body), "\n") {
		_, field, found := strings.Cut(line, "ran ")
		pid, err := strconv.Atoi(field)
		if !found || err != nil || pid < lowestGroup {
			continue
		}
		running, _ := exec.Command("ps", "-o", "command=", "-p", field).Output()
		if strings.Contains(string(running), "stoptiny.test") {
			syscall.Kill(-pid, syscall.SIGKILL)
			syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

// startRun starts game mutants as a child over a copy of the stoptiny
// module with one worker. HOME and TMPDIR are as given. the child leads a
// group of its own. the test ends it when it is done.
func startRun(t *testing.T, home, tmpdir string) stoppable {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	module := t.TempDir()
	if err := os.CopyFS(module, os.DirFS("testdata/stoptiny")); err != nil {
		t.Fatal(err)
	}
	run := stoppable{t: t, out: &bytes.Buffer{},
		note:   filepath.Join(t.TempDir(), "note"),
		report: filepath.Join(t.TempDir(), "report")}
	run.cmd = exec.Command(self, "mutants", "--workers", "1", "--timeout",
		"60s", "--report", run.report)
	run.cmd.Dir = module
	run.cmd.Env = append(os.Environ(), asMain+"=1", "GAME_HOME="+t.TempDir(),
		"HOME="+home, "TMPDIR="+tmpdir, "STOPTINY_NOTE="+run.note)
	run.cmd.Stdout, run.cmd.Stderr = run.out, run.out
	run.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := run.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(run.end)
	return run
}

// waitForNote waits until the fixture's tests have run that many times.
func (run stoppable) waitForNote(runs int) {
	run.t.Helper()
	for range stopLooks {
		body, _ := os.ReadFile(run.note)
		if strings.Count(string(body), "\n") >= runs {
			return
		}
		time.Sleep(stopEvery)
	}
	run.t.Fatalf("the tests never ran %d times", runs)
}

// waiting waits for a child to end and says how it did.
type waiting struct {
	cmd  *exec.Cmd
	ends chan error
}

// wait waits for the child and sends how it ended.
func (wait waiting) wait() {
	wait.ends <- wait.cmd.Wait()
}

// stop sends the run SIGTERM, waits for it to end and gives its exit code.
// it fails when the run does not end in time.
func (run stoppable) stop() int {
	run.t.Helper()
	if err := run.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		run.t.Fatal(err)
	}
	wait := waiting{cmd: run.cmd, ends: make(chan error, count.One)}
	go wait.wait()
	select {
	case err := <-wait.ends:
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return exited.ExitCode()
		}
		if err != nil {
			run.t.Fatal(err)
		}
		return count.Nothing
	case <-time.After(stopGrace):
		run.t.Fatal("the stopped run never ended")
		return exitNever
	}
}

// startedAndStopped is a run that was started and stopped with a signal
// while a mutant was under test, and its exit code.
func startedAndStopped(t *testing.T, home, tmpdir string) (stoppable, int) {
	t.Helper()
	run := startRun(t, home, tmpdir)
	run.waitForNote(notedRuns)
	return run, run.stop()
}

// sent SIGTERM while a mutant is under test the run stops. it writes the
// report of every mutant finished. the report says the run is partial and
// how many were not reached. the usual tally is told. the exit is nonzero.
func TestASignalledRunWritesAPartialReportAndExitsNonzero(t *testing.T) {
	t.Parallel()
	run, code := startedAndStopped(t, t.TempDir(), stopPlace(t))
	told := run.out.String()
	if code == count.Nothing {
		t.Errorf("a stopped run exited 0:\n%s", told)
	}
	written, err := os.ReadFile(run.report)
	if err != nil {
		t.Fatalf("a stopped run wrote no report: %v\n%s", err, told)
	}
	text := strings.TrimSpace(string(written))
	first, _, _ := strings.Cut(text, "\n")
	if !strings.HasPrefix(first, "partial run: ") ||
		!strings.Contains(first, " mutants not reached") {
		t.Errorf("the report does not say it is partial:\n%s", text)
	}
	if !strings.Contains(text, "\nkilled ") ||
		!strings.Contains(text, "\nstopped ") {
		t.Errorf("the report lacks the mutant finished or the one stopped:\n%s",
			text)
	}
	if !strings.Contains(told, "mutants: 1 run; killed 1") ||
		!strings.Contains(told, "stopped with ") {
		t.Errorf("the tally:\n%s", told)
	}
}

// TestAKilledRunHasLeftEveryMutantItFinished: a run ended by SIGKILL,
// as the out-of-memory killer ends one, gets no chance to settle. Its
// report still holds the running line and the mutant it finished, so
// the night is not lost with it.
func TestAKilledRunHasLeftEveryMutantItFinished(t *testing.T) {
	t.Parallel()
	run := startRun(t, t.TempDir(), stopPlace(t))
	run.waitForNote(notedRuns)
	run.waitForReport("\nkilled ")
	pid := run.cmd.Process.Pid
	if pid < lowestGroup {
		t.Fatalf("the run's pid %d leads no group of its own", pid)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	run.cmd.Wait()
	written, err := os.ReadFile(run.report)
	text := string(written)
	if err != nil || !strings.HasPrefix(text, runningLine+"\n") ||
		!strings.Contains(text, "\nkilled ") ||
		strings.Contains(text, "partial run: ") {
		t.Errorf("a killed run left %q: %v\n%s", text, err, run.out)
	}
}

// waitForReport waits until the run's report holds what was asked.
func (run stoppable) waitForReport(holds string) {
	run.t.Helper()
	for range stopLooks {
		body, _ := os.ReadFile(run.report)
		if strings.Contains(string(body), holds) {
			return
		}
		time.Sleep(stopEvery)
	}
	run.t.Fatalf("the report never held %q", holds)
}

// notedPids is the pids of the fixture's test binaries, as its tests
// noted them.
func notedPids(t *testing.T, note string) []int {
	t.Helper()
	body, err := os.ReadFile(note)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(body))
	var pids []int
	for _, line := range strings.Split(text, "\n") {
		_, field, _ := strings.Cut(line, "ran ")
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatalf("a line of the note: %q", line)
		}
		pids = append(pids, pid)
	}
	return pids
}

// stillThere is whether a process by that pid can be signalled.
func stillThere(pid int) bool {
	return syscall.Kill(pid, count.Nothing) == nil
}

// goneWithin waits for a killed process to be gone, and says whether it was.
func goneWithin(pid int) bool {
	for range goneLooks {
		if !stillThere(pid) {
			return true
		}
		time.Sleep(stopEvery)
	}
	return false
}

// stopped runs leave nothing running. the test binary that was under
// test when the signal came is gone when the run has ended. go test
// alone being stopped would have left it to wait out its pause.
func TestASignalledRunLeavesNothingRunning(t *testing.T) {
	t.Parallel()
	run, _ := startedAndStopped(t, t.TempDir(), stopPlace(t))
	for _, pid := range notedPids(t, run.note) {
		if !goneWithin(pid) {
			t.Errorf("pid %d, a test binary of the stopped run, is still running",
				pid)
		}
	}
}

// folderNames is the names of what a folder holds.
func folderNames(t *testing.T, place string) []string {
	t.Helper()
	left, err := os.ReadDir(place)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, each := range left {
		names = append(names, each.Name())
	}
	return names
}

// stopped runs remove their copies like finished ones do. the folder the
// run was told to use as its temp folder holds nothing when the run has
// ended. neither the copies of the tree nor what go left when it was
// killed are there.
func TestASignalledRunLeavesTheTempFolderEmpty(t *testing.T) {
	t.Parallel()
	place := stopPlace(t)
	run := startRun(t, t.TempDir(), place)
	run.waitForNote(notedRuns)
	// the copies are there while the mutant is under test
	if during := folderNames(t, place); len(during) == count.Nothing {
		t.Fatalf("no copies were made in %s:\n%s", place, run.out)
	}
	run.stop()
	if left := folderNames(t, place); len(left) != count.Nothing {
		t.Errorf("a stopped run left %v in %s", left, place)
	}
}

// gathering is a walk of a folder for the folders named game-mutants-,
// which are the copies a run leaves while it works.
type gathering struct {
	found []string
}

// visit notes a copy and does not go into it.
func (gather *gathering) visit(path string, entry os.DirEntry,
	err error) error {
	if err == nil && entry.IsDir() &&
		strings.HasPrefix(entry.Name(), "game-mutants-") {
		gather.found = append(gather.found, path)
		return filepath.SkipDir
	}
	return nil
}

// copiesIn is the folders named game-mutants- under root.
func copiesIn(root string) []string {
	gather := &gathering{}
	filepath.WalkDir(root, gather.visit)
	return gather.found
}

// when TMPDIR is under the system's temp folders the run makes its copies
// in the user's cache folder under game/mutants. every login has such a
// TMPDIR unless it is changed. Xcode does not measure sources under
// /var/folders. the run removes the copies when it is stopped. the
// system's folder is not made.
func TestASignalledRunUnderTheSystemTempFolderCopiesToTheCache(t *testing.T) {
	t.Parallel()
	home := stopPlace(t)
	// this folder is under the system's and the run is not to make it
	system := "/var/folders/game-stop-test-none/T"
	run := startRun(t, home, system)
	run.waitForNote(notedRuns)
	copies := copiesIn(home)
	where := filepath.Join("game", "mutants", "game-mutants-")
	if len(copies) != count.One || !strings.Contains(copies[count.First], where) {
		t.Errorf("the copies were %v, not under the cache's game/mutants:\n%s",
			copies, run.out)
	}
	run.stop()
	if left := copiesIn(home); len(left) != count.Nothing {
		t.Errorf("a stopped run left %v", left)
	}
	if _, err := os.Stat(system); err == nil {
		t.Errorf("the run made %s", system)
	}
}
