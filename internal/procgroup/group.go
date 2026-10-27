// Package procgroup makes a command lead a process group of its own. It
// makes stopping the command stop the group. The test binary go test runs
// is a child of go. Stopping go alone leaves the binary running along
// with whatever it started.
package procgroup

import (
	"errors"
	"fmt"
	"math"
	"os/exec"
	"syscall"
)

// lowestLeader is the lowest pid that may lead a group that is signalled.
// It's defined: pid 1 is the system's first process, and a signal to group
// 0 is a signal to game's own group.
const lowestLeader = 2

// noGroup is a pid no process has, so kill answers that there is no such
// process. It's defined: pids on darwin and linux stop far below it.
const noGroup = math.MaxInt32

// errNoGroup is groupOf's refusal.
var errNoGroup = errors.New("no command's group to signal")

// groupOf is the process group a command leads, as kill takes it: the
// command's pid made negative. It refuses a pid below lowestLeader, since
// kill(0) signals game's own group and kill(-1) every process of the
// user's.
//
// With its refusal it gives noGroup, so even a kill sent with it reaches
// nothing.
func groupOf(pid int) (int, error) {
	if pid < lowestLeader {
		return noGroup, fmt.Errorf("%w: pid %d", errNoGroup, pid)
	}
	return -pid, nil
}

// Signals is the signals a stopped command's group is sent. System sends
// them for real. Tests give a mock, so that a test of what is refused
// never signals anything.
type Signals interface {
	// Kill sends the signal to the pid, as kill(2) does: a negative pid
	// is a process group.
	Kill(pid int, sig syscall.Signal) error
}

// system sends signals with kill(2).
type system struct{}

// Kill sends the signal.
func (system) Kill(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

// System is the signals of this machine, which are sent for real.
func System() Signals { return system{} }

// leader is a command that leads a group of its own, and the signals it is
// stopped with.
type leader struct {
	cmd     *exec.Cmd
	signals Signals
}

// cancel stops the group the command leads, and says why when it is
// refused one to stop. It is what exec.Cmd calls when the command's
// context is done.
func (lead leader) cancel() error {
	group, refused := groupOf(lead.cmd.Process.Pid)
	killed := lead.signals.Kill(group, syscall.SIGKILL)
	return errors.Join(refused, killed)
}

// Leading makes the command lead a group of its own. Stopping it by its
// context then stops the group with the signals given.
func Leading(cmd *exec.Cmd, signals Signals) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = leader{cmd: cmd, signals: signals}.cancel
}
