package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
	"go.uber.org/mock/gomock"
)

// nothingFound is a run that found nothing: each list the verb reads
// after a run is read once, and is empty.
func nothingFound(ctrl *gomock.Controller) *Mockfindings {
	found := NewMockfindings(ctrl)
	found.EXPECT().Untested().Return(nil)
	found.EXPECT().Broken().Return(nil)
	found.EXPECT().Stale().Return(nil)
	found.EXPECT().Outcomes().Return(nil)
	return found
}

// benchFound is a bench that found these suites, and says the run would
// take asked at the workers given and later at four times as many.
func benchFound(ctrl *gomock.Controller, suites []suiteCost, workers int,
	asked, later time.Duration) *Mockfindings {
	return benchOf(ctrl, suites, nil, nil, workers, asked, later)
}

// mines is a miner that expects to be run once on the tree at ".". it
// keeps the options it was given in opts. it finds found or fails with
// err.
func mines(ctrl *gomock.Controller, opts *mutants.Options, found findings,
	err error) *Mockminer {
	run := NewMockminer(ctrl)
	run.EXPECT().Run(gomock.Any(), ".", gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		minerStub{opts: opts, found: found, err: err}.run)
	return run
}

// minerStub is what a mocked run does when it is called: it keeps the
// options it was given and gives what it was told to.
type minerStub struct {
	opts  *mutants.Options
	found findings
	err   error
}

// run is the mocked miner's Run.
func (stub minerStub) run(_ context.Context, _ string,
	given mutants.Options, _ func(findings),
	_ func(string)) (findings, error) {
	*stub.opts = given
	return stub.found, stub.err
}

// mutated is mutate over a miner, with what it said.
func mutated(run miner, args ...string) (string, error) {
	var out bytes.Buffer
	err := mutateBy(repoAt("."), args, run, &out)
	return out.String(), err
}

// with no flags, a run is two workers and twenty seconds a mutant: the
// numbers its usage gives.
func TestMutantsDefaultsAreTheDocumentedOnes(t *testing.T) {
	t.Parallel()
	told, err := mutantFlags(nil)
	workers := countHeld(t, "default-workers")
	timeout := secondsHeld(t, "default-timeout-seconds")
	if err != nil || told.workers != workers || told.timeout != timeout ||
		told.bench || told.only != "" || told.again != "" ||
		told.report != "" {
		t.Errorf("%+v %v", told, err)
	}
}

// the word that is not a flag is refused and named. the miner is never run.
// it has no expectation so a call to it fails the test.
func TestMutantsRefusesAWordThatIsNoFlag(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"stray"}, {"--workers", "3", "stray"}} {
		run := NewMockminer(gomock.NewController(t))
		_, err := mutated(run, args...)
		if err == nil || !strings.Contains(err.Error(), `"stray"`) {
			t.Errorf("%v: %v", args, err)
		}
	}
}

// the flag alone is no stray word. the run goes ahead with it.
func TestMutantsTakesAFlagAlone(t *testing.T) {
	t.Parallel()
	some := int(held(t, "some-workers"))
	told, err := mutantFlags([]string{"--workers", strconv.Itoa(some)})
	if err != nil || told.workers != some {
		t.Errorf("one flag alone: %+v %v", told, err)
	}
	var opts mutants.Options
	ctrl := gomock.NewController(t)
	run := mines(ctrl, &opts, nothingFound(ctrl), nil)
	if _, err := mutated(run, "--workers", strconv.Itoa(some)); err != nil {
		t.Errorf("a run with a flag: %v", err)
	}
}

// options that make no sense are refused before anything runs: the miner
// has no expectation, so it is not called.
func TestMutantsRefusesNonsenseBeforeItRuns(t *testing.T) {
	t.Parallel()
	for want, args := range map[string][]string{
		"--workers": {"--workers", "0"},
		"--report":  {"--bench", "--report", "r.txt"},
	} {
		run := NewMockminer(gomock.NewController(t))
		_, err := mutated(run, args...)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v", args, err)
		}
	}
}

// either a bench or a report alone is fine, and runs.
func TestMutantsTakesABenchOrAReportAlone(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	var opts mutants.Options
	run := mines(ctrl, &opts, benchFound(ctrl, nil, twoWorkers, noTime, noTime), nil)
	if _, err := mutated(run, "--bench"); err != nil {
		t.Errorf("a bench: %v", err)
	}
	found := nothingFound(ctrl)
	found.EXPECT().Lines().Return(nil)
	run = mines(ctrl, &opts, found, nil)
	report := filepath.Join(t.TempDir(), "r.txt")
	if _, err := mutated(run, "--report", report); err != nil {
		t.Errorf("a report: %v", err)
	}
}

