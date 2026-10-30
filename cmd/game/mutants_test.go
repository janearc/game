package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

// outcomeAt is the outcome in this state of a change on that line of
// f in that file, with the tests that failed on it.
func outcomeAt(state, file string, line int, failing string) mutants.Outcome {
	return mutants.NewOutcome(
		mutants.NewMutant(file, "f", line, "a", "b", nil), state, failing)
}

// benchOf is a bench of these suites with these broken packages and stale
// lines.
func benchOf(suites []mutants.Suite, broken, stale []string) mutants.Report {
	return mutants.NewReport(nil, nil, broken, stale, suites)
}

// verdictCase is a run as the verdict reads it. it holds the packages that
// could not be run and the stale lines. it holds the state of each mutant.
type verdictCase struct {
	name, want    string
	broken, stale []string
	states        []string
}

// verdictOf is the verdict on the case, tallied as the verb tallies.
func verdictOf(each verdictCase, stopped bool) error {
	counts := map[string]int{}
	for _, state := range each.states {
		counts[state]++
	}
	return mutantsVerdict(len(each.broken), len(each.stale), counts, stopped)
}

// each reason a run is not clean is said by the verdict, and a run with
// none is not an error.
func TestMutantsVerdict(t *testing.T) {
	t.Parallel()
	cases := []verdictCase{
		{name: "nothing"},
		{name: "killed and equivalent", states: []string{"killed",
			"killed", "equivalent", "unbuildable"}},
		{name: "a broken package", broken: []string{"p"},
			want: "1 could not be run"},
		{name: "a broken mutant", states: []string{"broken"},
			want: "1 could not be run"},
		{name: "survivors", states: []string{"survived", "survived"},
			want: "2 survived"},
		{name: "timed out", states: []string{"timed out", "timed out",
			"timed out"}, want: "3 timed out"},
		{name: "stale", stale: []string{"a"}, want: "1 stale"},
		{name: "stopped", states: []string{"stopped"},
			want: "stopped with 1"},
	}
	for _, each := range cases {
		err := verdictOf(each, false)
		switch {
		case each.want == "" && err != nil:
			t.Errorf("%s: %v", each.name, err)
		case each.want != "" && (err == nil ||
			!strings.Contains(err.Error(), each.want)):
			t.Errorf("%s: %v, want %q", each.name, err, each.want)
		}
	}
}

// every reason is said at once. the interrupted run is a reason by itself
// and needs no count. the run that was not interrupted does not say it
// was.
func TestMutantsVerdictSaysEveryReason(t *testing.T) {
	t.Parallel()
	everything := verdictCase{
		broken: []string{"p"}, stale: []string{"a"},
		states: []string{"survived", "stopped", "broken", "timed out"},
	}
	err := verdictOf(everything, false)
	for _, want := range []string{"2 could not be run", "1 survived",
		"1 timed out", "1 stale", "stopped with 1"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("every reason: %v lacks %q", err, want)
		}
	}
	if err != nil && strings.Contains(err.Error(), "interrupted") {
		t.Errorf("a run with stopped mutants was interrupted too: %v", err)
	}
	err = verdictOf(verdictCase{}, true)
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("stopped before anything ran: %v", err)
	}
}

func TestMutantOptionsRefuseNonsense(t *testing.T) {
	t.Parallel()
	bad := []struct {
		want, only, again string
		workers           int
		timeout           time.Duration
	}{
		{"--workers", "", "", count.Nothing, time.Second},
		{"--timeout", "", "", count.One, time.Duration(count.Nothing)},
		{"--only", "(", "", count.One, time.Second},
	}
	for _, each := range bad {
		_, err := mutantOptions(each.only, each.again, each.workers,
			each.timeout)
		if err == nil || !strings.Contains(err.Error(), each.want) {
			t.Errorf("%s: %v", each.want, err)
		}
	}
	if _, err := mutantOptions("", "/nowhere/report.txt", count.One,
		time.Second); err == nil {
		t.Error("a missing report was read")
	}
	workers := int(held(t, "some-workers"))
	opts, err := mutantOptions("^x", "", workers, time.Minute)
	if err != nil || !strings.HasPrefix(opts.String(), "3 workers, 1m0s a mutant, only ^x, copies in ") {
		t.Errorf("%v %v", opts, err)
	}
}

// aBench is the one suite a bench of these tests times, and the workers
// it is asked at.
type aBench struct {
	suites  []mutants.Suite
	workers int
}

// found is the bench with these broken packages and stale lines.
func (bench aBench) found(broken, stale []string) mutants.Report {
	return benchOf(bench.suites, broken, stale)
}

