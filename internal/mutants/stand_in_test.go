package mutants_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
)

// the waits of the stand-in tests, defined here.
const (
	childPause = "30"                  // defined here: seconds a left-running wait would last
	standLooks = 300                   // defined here: how many looks for a process to appear or go
	standEvery = 10 * time.Millisecond // defined here: a look
	loopBound  = 10 * time.Second      // defined here: far longer than a stand-in needs to refuse
	noteSecond = 2                     // defined here: a note of the stand-in with a wait has two pids
	wantPids   = 4                     // defined here: go list, the suite, the mutant and the wait it left
)

// pidsOf is the pids the stand-in has noted so far.
func (stub goStub) pidsOf(t *testing.T) []int {
	t.Helper()
	noted, _ := os.ReadFile(filepath.Join(stub.dir, "pids"))
	var pids []int
	for _, field := range strings.Fields(string(noted)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatal(err)
		}
		pids = append(pids, pid)
	}
	return pids
}

// alive is whether a process by that pid is there to be signalled.
func alive(pid int) bool {
	return syscall.Kill(pid, count.Nothing) == nil
}

// goneSoon waits for the process to be gone, and says whether it went.
func goneSoon(pid int) bool {
	for range standLooks {
		if !alive(pid) {
			return true
		}
		time.Sleep(standEvery)
	}
	return false
}

// waitForPids waits until the stand-in has noted that many pids.
func (stub goStub) waitForPids(t *testing.T, many int) {
	t.Helper()
	for range standLooks {
		if len(stub.pidsOf(t)) >= many {
			return
		}
		time.Sleep(standEvery)
	}
	t.Fatalf("the stand-in noted %v, wanted %d pids", stub.pidsOf(t), many)
}

// stand-ins that have started a wait and left it running are ended with
// their wait when the test is over. end gives both and they are gone.
func TestAStandInLeftRunningIsEnded(t *testing.T) {
	stub := stubGo(t)
	stub.keep(t, "child.1", childPause)
	cmd := exec.Command("go", "test")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stub.waitForPids(t, noteSecond)
	ended := stub.end(t)
	// the stand-in is this test's child, so it is reaped to be gone
	cmd.Wait()
	if len(ended) != noteSecond {
		t.Fatalf("ended %v of %v", ended, stub.pidsOf(t))
	}
	for _, pid := range ended {
		if !goneSoon(pid) {
			t.Errorf("pid %d is still there after it was ended", pid)
		}
	}
}

// stand-ins that exit normally are unaffected by the cleanup. their
// answer is what they were told to give, and ending them ends nothing.
// pids noted that are other processes' now are left alone. such numbers
// have been given out again.
func TestAStandInThatExitedIsUnaffectedAndAnotherProcessIsLeftAlone(t *testing.T) {
	stub := stubGo(t)
	stub.answer(t, count.One, "fine\n", count.Nothing)
	out, err := exec.Command("go", "test").Output()
	if err != nil || string(out) != "fine\n" {
		t.Fatalf("a stand-in that exits normally gave %q, %v", out, err)
	}
	other := exec.Command("sleep", childPause)
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reaper{cmd: other}.reap)
	noted, _ := os.ReadFile(filepath.Join(stub.dir, "pids"))
	stub.keep(t, "pids", string(noted)+strconv.Itoa(other.Process.Pid)+"\n")
	if ended := stub.end(t); len(ended) != count.Nothing {
		t.Errorf("ended %v: the stand-in had exited and the other was not its own",
			ended)
	}
	if !alive(other.Process.Pid) {
		t.Error("a process that was not the stand-in's was ended")
	}
}

// the stand-in whose REAL_GO is itself is what a broken stand-in does. it
// refuses and exits where it would run without end.
func TestAStandInThatRunsItselfExits(t *testing.T) {
	stub := stubGo(t)
	t.Setenv("REAL_GO", filepath.Join(stub.bin, "go"))
	ctx, cancel := context.WithTimeout(context.Background(), loopBound)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list")
	var errs strings.Builder
	cmd.Stderr = &errs
	err := cmd.Run()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != exitRefusedSelf ||
		!strings.Contains(errs.String(), "runs itself") {
		t.Errorf("a stand-in that runs itself: %v, said %q", err, errs.String())
	}
}

// exitRefusedSelf is what the stand-in exits with when it finds it runs
// itself, defined by the stand-in.
const exitRefusedSelf = 97

// cancelling a run ends what go test started as well as go test: the
// test binary that go test leaves running holds nothing of the run when it
// returns.
func TestCancellingARunEndsWhatGoTestStarted(t *testing.T) {
	placed(t)
	stub := stubGo(t)
	root := fixture(t, "")
	stub.answer(t, count.One, "ok\n", count.Nothing)
	stub.keep(t, "child.2", childPause)
	ctx, cancel := context.WithCancel(context.Background())
	go cancelWhenNoted(t, stub, wantPids, cancel)
	files, _ := disk(t)
	// one worker, so the second run of go test is the mutant that waits
	opts := askFor(count.One, secondsHeld(t, "test-timeout-seconds"))
	_, err := runBefore(t, ctx, root, opts.Writing(files))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled run returned %v", err)
	}
	var left []int
	for _, pid := range stub.pidsOf(t) {
		if !goneSoon(pid) {
			left = append(left, pid)
		}
	}
	if !slices.Equal(left, nil) {
		t.Errorf("a cancelled run left %v running", left)
	}
}

// reaper ends a process the test started and waits for it, when the test
// is over.
type reaper struct {
	cmd *exec.Cmd
}

// reap kills the process and waits for it.
func (end reaper) reap() {
	end.cmd.Process.Kill()
	end.cmd.Wait()
}

// cancelWhenNoted cancels once the stand-in has noted that many pids.
func cancelWhenNoted(t *testing.T, stub goStub, many int,
	cancel context.CancelFunc) {
	for len(stub.pidsOf(t)) < many {
		time.Sleep(standEvery)
	}
	cancel()
}