// the shortest timeout there is is a nanosecond. it is a timeout.
func TestATimeoutOfANanosecondIsOne(t *testing.T) {
	t.Parallel()
	if _, err := mutantOptions("", "", count.One, time.Nanosecond); err != nil {
		t.Error(err)
	}
}

// the run that fails is the error. nothing it returned is read. the miner
// gives no findings at all. the run that was interrupted says what it found
// and says it was stopped.
func TestMutantsGivesARunsFailureAndSaysAStoppedOne(t *testing.T) {
	t.Parallel()
	var opts mutants.Options
	ctrl := gomock.NewController(t)
	failed := mines(ctrl, &opts, nil, errors.New("no tree here"))
	if _, err := mutated(failed); err == nil ||
		err.Error() != "no tree here" {
		t.Errorf("a failed run: %v", err)
	}
	stopped := mines(ctrl, &opts, nothingFound(ctrl), context.Canceled)
	said, err := mutated(stopped)
	if err == nil || !strings.Contains(err.Error(), "interrupted") ||
		!strings.Contains(said, "mutants: 0 run") {
		t.Errorf("a stopped run: %v, said %q", err, said)
	}
	sound := mines(ctrl, &opts, nothingFound(ctrl), nil)
	said, err = mutated(sound, "--workers", "3", "--timeout", "7s")
	if err != nil || !strings.Contains(said, "mutants: 0 run") ||
		!strings.HasPrefix(opts.String(), "3 workers, 7s a mutant, copies in ") {
		t.Errorf("a sound run: %v, said %q, opts %v", err, said, opts)
	}
}

// the bench says the bench and nothing of mutants. it is told it is one.
// the run that is no bench is not told so. it says no bench.
func TestMutantsBenchIsABench(t *testing.T) {
	t.Parallel()
	some := countHeld(t, "some-workers")
	suites := []suiteCost{{dir: "a", took: time.Second, alone: time.Second,
		mutants: some}}
	var opts mutants.Options
	ctrl := gomock.NewController(t)
	bench := mines(ctrl, &opts, benchFound(ctrl, suites, twoWorkers,
		time.Second, time.Second), nil)
	said, err := mutated(bench, "--bench")
	if err != nil || !strings.HasSuffix(opts.String(), ", a bench") ||
		!strings.Contains(said, "bench: at most about") ||
		strings.Contains(said, "mutants: ") {
		t.Errorf("%v, opts %v, said:\n%s", err, opts, said)
	}
	plain := mines(ctrl, &opts, nothingFound(ctrl), nil)
	said, _ = mutated(plain)
	if strings.Contains(opts.String(), "bench") ||
		strings.Contains(said, "bench:") {
		t.Errorf("a run that is no bench: %v, said:\n%s", opts, said)
	}
}

// the report holds every mutant's line, and a report that cannot be
// written is the error.
func TestMutantsWritesTheReportWhereAsked(t *testing.T) {
	t.Parallel()
	lines := []string{"killed     a.go:1 a.go F: x > 0 => x >= 0\tTestF"}
	var opts mutants.Options
	ctrl := gomock.NewController(t)
	found := nothingFound(ctrl)
	found.EXPECT().Lines().Return(lines)
	report := filepath.Join(t.TempDir(), "r.txt")
	if _, err := mutated(mines(ctrl, &opts, found, nil), "--report",
		report); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(report)
	want := strings.Join(lines, "\n") + "\n"
	if err != nil || string(written) != want {
		t.Errorf("wrote %q, want %q (%v)", written, want, err)
	}
	found = NewMockfindings(ctrl)
	found.EXPECT().Lines().Return(lines)
	nowhere := filepath.Join(t.TempDir(), "no", "r.txt")
	if _, err := mutated(mines(ctrl, &opts, found, nil), "--report",
		nowhere); err == nil {
		t.Error("a report that could not be written was not an error")
	}
}

// benchSuites is three suites whose costs alone differ: a cheap one, a
// middling one and a dear one.
func benchSuites(t *testing.T) []suiteCost {
	t.Helper()
	return []suiteCost{
		suiteOf("cheap", secondsHeld(t, "cheap-seconds"),
			countHeld(t, "cheap-mutants")),
		suiteOf("dear", secondsHeld(t, "bench-slow-seconds"),
			countHeld(t, "bench-slow-mutants")),
		suiteOf("middling", secondsHeld(t, "middling-seconds"),
			countHeld(t, "middling-mutants")),
	}
}

