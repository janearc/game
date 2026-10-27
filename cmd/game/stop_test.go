package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
	"go.uber.org/mock/gomock"
)

// the counts of the stop tests, defined here.
const (
	twice         = 2 // defined here: the tally and the report's note each read the outcomes
	reportMinimum = 2 // defined here: a report with a note has at least two lines
)

// stoppedFound is a run that was stopped with these outcomes. every list
// the verb reads after a run is read. the outcomes are read twice: once
// for the report's note and once for the tally.
func stoppedFound(ctrl *gomock.Controller, lines []string,
	states []string) *Mockfindings {
	var outcomes []finding
	for _, state := range states {
		outcomes = append(outcomes, finding{state: state, name: "n",
			at: "a.go:1 n"})
	}
	found := NewMockfindings(ctrl)
	found.EXPECT().Lines().Return(lines)
	found.EXPECT().Untested().Return(nil)
	found.EXPECT().Broken().Return(nil)
	found.EXPECT().Stale().Return(nil)
	found.EXPECT().Outcomes().Return(outcomes).Times(twice)
	return found
}

// stopped part way the run writes the report of every mutant that
// finished. the report says at its head that it is partial. it says how
// many were not reached. the tally is still told and the run is still an
// error.
func TestAStoppedRunWritesAPartialReport(t *testing.T) {
	t.Parallel()
	lines := []string{
		"killed     a.go:1 a.go F: a => b\tTestF",
		"stopped    a.go:2 a.go F: c => d\t",
		"stopped    a.go:3 a.go F: e => f\t",
	}
	var opts mutants.Options
	ctrl := gomock.NewController(t)
	found := stoppedFound(ctrl, lines, []string{"killed", "stopped", "stopped"})
	report := filepath.Join(t.TempDir(), "r.txt")
	said, err := mutated(mines(ctrl, &opts, found, context.Canceled),
		"--report", report)
	if err == nil || !strings.Contains(err.Error(), "stopped with 2 not run") {
		t.Errorf("a stopped run: %v", err)
	}
	if !strings.Contains(said, "mutants: 1 run; killed 1") {
		t.Errorf("a stopped run told %q", said)
	}
	written, readErr := os.ReadFile(report)
	want := "partial run: 2 of 3 mutants not reached\n" +
		strings.Join(lines, "\n") + "\n"
	if readErr != nil || string(written) != want {
		t.Errorf("wrote %q, want %q (%v)", written, want, readErr)
	}
}

// stopped after the last mutant finished the run has none unreached. its
// report is whole and carries no note.
func TestAStoppedRunWithNothingUnreachedIsNotPartial(t *testing.T) {
	t.Parallel()
	lines := []string{"killed     a.go:1 a.go F: a => b\tTestF"}
	var opts mutants.Options
	ctrl := gomock.NewController(t)
	found := stoppedFound(ctrl, lines, []string{"killed"})
	report := filepath.Join(t.TempDir(), "r.txt")
	if _, err := mutated(mines(ctrl, &opts, found, context.Canceled),
		"--report", report); err == nil {
		t.Error("a stopped run was not an error")
	}
	written, _ := os.ReadFile(report)
	if want := lines[count.First] + "\n"; string(written) != want {
		t.Errorf("wrote %q, want %q", written, want)
	}
}

// the run that completes writes the full report. it does not call it
// partial, whatever it found.
func TestACompleteRunWritesAReportThatIsNotPartial(t *testing.T) {
	t.Parallel()
	lines := []string{
		"killed     a.go:1 a.go F: a => b\tTestF",
		"equivalent a.go:2 a.go F: c => d\t",
	}
	var opts mutants.Options
	ctrl := gomock.NewController(t)
	found := nothingFound(ctrl)
	found.EXPECT().Lines().Return(lines)
	report := filepath.Join(t.TempDir(), "r.txt")
	if _, err := mutated(mines(ctrl, &opts, found, nil), "--report",
		report); err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(report)
	if want := strings.Join(lines, "\n") + "\n"; string(written) != want ||
		strings.Contains(string(written), "partial") {
		t.Errorf("wrote %q, want %q", written, want)
	}
}

// settlingMiner is a miner that settles with the findings it is given
// before it returns, as the real one settles before it removes its copies.
// it notes whether the report was on disk when it was about to return.
type settlingMiner struct {
	settles, returns findings
	err              error
	report           string
	onDisk           *bool
	before           func()
}

