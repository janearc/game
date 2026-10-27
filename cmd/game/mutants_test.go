package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"go.uber.org/mock/gomock"
)

// benchOf is a bench of these suites with these broken packages and stale
// lines. it says the run would take asked at the workers given. it says
// later at four times as many.
func benchOf(ctrl *gomock.Controller, suites []suiteCost, broken,
	stale []string, workers int, asked, later time.Duration) *Mockfindings {
	found := NewMockfindings(ctrl)
	found.EXPECT().Suites().Return(suites)
	found.EXPECT().Broken().Return(broken)
	found.EXPECT().Stale().Return(stale)
	found.EXPECT().Estimate(workers).Return(asked)
	found.EXPECT().Estimate(fourTimes * workers).Return(later)
	return found
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

// aBench is the one suite a bench of these tests times, and what it
// takes to find it again.
type aBench struct {
	ctrl    *gomock.Controller
	suites  []suiteCost
	workers int
	took    time.Duration
}

// found is the bench with these broken packages and stale lines.
func (bench aBench) found(broken, stale []string) *Mockfindings {
	return benchOf(bench.ctrl, bench.suites, broken, stale,
		bench.workers, bench.took, bench.took)
}

// the bench fails only for what could not be run. that is a broken package
// or a stale line or an interrupt. the sound bench does not fail.
func TestBenchFailsOnlyForWhatCouldNotRun(t *testing.T) {
	t.Parallel()
	slow := secondsHeld(t, "bench-slow-seconds")
	workers := int(held(t, "bench-workers"))
	bench := aBench{
		ctrl: gomock.NewController(t), workers: workers, took: slow,
		suites: []suiteCost{{dir: "a", took: slow, alone: slow,
			mutants: count.One}},
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
	ctrl := gomock.NewController(t)
	var out strings.Builder
	found := benchOf(ctrl, nil, []string{"c: no"}, []string{"old"},
		count.One, time.Second, time.Second)
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
	states := []struct{ state, at string }{
		{"killed", "a.go:1 a.go f: a => b"},
		{"survived", "b.go:2 b.go f: a => b"},
		{"timed out", "c.go:3 c.go f: a => b"},
		{"broken", "d.go:4 d.go f: a => b"},
		{"equivalent", "e.go:5 e.go f: a => b"},
		{"unbuildable", "f.go:6 f.go f: a => b"},
		{"stopped", "g.go:7 g.go f: a => b"},
	}
	var outcomes []finding
	for _, each := range states {
		_, name, _ := strings.Cut(each.at, " ")
		outcomes = append(outcomes, finding{state: each.state,
			name: name, at: each.at, failing: "why"})
	}
	found := NewMockfindings(gomock.NewController(t))
	found.EXPECT().Untested().Return([]string{"cmd/x"})
	found.EXPECT().Broken().Return([]string{"p: no"})
	found.EXPECT().Stale().Return([]string{"old"})
	found.EXPECT().Outcomes().Return(outcomes)
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
	found := NewMockfindings(gomock.NewController(t))
	found.EXPECT().Untested().Return(nil)
	found.EXPECT().Broken().Return(nil)
	found.EXPECT().Stale().Return(nil)
	found.EXPECT().Outcomes().Return([]finding{
		{state: "killed", name: "n", at: "a.go:1 n"},
		{state: "equivalent", name: "m", at: "a.go:2 m"},
	})
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

// checkBenched holds a bench of the fixture to what the report timed. that
// is one suite and its cost alone. it is the estimate over one worker.
func checkBenched(t *testing.T, benched findings) {
	t.Helper()
	suites := benched.Suites()
	if len(suites) != count.One {
		t.Fatalf("suites %+v", suites)
	}
	suite := suites[count.First]
	many := time.Duration(suite.mutants)
	if suite.mutants < count.One || suite.alone != suite.took*many {
		t.Errorf("suite %+v", suite)
	}
	if got := benched.Estimate(count.One); got != suite.alone {
		t.Errorf("estimate %s for %+v", got, suite)
	}
}

// checkRan holds a run of the fixture to what the report found. each
// mutant is placed in tiny.go and named. the held function is killed. the
// unheld one survives. nothing else is wrong with the tree.
func checkRan(t *testing.T, ran findings) {
	t.Helper()
	states := map[string]bool{}
	outcomes := ran.Outcomes()
	for _, each := range outcomes {
		states[each.state] = true
		if !strings.Contains(each.at, "tiny.go:") ||
			!strings.HasSuffix(each.at, each.name) ||
			!strings.HasPrefix(each.name, "tiny.go ") {
			t.Errorf("a finding placed wrongly: %+v", each)
		}
	}
	if !states["killed"] || !states["survived"] {
		t.Errorf("the fixture has a held and an unheld function: %v",
			states)
	}
	lines := ran.Lines()
	if len(lines) != len(outcomes) {
		t.Errorf("%d lines for %d outcomes", len(lines), len(outcomes))
	}
	if ran.Untested() != nil || ran.Broken() != nil || ran.Stale() != nil {
		t.Errorf("untested %v broken %v stale %v", ran.Untested(),
			ran.Broken(), ran.Stale())
	}
}

// diskMiner runs the real mutants over a real tree. the findings it gives
// are the report's. the bench times the fixture's suite. the run names and
// places its mutants and sorts them into states.
func TestDiskMinerGivesTheReportsFindings(t *testing.T) {
	t.Parallel()
	tiny := tinyTree(t)
	opts, err := mutantOptions("", "", twoWorkers, suiteTimeout)
	if err != nil {
		t.Fatal(err)
	}
	var run miner = diskMiner{}
	note := &settledNote{}
	benched, err := run.Run(context.Background(), tiny, opts.Benchmarking(),
		note.settle, note.decide)
	if err != nil {
		t.Fatal(err)
	}
	checkBenched(t, benched)
	if note.calls != count.Nothing {
		t.Errorf("a bench settled %d times", note.calls)
	}
	ran, err := run.Run(context.Background(), tiny, opts, note.settle,
		note.decide)
	if err != nil {
		t.Fatal(err)
	}
	checkRan(t, ran)
	checkSaid(t, ran, note.said)
	if note.calls != count.One || len(note.found.Lines()) != len(ran.Lines()) {
		t.Errorf("the run settled %d times", note.calls)
	}
}

// settledNote notes what a miner settled with, and how often.
type settledNote struct {
	found findings
	calls int
	said  []string
}

// decide notes a line the miner said as it decided a mutant.
func (note *settledNote) decide(line string) {
	note.said = append(note.said, line)
}

// settle notes the findings the miner settled with.
func (note *settledNote) settle(found findings) {
	note.found = found
	note.calls++
}

// checkSaid holds the lines said to the report lines of the survivors.
func checkSaid(t *testing.T, ran findings, said []string) {
	t.Helper()
	var want []string
	for place, each := range ran.Outcomes() {
		if each.state == "survived" || each.state == "timed out" {
			want = append(want, ran.Lines()[place])
		}
	}
	if len(want) == count.Nothing {
		t.Fatal("the fixture has a survivor to say")
	}
	slices.Sort(want)
	slices.Sort(said)
	if !slices.Equal(said, want) {
		t.Errorf("said\n%q\nwant\n%q", said, want)
	}
}