// the bench lists the suites dearest first. each has what its mutants cost
// alone. the stopped bench says so. the last line gives the run at the
// workers asked and at four times as many. the findings estimate both.
func TestBenchSaysEachSuitesCostDearestFirst(t *testing.T) {
	t.Parallel()
	suites := benchSuites(t)
	workers := int(held(t, "some-workers"))
	asked := secondsHeld(t, "middling-seconds")
	later := secondsHeld(t, "cheap-seconds")
	ctrl := gomock.NewController(t)
	var out bytes.Buffer
	found := benchFound(ctrl, suites, workers, asked, later)
	if err := tellBench(&out, found, workers, false); err != nil {
		t.Fatal(err)
	}
	said := out.String()
	dear := strings.Index(said, "dear ")
	middling := strings.Index(said, "middling ")
	cheap := strings.Index(said, "cheap ")
	if dear < count.Nothing || !(dear < middling && middling < cheap) {
		t.Errorf("not dearest first:\n%s", said)
	}
	for _, suite := range suites {
		want := "at most " + mutants.About(suite.alone) + " alone"
		if !strings.Contains(said, want) {
			t.Errorf("want %q in:\n%s", want, said)
		}
	}
	tail := fmt.Sprintf("bench: at most about %s at %d workers; %s at %d\n",
		mutants.About(asked), workers, mutants.About(later),
		fourTimes*workers)
	if !strings.Contains(said, tail) {
		t.Errorf("want %q in:\n%s", tail, said)
	}
	if strings.Contains(said, "stopped") {
		t.Errorf("a whole bench said it was stopped:\n%s", said)
	}
	out.Reset()
	again := benchFound(ctrl, suites, workers, asked, later)
	_ = tellBench(&out, again, workers, true)
	if !strings.Contains(out.String(),
		"bench: stopped before every suite ran; partial\n") {
		t.Errorf("a stopped bench:\n%s", out.String())
	}
}

// suiteOf is a suite that took this long and has this many mutants.
func suiteOf(dir string, took time.Duration, many int) suiteCost {
	return suiteCost{dir: dir, took: took,
		alone: took * time.Duration(many), mutants: many}
}

// the report is written for its owner to change and everyone to read, and
// for no one to run.
func TestMutantsReportIsOwnersToWriteAndNoOnesToRun(t *testing.T) {
	t.Parallel()
	var opts mutants.Options
	ctrl := gomock.NewController(t)
	found := nothingFound(ctrl)
	found.EXPECT().Lines().Return(nil)
	report := filepath.Join(t.TempDir(), "r.txt")
	if _, err := mutated(mines(ctrl, &opts, found, nil), "--report",
		report); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(report)
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm()
	owner, run, others := os.FileMode(0o600), os.FileMode(0o111),
		os.FileMode(0o022)
	if perm&owner != owner || perm&run != count.Nothing ||
		perm&others != count.Nothing {
		t.Errorf("the report is mode %v", perm)
	}
}

// decidingMiner is a miner stub that says these lines as it decides
// mutants, and then finds nothing.
type decidingMiner struct {
	lines []string
	found findings
}

// run is the mocked miner's Run: it says the lines through decided and
// gives what it found.
func (stub decidingMiner) run(_ context.Context, _ string,
	_ mutants.Options, _ func(findings),
	decided func(string)) (findings, error) {
	for _, line := range stub.lines {
		decided(line)
	}
	return stub.found, nil
}

// TestMutantsSaysEachDecidedLineOnItsOutput: before the tally.
func TestMutantsSaysEachDecidedLineOnItsOutput(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	lines := []string{
		"survived   a.go:3 a.go F: return 1 + 1 => return 1 - 1\t",
		"timed out  b.go:9 b.go G: x < 2 => x <= 2\t",
	}
	stub := decidingMiner{lines: lines, found: nothingFound(ctrl)}
	run := NewMockminer(ctrl)
	run.EXPECT().Run(gomock.Any(), ".", gomock.Any(), gomock.Any(),
		gomock.Any()).DoAndReturn(stub.run)
	said, err := mutated(run)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join(lines, "\n") + "\nmutants: 0 run"
	if !strings.HasPrefix(said, want) {
		t.Errorf("said %q, want it to begin %q", said, want)
	}
}

// TestMutantsSaysNoDecidedLineWhenNoneWasNamed: none is none.
func TestMutantsSaysNoDecidedLineWhenNoneWasNamed(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	run := NewMockminer(ctrl)
	stub := decidingMiner{found: nothingFound(ctrl)}
	run.EXPECT().Run(gomock.Any(), ".", gomock.Any(), gomock.Any(),
		gomock.Any()).DoAndReturn(stub.run)
	said, err := mutated(run)
	if err != nil || !strings.HasPrefix(said, "mutants: 0 run") {
		t.Errorf("said %q: %v", said, err)
	}
}
