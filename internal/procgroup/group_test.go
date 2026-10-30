package procgroup_test

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/procgroup"
)

// the waits and pids of the group tests, defined here.
const (
	childLasts  = "30"                  // defined here: seconds a left-running child would last
	looks       = 300                   // defined here: how many looks for a process to go
	lookEvery   = 10 * time.Millisecond // defined here: a look
	systemFirst = 1                     // defined by the system: pid 1 is its first process
	ownGroup    = 0                     // defined by kill(2): pid 0 is the caller's own group
	everyone    = -1                    // defined by kill(2): pid -1 is every process there is
	lowestPid   = 2                     // defined here: the lowest pid that may lead a group
	exitedWith  = 3                     // defined here: what the command that exits on its own exits with
)

// parent is a shell that starts a wait of its own and says its pid, then
// waits. it is the shape of go test and its test binary.
const parent = "sleep " + childLasts + " & echo $!; wait"

// ender ends a process and its group when a test is over. it does so
// whatever the test found. pids below lowestPid lead no group of the
// test's.
type ender struct {
	cmd *exec.Cmd
}

// end kills the group the command leads and the command, and waits for it.
func (end ender) end() {
	pid := end.cmd.Process.Pid
	if pid < lowestPid {
		return
	}
	syscall.Kill(-pid, syscall.SIGKILL)
	syscall.Kill(pid, syscall.SIGKILL)
	end.cmd.Wait()
}

// killer kills a process by its pid when a test is over.
type killer struct {
	pid int
}

// kill ends the process, if it is a pid that can be one of the test's.
func (end killer) kill() {
	if end.pid >= lowestPid {
		syscall.Kill(end.pid, syscall.SIGKILL)
	}
}

// alive is whether a process by that pid is there to be signalled.
func alive(pid int) bool {
	return syscall.Kill(pid, count.Nothing) == nil
}

// goneSoon waits for the process to go, and says whether it did.
func goneSoon(pid int) bool {
	for range looks {
		if !alive(pid) {
			return true
		}
		time.Sleep(lookEvery)
	}
	return false
}

// stopping a command that leads a group stops the command and what it
// started: the wait the shell left running is gone too.
func TestStoppingACommandStopsWhatItStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", parent)
	procgroup.Leading(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ender{cmd: cmd}.end)
	said, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(said))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(killer{pid: child}.kill)
	cancel()
	cmd.Wait()
	if !goneSoon(child) {
		t.Errorf("pid %d, the wait the shell started, is still running", child)
	}
}

// commands that lead a group are no different when they exit on their
// own. they say what they say and exit as they exit.
func TestACommandThatExitsOnItsOwnIsUnaffected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", "echo said; exit 3")
	procgroup.Leading(cmd)
	out, err := cmd.Output()
	var exited *exec.ExitError
	if string(out) != "said\n" || !errors.As(err, &exited) ||
		exited.ExitCode() != exitedWith {
		t.Errorf("a command that exits on its own: %q, %v", out, err)
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Error("the command does not lead a group of its own")
	}
}

// stopped is a command that was started as pid and is stopped. nothing
// was started: the process is only the pid.
func stopped(pid int) error {
	cmd := exec.Command("true")
	procgroup.Leading(cmd)
	cmd.Process = &os.Process{Pid: pid}
	return cmd.Cancel()
}

// pids that cannot lead a group are refused and say so. what is signalled
// is a pid no process has, so the signal reaches nothing: kill(2) answers
// that there is no such process.
func refused(t *testing.T, pid int) {
	t.Helper()
	err := stopped(pid)
	if err == nil || !strings.Contains(err.Error(), "no command's group") ||
		!errors.Is(err, syscall.ESRCH) {
		t.Errorf("stopping pid %d: %v", pid, err)
	}
}

// the pids that would signal more than a command's group are refused.
// they are the system's first process, the caller's own group and every
// process.
func TestAPidThatWouldSignalMoreThanItsGroupIsRefused(t *testing.T) {
	refused(t, systemFirst)
	refused(t, ownGroup)
	refused(t, everyone)
}
