package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

// with no flags, a run is two workers and twenty seconds a mutant: the
// numbers its usage gives.
func TestMutantsDefaultsAreTheDocumentedOnes(t *testing.T) {
	t.Parallel()
	told, err := mutantsAsked(mustCall(t, "mutants", nil))
	workers := countHeld(t, "default-workers")
	timeout := secondsHeld(t, "default-timeout-seconds")
	if err != nil || told.workers != workers || told.timeout != timeout ||
		told.bench || told.only != "" || told.again != "" ||
		told.report != "" {
		t.Errorf("%+v %v", told, err)
	}
}

// the word that is not a flag is refused and named. nothing is run: a
// run would say something.
func TestMutantsRefusesAWordThatIsNoFlag(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"stray"}, {"--workers", "3", "stray"}} {
		said, err := mutated(t, args...)
		if err == nil || !strings.Contains(err.Error(), `"stray"`) ||
			said != "" {
			t.Errorf("%v: %v, said %q", args, err, said)
		}
	}
}

// the flag alone is no stray word. the run goes ahead with it, at the
// workers it was told.
func TestMutantsTakesAFlagAlone(t *testing.T) {
	t.Parallel()
	some := int(held(t, "some-workers"))
	call := mustCall(t, "mutants", []string{"--workers", strconv.Itoa(some)})
	told, err := mutantsAsked(call)
	if err != nil || told.workers != some {
		t.Errorf("one flag alone: %+v %v", told, err)
	}
	said, err := mutated(t, "--bench", "--workers", strconv.Itoa(some))
	if err != nil || !strings.Contains(said,
		fmt.Sprintf(" at %d workers; ", some)) {
		t.Errorf("a run with a flag: %v, said %q", err, said)
	}
}

// options that make no sense are refused before anything runs: nothing
// is said.
func TestMutantsRefusesNonsenseBeforeItRuns(t *testing.T) {
	t.Parallel()
	for want, args := range map[string][]string{
		"--workers": {"--workers", "0"},
		"--report":  {"--bench", "--report", "r.txt"},
	} {
		said, err := mutated(t, args...)
		if err == nil || !strings.Contains(err.Error(), want) ||
			said != "" {
			t.Errorf("%v: %v, said %q", args, err, said)
		}
	}
}

// the shortest timeout there is is a nanosecond. it is a timeout.
func TestATimeoutOfANanosecondIsOne(t *testing.T) {
	t.Parallel()
	if _, err := mutantOptions("", "", count.One, time.Nanosecond); err != nil {
		t.Error(err)
	}
}

// a timeout the unchanged suite cannot meet leaves its package broken,
// which shows the verb gave the run the timeout it was told. nothing in
// the package is run, and that is the error.
func TestMutantsGivesTheTimeoutItWasTold(t *testing.T) {
	t.Parallel()
	said, err := mutated(t, "--timeout", "1ns")
	if err == nil || !strings.Contains(err.Error(), "could not be run") ||
		!strings.Contains(said, "deadline exceeded; nothing of it was run") {
		t.Errorf("a timeout nothing meets: %v, said %q", err, said)
	}
}

// the run that fails is the error. nothing it returned is read, so
// nothing is tallied.
func TestMutantsGivesARunsFailure(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	gone := repoAt(filepath.Join(t.TempDir(), "gone"))
	err := argsMutate(t, gone, nil, &out)
	if err == nil || out.String() != "" {
		t.Errorf("a run over a tree that is not there: %v, said %q",
			err, out.String())
	}
}

// the bench says the bench and nothing of mutants. the run that is no
// bench says no bench.
func TestMutantsBenchIsABench(t *testing.T) {
	t.Parallel()
	said, err := mutated(t, "--bench")
	if err != nil || !strings.Contains(said, "bench: at most about") ||
		strings.Contains(said, "mutants: ") {
		t.Errorf("%v, said:\n%s", err, said)
	}
	said, _ = mutated(t)
	if strings.Contains(said, "bench:") {
		t.Errorf("a run that is no bench said:\n%s", said)
	}
}

// the report holds every mutant's line, and a report that cannot be
// written is the error.
func TestMutantsWritesTheReportWhereAsked(t *testing.T) {
	t.Parallel()
	report := filepath.Join(t.TempDir(), "r.txt")
	said, err := mutated(t, "--report", report)
	if err == nil || !strings.Contains(err.Error(), "survived") {
		t.Fatalf("a run over a tree with a survivor: %v", err)
	}
	written, readErr := os.ReadFile(report)
	text := string(written)
	if readErr != nil || strings.Contains(text, "partial") ||
		!strings.Contains(text, "survived   tiny.go:") ||
		!strings.Contains(text, "killed     tiny.go:") {
		t.Errorf("wrote %q (%v), said:\n%s", written, readErr, said)
	}
	nowhere := filepath.Join(t.TempDir(), "no", "r.txt")
	if _, err := mutated(t, "--report", nowhere); err == nil ||
		strings.Contains(err.Error(), "survived") {
		t.Errorf("a report that could not be written: %v", err)
	}
}

// benchSuites is three suites whose costs alone differ: a cheap one, a
// middling one and a dear one.
func benchSuites(t *testing.T) []mutants.Suite {
	t.Helper()
	return []mutants.Suite{
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
// workers asked and at four times as many. the report estimates both.
func TestBenchSaysEachSuitesCostDearestFirst(t *testing.T) {
	t.Parallel()
	suites := benchSuites(t)
	workers := int(held(t, "some-workers"))
	found := benchOf(suites, nil, nil)
	asked, later := found.Estimate(workers), found.Estimate(fourTimes*workers)
	var out bytes.Buffer
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
		want := "at most " + mutants.About(suite.Alone()) + " alone"
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
	_ = tellBench(&out, found, workers, true)
	if !strings.Contains(out.String(),
		"bench: stopped before every suite ran; partial\n") {
		t.Errorf("a stopped bench:\n%s", out.String())
	}
}

// suiteOf is a suite that took this long and has this many mutants.
func suiteOf(dir string, took time.Duration, many int) mutants.Suite {
	return mutants.NewSuite(dir, took, many)
}

// the report is written for its owner to change and everyone to read, and
// for no one to run.
func TestMutantsReportIsOwnersToWriteAndNoOnesToRun(t *testing.T) {
	t.Parallel()
	report := filepath.Join(t.TempDir(), "r.txt")
	if _, err := mutated(t, "--report", report); err == nil {
		t.Fatal("a run over a tree with a survivor passed")
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

// each mutant that survives is said as it is decided, before the tally.
func TestMutantsSaysEachDecidedLineOnItsOutput(t *testing.T) {
	t.Parallel()
	said, _ := mutated(t)
	decided := strings.Index(said, "survived   tiny.go:")
	tally := strings.Index(said, "mutants: ")
	if decided < count.Nothing || tally < decided {
		t.Errorf("a decided line comes before the tally:\n%s", said)
	}
}

// a bench decides no mutant, so says no decided line.
func TestMutantsSaysNoDecidedLineWhenNoneWasNamed(t *testing.T) {
	t.Parallel()
	said, err := mutated(t, "--bench")
	if err != nil || strings.Contains(said, "survived ") {
		t.Errorf("said %q: %v", said, err)
	}
}
