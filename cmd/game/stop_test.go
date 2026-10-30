package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

// the counts of the stop tests, defined here.
const (
	reportMinimum = 2                      // defined here: a report with a note has at least two lines
	signalGrace   = 300 * time.Millisecond // defined here: the time a signal sent to this process has to be read
)

// foundWith is a report with one outcome in each of these states.
func foundWith(states ...string) mutants.Report {
	var outcomes []mutants.Outcome
	for at, state := range states {
		outcomes = append(outcomes, outcomeAt(state, "a.go", at+1, "TestF"))
	}
	return mutants.NewReport(outcomes, nil, nil, nil, nil)
}

// stopped part way the run writes the report of every mutant that
// finished. the report says at its head that it is partial. it says how
// many were not reached. the tally is still told and the run is still an
// error.
func TestAStoppedRunWritesAPartialReport(t *testing.T) {
	t.Parallel()
	found := foundWith("killed", "stopped", "stopped")
	report := filepath.Join(t.TempDir(), "r.txt")
	var out strings.Builder
	err := writeAndTell(&out, found, report, true)
	if err == nil || !strings.Contains(err.Error(), "stopped with 2 not run") {
		t.Errorf("a stopped run: %v", err)
	}
	if !strings.Contains(out.String(), "mutants: 1 run; killed 1") {
		t.Errorf("a stopped run told %q", out.String())
	}
	written, readErr := os.ReadFile(report)
	want := "partial run: 2 of 3 mutants not reached\n" +
		strings.Join(found.Lines(), "\n") + "\n"
	if readErr != nil || string(written) != want {
		t.Errorf("wrote %q, want %q (%v)", written, want, readErr)
	}
}

// stopped after the last mutant finished the run has none unreached. its
// report is whole and carries no note.
func TestAStoppedRunWithNothingUnreachedIsNotPartial(t *testing.T) {
	t.Parallel()
	found := foundWith("killed")
	report := filepath.Join(t.TempDir(), "r.txt")
	var out strings.Builder
	if err := writeAndTell(&out, found, report, true); err == nil {
		t.Error("a stopped run was not an error")
	}
	written, _ := os.ReadFile(report)
	if want := found.Lines()[count.First] + "\n"; string(written) != want {
		t.Errorf("wrote %q, want %q", written, want)
	}
}

// the run that completes writes the full report. it does not call it
// partial, whatever it found.
func TestACompleteRunWritesAReportThatIsNotPartial(t *testing.T) {
	t.Parallel()
	found := foundWith("killed", "equivalent")
	report := filepath.Join(t.TempDir(), "r.txt")
	var out strings.Builder
	if err := writeAndTell(&out, found, report, false); err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(report)
	if want := strings.Join(found.Lines(), "\n") + "\n"; string(written) != want ||
		strings.Contains(string(written), "partial") {
		t.Errorf("wrote %q, want %q", written, want)
	}
}

// the report is handed over as the run settles. the verb writes the report
// and tells the tally then, and does not do either again when the run
// returns: what the run returns is never asked.
func TestTheReportIsWrittenAsTheRunSettles(t *testing.T) {
	t.Parallel()
	report := filepath.Join(t.TempDir(), "r.txt")
	var out strings.Builder
	tell := &settled{ctx: context.Background(), out: &out, path: report}
	tell.settle(foundWith("killed"))
	if _, err := os.Stat(report); err != nil {
		t.Errorf("the report was not on disk as the run settled: %v", err)
	}
	if err := tell.verdict(foundWith("survived"), false); err != nil {
		t.Errorf("settled: %v", err)
	}
	if strings.Count(out.String(), "mutants: ") != count.One ||
		strings.Contains(out.String(), "survived: ") {
		t.Errorf("the tally was told %q", out.String())
	}
}

// the run's error is a report that cannot be written as the run settles.
func TestAReportThatCannotBeWrittenAsTheRunSettlesIsAnError(t *testing.T) {
	t.Parallel()
	nowhere := filepath.Join(t.TempDir(), "no", "r.txt")
	var out strings.Builder
	tell := &settled{ctx: context.Background(), out: &out, path: nowhere}
	tell.settle(foundWith("killed"))
	if err := tell.verdict(foundWith("killed"), false); err == nil {
		t.Error("a report that could not be written was not an error")
	}
}

// a run that settles with the report not yet written writes it when it
// returns.
func TestARunThatNeverSettledWritesWhenItReturns(t *testing.T) {
	t.Parallel()
	report := filepath.Join(t.TempDir(), "r.txt")
	var out strings.Builder
	tell := &settled{ctx: context.Background(), out: &out, path: report}
	if err := tell.verdict(foundWith("killed"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(report); err != nil ||
		strings.Count(out.String(), "mutants: ") != count.One {
		t.Errorf("not written (%v), told %q", err, out.String())
	}
}

// signalAtFirstLine sends this process the signal the verb has asked to
// be told of, as a stopped run is, when the first line is said to it. it
// then gives the signal time to be read.
type signalAtFirstLine struct {
	t    *testing.T
	sent bool
	said strings.Builder
}

// Write notes what is said and signals at the first.
func (sig *signalAtFirstLine) Write(line []byte) (int, error) {
	if !sig.sent {
		sig.sent = true
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			sig.t.Error(err)
		}
		time.Sleep(signalGrace)
	}
	return sig.said.Write(line)
}

// stopped by a signal the run settles a report that is told as stopped.
// the verb reads the stop from the run's own context. the signal is sent
// to this process. the verb has asked to be told of it. so the test runs
// alone.
func TestARunStoppedByASignalSettlesAsStopped(t *testing.T) {
	sig := &signalAtFirstLine{t: t}
	err := argsMutate(t, repoAt(tinyTree(t)), nil, sig)
	if err == nil || (!strings.Contains(err.Error(), "interrupted") &&
		!strings.Contains(err.Error(), "stopped with")) {
		t.Errorf("a run stopped by a signal: %v, said %q", err,
			sig.said.String())
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
	err := argsMutate(t, repoAt(tiny), []string{"--report", report}, &out)
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
