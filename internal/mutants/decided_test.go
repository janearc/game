package mutants_test

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

// decidedLines is the lines a run said as it decided mutants, kept under
// a lock.
type decidedLines struct {
	lock  sync.Mutex
	lines []string
}

// say keeps one line.
func (got *decidedLines) say(line string) {
	got.lock.Lock()
	defer got.lock.Unlock()
	got.lines = append(got.lines, line)
}

// the stub answers the three mutants of one function in turn.
const (
	callSurvives = 2 // defined here: the call after the unchanged suite
	callTimesOut = 3 // defined here: the call after that
	callIsKilled = 4 // defined here: the last of the three
)

// answerOneEach readies the stub and the tree for those three.
func answerOneEach(t *testing.T) string {
	t.Helper()
	stub := stubGo(t)
	root := withATest(t, "many.go", manyFunctions(count.One))
	stub.answer(t, count.One, "ok\n", count.Nothing)
	stub.answer(t, callSurvives, "ok\n", count.Nothing)
	stub.answer(t, callTimesOut,
		"panic: test timed out after 20s\n", count.One)
	stub.answer(t, callIsKilled,
		"--- FAIL: TestX (0.00s)\nFAIL\n", count.One)
	return root
}

// TestARunSaysEachSurvivorAndTimeoutAsItIsDecided: --again reads them.
func TestARunSaysEachSurvivorAndTimeoutAsItIsDecided(t *testing.T) {
	root := answerOneEach(t)
	got := &decidedLines{}
	files, _ := disk(t)
	opts := askFor(count.One, byTheSecond).Deciding(got.say)
	report := runWith(t, root, opts, files)
	var want []string
	for _, made := range report.Outcomes() {
		if made.State() == "survived" || made.State() == "timed out" {
			want = append(want, reportLineOf(report, made))
		}
	}
	if len(want) != count.Next(count.One) {
		t.Fatalf("fixture gave %d to say, want 2: %q", len(want),
			report.Lines())
	}
	if !slices.Equal(got.lines, want) {
		t.Errorf("said\n%q\nwant\n%q", got.lines, want)
	}
	again := mutants.ParseAgain(strings.Join(got.lines, "\n"))
	if len(again) != len(want) {
		t.Errorf("--again reads %d of the %d lines said", len(again),
			len(want))
	}
}

// reportLineOf is the line of the report that holds this outcome.
func reportLineOf(report mutants.Report, made mutants.Outcome) string {
	for place, each := range report.Outcomes() {
		if each.Mutant().Name() == made.Mutant().Name() {
			return report.Lines()[place]
		}
	}
	return ""
}

// TestARunSaysNothingOfKilledMutants: all killed gives no line.
func TestARunSaysNothingOfKilledMutants(t *testing.T) {
	stub := stubGo(t)
	root := withATest(t, "many.go", manyFunctions(count.One))
	stub.answer(t, count.One, "ok\n", count.Nothing)
	for call := range count.Next(count.Next(count.One)) {
		stub.answer(t, call+count.Next(count.One),
			"--- FAIL: TestX (0.00s)\nFAIL\n", count.One)
	}
	got := &decidedLines{}
	files, _ := disk(t)
	opts := askFor(count.One, byTheSecond).Deciding(got.say)
	report := runWith(t, root, opts, files)
	if countsOf(report)["killed"] != count.Next(count.Next(count.One)) {
		t.Fatalf("fixture: %q", report.Lines())
	}
	if len(got.lines) != count.Nothing {
		t.Errorf("a clean run said %q", got.lines)
	}
}

// the run's own progress lines are not what is said of decisions.
func TestDecidedLinesAreNotProgressLines(t *testing.T) {
	root := answerOneEach(t)
	got, said := &decidedLines{}, &heard{}
	files, _ := disk(t)
	opts := askFor(count.One, byTheSecond).Deciding(got.say).
		Telling(said.tell)
	runWith(t, root, opts, files)
	for _, line := range got.lines {
		if slices.Contains(said.lines, line) {
			t.Errorf("%q went to both", line)
		}
	}
}