// the bench fails only for what could not be run. that is a broken package
// or a stale line or an interrupt. the sound bench does not fail.
func TestBenchFailsOnlyForWhatCouldNotRun(t *testing.T) {
	t.Parallel()
	slow := secondsHeld(t, "bench-slow-seconds")
	workers := int(held(t, "bench-workers"))
	bench := aBench{
		workers: workers,
		suites:  []mutants.Suite{mutants.NewSuite("a", slow, count.One)},
	}
	if err := tellBench(io.Discard, bench.found(nil, nil), workers,
		false); err != nil {
		t.Errorf("a sound bench: %v", err)
	}
	err := tellBench(io.Discard, bench.found([]string{"c: no"}, nil),
		workers, false)
	if err == nil || !strings.Contains(err.Error(), "could not be run") {
		t.Errorf("a broken bench: %v", err)
	}
	err = tellBench(io.Discard, bench.found(nil, nil), workers, true)
	if err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("an interrupted bench: %v", err)
	}
	err = tellBench(io.Discard, bench.found(nil, []string{"a b: c => d"}),
		workers, false)
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Errorf("a stale bench: %v", err)
	}
}

// the bench names each broken package and each stale line.
func TestBenchNamesWhatIsBrokenAndStale(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	found := benchOf(nil, []string{"c: no"}, []string{"old"})
	_ = tellBench(&out, found, count.One, false)
	for _, want := range []string{"  broken: c: no\n",
		"stale: .mutants names a change the source no longer has: old\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("want %q in:\n%s", want, out.String())
		}
	}
}

// tellMutants says what could not be run and what survived. it says what
// timed out. each is on its own line. the tally follows and counts every
// state.
func TestTellMutantsSaysEachKind(t *testing.T) {
	t.Parallel()
	var outcomes []mutants.Outcome
	for at, state := range []string{"killed", "survived", "timed out",
		"broken", "equivalent", "unbuildable", "stopped"} {
		file := string(rune('a'+at)) + ".go"
		outcomes = append(outcomes, outcomeAt(state, file, at+1, "why"))
	}
	found := mutants.NewReport(outcomes, []string{"cmd/x"}, []string{"p: no"},
		[]string{"old"}, nil)
	var out strings.Builder
	err := tellMutants(&out, found, false)
	if err == nil {
		t.Fatal("a report with survivors and timeouts passed")
	}
	want := []string{
		"untested: cmd/x has source and no tests\n",
		"broken: p: no; nothing of it was run\n",
		"stale: .mutants names a change the source no longer has: old\n",
		"broken: d.go f: a => b: why\n",
		"survived: b.go:2 b.go f: a => b\n",
		"timed out: c.go:3 c.go f: a => b\n",
		"mutants: 6 run; killed 1, survived 1, timed out 1, " +
			"equivalent 1, would not build 1\n",
	}
	if out.String() != strings.Join(want, "") {
		t.Errorf("told\n%s\nwant\n%s", out.String(), strings.Join(want, ""))
	}
}

// the clean run says only its tally and is no error. nothing survived. so
// no survivor or timeout or break is said.
func TestTellMutantsSaysNothingOfACleanRun(t *testing.T) {
	t.Parallel()
	found := mutants.NewReport([]mutants.Outcome{
		outcomeAt("killed", "a.go", 1, ""),
		outcomeAt("equivalent", "a.go", 2, ""),
	}, nil, nil, nil, nil)
	var out strings.Builder
	if err := tellMutants(&out, found, false); err != nil {
		t.Errorf("a clean run: %v", err)
	}
	want := "mutants: 2 run; killed 1, survived 0, timed out 0, " +
		"equivalent 1, would not build 0\n"
	if out.String() != want {
		t.Errorf("told %q, want %q", out.String(), want)
	}
}

// tinyTree is a copy of the mutants fixture module in a directory of
// its own, which a run may change.
func tinyTree(t *testing.T) string {
	t.Helper()
	tiny, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture := os.DirFS("../../internal/mutants/testdata/tiny")
	if err := os.CopyFS(tiny, fixture); err != nil {
		t.Fatal(err)
	}
	return tiny
}

// mutated is game mutants over a copy of the fixture, with what it said.
func mutated(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := argsMutate(t, repoAt(tinyTree(t)), args, &out)
	return out.String(), err
}

// the verb runs the tree it is given: the bench times the fixture's one
// suite, and the run names and places its mutants and sorts them into
// states. the held function is killed. the unheld one survives. the one
// that will not build is said so. the tally is told once.
func TestMutantsRunsTheTreeItIsGiven(t *testing.T) {
	t.Parallel()
	benched, err := mutated(t, "--bench")
	if err != nil || !strings.Contains(benched, "  . ") ||
		!strings.Contains(benched, "bench: at most about") {
		t.Errorf("a bench of the fixture: %v\n%s", err, benched)
	}
	said, err := mutated(t)
	if err == nil || !strings.Contains(err.Error(), "survived") {
		t.Errorf("a run over a tree with a survivor: %v", err)
	}
	for _, want := range []string{"survived: tiny.go:",
		"killed ", "would not build "} {
		if !strings.Contains(said, want) {
			t.Errorf("want %q in:\n%s", want, said)
		}
	}
	if strings.Count(said, "mutants: ") != count.One {
		t.Errorf("the tally was told more than once:\n%s", said)
	}
}