// run settles and notes whether the report is on disk. then it returns.
func (miner settlingMiner) run(ctx context.Context, _ string,
	_ mutants.Options, settled func(findings),
	_ func(string)) (findings, error) {
	if miner.before != nil {
		miner.before()
		<-ctx.Done()
	}
	settled(miner.settles)
	_, err := os.Stat(miner.report)
	*miner.onDisk = err == nil
	return miner.returns, miner.err
}

// the miner hands the report over as it settles, before it removes its
// copies. the verb writes the report and tells the tally then, and does
// not do either again when the run returns: the findings the miner returns
// are never asked. the report is on disk before the miner returns.
func TestTheReportIsWrittenAsTheRunSettles(t *testing.T) {
	t.Parallel()
	report := filepath.Join(t.TempDir(), "r.txt")
	ctrl := gomock.NewController(t)
	found := nothingFound(ctrl)
	found.EXPECT().Lines().Return(nil)
	var onDisk bool
	stub := settlingMiner{settles: found, returns: NewMockfindings(ctrl),
		report: report, onDisk: &onDisk}
	run := NewMockminer(ctrl)
	run.EXPECT().Run(gomock.Any(), ".", gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(stub.run)
	said, err := mutated(run, "--report", report)
	if err != nil || !onDisk {
		t.Errorf("settled: %v, on disk when the miner came back: %v",
			err, onDisk)
	}
	if strings.Count(said, "mutants: 0 run") != count.One {
		t.Errorf("the tally was told %q", said)
	}
}

// the run's error is a report that cannot be written as the run settles.
func TestAReportThatCannotBeWrittenAsTheRunSettlesIsAnError(t *testing.T) {
	t.Parallel()
	nowhere := filepath.Join(t.TempDir(), "no", "r.txt")
	ctrl := gomock.NewController(t)
	found := NewMockfindings(ctrl)
	found.EXPECT().Lines().Return(nil)
	var onDisk bool
	stub := settlingMiner{settles: found, returns: NewMockfindings(ctrl),
		report: nowhere, onDisk: &onDisk}
	run := NewMockminer(ctrl)
	run.EXPECT().Run(gomock.Any(), ".", gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(stub.run)
	if _, err := mutated(run, "--report", nowhere); err == nil {
		t.Error("a report that could not be written was not an error")
	}
}

// signalSelf sends this process the signal the verb has asked to be told
// of, as a stopped run is.
func signalSelf(t *testing.T) func() {
	return signalling{t: t}.send
}

// signalling is a test that sends its own process SIGTERM.
type signalling struct {
	t *testing.T
}

// send sends the signal.
func (sig signalling) send() {
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		sig.t.Error(err)
	}
}

// stopped by a signal the run settles a report that is told as stopped.
// the verb reads the stop from the run's own context. the signal is sent
// to this process. the verb has asked to be told of it. so the test runs
// alone.
func TestARunStoppedByASignalSettlesAsStopped(t *testing.T) {
	ctrl := gomock.NewController(t)
	found := nothingFound(ctrl)
	var onDisk bool
	stub := settlingMiner{settles: found, returns: NewMockfindings(ctrl),
		report: os.DevNull, onDisk: &onDisk, before: signalSelf(t)}
	stub.err = context.Canceled
	run := NewMockminer(ctrl)
	run.EXPECT().Run(gomock.Any(), ".", gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(stub.run)
	_, err := mutated(run)
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("a run stopped by a signal: %v", err)
	}
}

// the run that finishes removes its copies and keeps what it found. the
// report is on disk whole and not partial when the copies' folder is
// empty. the fixture's one unheld function survives. so the run is an
// error that says so. it does not run the tree it is in.
func TestAFinishedRunKeepsItsReportWhenTheCopiesGo(t *testing.T) {
	// the test's own folders are made before the temp folder is pointed
	// at the place, so the place holds only what the run puts in it
	tiny := tinyTree(t)
	report := filepath.Join(t.TempDir(), "report")
	place := stopPlace(t)
	t.Setenv("TMPDIR", place)
	var out strings.Builder
	err := mutateBy(repoAt(tiny), []string{"--report", report},
		diskMiner{}, &out)
	if err == nil || !strings.Contains(err.Error(), "survived") {
		t.Errorf("a run over a tree with a survivor: %v", err)
	}
	written, readErr := os.ReadFile(report)
	text := strings.TrimSpace(string(written))
	lines := strings.Split(text, "\n")
	if readErr != nil || len(lines) < reportMinimum ||
		strings.Contains(text, "partial") ||
		strings.Contains(text, "stopped ") {
		t.Errorf("the report after the copies went (%v):\n%s", readErr, text)
	}
	left, _ := os.ReadDir(place)
	if len(left) != count.Nothing {
		t.Errorf("a finished run left %d entries in %s", len(left), place)
	}
}
