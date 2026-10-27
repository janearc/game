package mutants_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

// the suite times and timeouts of the short-timeout tests, defined here.
const (
	suiteTook    = 10 * time.Second       // defined here: what a scripted suite takes
	dearSuite    = 14 * time.Second       // defined here: what the dearer of two suites takes
	oneJustShort = 19 * time.Second       // defined here: a second under twice suiteTook
	justEnough   = 20 * time.Second       // defined here: twice suiteTook
	plenty       = 21 * time.Second       // defined here: a second over twice suiteTook
	longEnough   = 28 * time.Second       // defined here: twice dearSuite
	justUnder    = 27 * time.Second       // defined here: a second under twice dearSuite
	aFifth       = 200 * time.Millisecond // defined here: a fifth of a second over, to round up
	aHalf        = 500 * time.Millisecond // defined here: half a second over, to be twice exactly
)

// scripted is a clock whose first reading after the unchanged suite
// began is that long on: the suite took that long.
func scripted(took ...time.Duration) *ticking {
	return &ticking{at: time.Unix(count.Nothing, count.Nothing), steps: took,
		rest: time.Millisecond}
}

// runWithSuite runs the fixture with stub go and a clock under which the
// unchanged suite took that long, and the timeout given.
func runWithSuite(t *testing.T, took, timeout time.Duration,
	stub goStub) (mutants.Report, error) {
	t.Helper()
	root := fixture(t, "")
	stub.answer(t, count.One, "ok\n", count.Nothing)
	files, _ := disk(t)
	opts := askFor(count.One, timeout).Nowed(scripted(took).now).Writing(files)
	return runBefore(t, context.Background(), root, opts)
}

// timeouts shorter than twice the unchanged suite's time are refused
// before any mutant runs. the error says what the suite took and what the
// timeout is. it says the timeout to use.
func TestATimeoutShorterThanTheSuiteIsRefused(t *testing.T) {
	stub := stubGo(t)
	report, err := runWithSuite(t, suiteTook, oneJustShort, stub)
	if err == nil {
		t.Fatal("a timeout under twice the suite's time was not refused")
	}
	for _, want := range []string{
		"the unchanged suite of . took 10.0s",
		"--timeout is 19.0s",
		"use --timeout 20s or more",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal lacks %q: %v", want, err)
		}
	}
	judged := report.Outcomes()
	if stub.calls() != count.One || count.Positive(len(judged)) {
		t.Errorf("go test ran %d times and %d mutants were judged after a refusal",
			stub.calls(), len(judged))
	}
}

// timeouts of twice the suite's time or more start. twice is enough.
func TestATimeoutOfTwiceTheSuiteOrMoreStarts(t *testing.T) {
	for _, timeout := range []time.Duration{justEnough, plenty} {
		stub := stubGo(t)
		report, err := runWithSuite(t, suiteTook, timeout, stub)
		if err != nil {
			t.Errorf("a timeout of %s was refused: %v", timeout, err)
		}
		judged := report.Outcomes()
		if len(judged) != len(tinyNames) {
			t.Errorf("a timeout of %s judged %d mutants", timeout,
				len(judged))
		}
	}
}

// benches time the suites to learn what timeout to give. they are not
// refused for the timeout they were given.
func TestABenchIsNotRefusedForItsTimeout(t *testing.T) {
	stub := stubGo(t)
	root := fixture(t, "")
	stub.answer(t, count.One, "ok\n", count.Nothing)
	files, _ := disk(t)
	opts := askFor(count.One, time.Second).
		Nowed(scripted(suiteTook).now).Writing(files).Benchmarking()
	report, err := runBefore(t, context.Background(), root, opts)
	suites := report.Suites()
	if err != nil || len(suites) != count.One {
		t.Errorf("a bench with a short timeout: %v, suites %d", err,
			len(suites))
	}
}

// with two suites the dearer decides. the refusal names it.
func TestTheDearerSuiteDecidesAndIsNamed(t *testing.T) {
	stub := stubGo(t)
	root := moduleOf(t, map[string]string{
		"a/a.go": "package a\n\nfunc F() int { return 1 + 1 }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\n" +
			"func TestNothing(t *testing.T) {}\n",
		"b/b.go": "package b\n\nfunc F() int { return 1 + 1 }\n",
		"b/b_test.go": "package b\n\nimport \"testing\"\n\n" +
			"func TestNothing(t *testing.T) {}\n",
	})
	stub.answer(t, count.One, "ok\n", count.Nothing)
	stub.answer(t, count.Next(count.One), "ok\n", count.Nothing)
	files, _ := disk(t)
	clock := scripted(suiteTook, count.Nothing, dearSuite)
	opts := askFor(count.One, justUnder).Nowed(clock.now).Writing(files)
	_, err := runBefore(t, context.Background(), root, opts)
	if err == nil || !strings.Contains(err.Error(), "suite of b took 14.0s") ||
		!strings.Contains(err.Error(), "use --timeout 28s or more") {
		t.Errorf("the dearer suite was not named: %v", err)
	}
	stub = stubGo(t)
	stub.answer(t, count.One, "ok\n", count.Nothing)
	stub.answer(t, count.Next(count.One), "ok\n", count.Nothing)
	clock = scripted(suiteTook, count.Nothing, dearSuite)
	opts = askFor(count.One, longEnough).Nowed(clock.now).Writing(files)
	if _, err := runBefore(t, context.Background(), root, opts); err != nil {
		t.Errorf("twice the dearer suite was refused: %v", err)
	}
}

// the timeout to use is said in whole seconds and is never less than twice
// the suite's time: a suite of ten and a fifth seconds wants twenty-one.
func TestTheTimeoutToUseIsRoundedUp(t *testing.T) {
	stub := stubGo(t)
	took := suiteTook + aFifth
	_, err := runWithSuite(t, took, justEnough, stub)
	if err == nil || !strings.Contains(err.Error(), "use --timeout 21s or more") {
		t.Errorf("a suite of %s with %s: %v", took, justEnough, err)
	}
	stub = stubGo(t)
	_, err = runWithSuite(t, suiteTook+aHalf, plenty, stub)
	if err != nil {
		t.Errorf("twice a suite of ten and a half seconds is %s: %v", plenty, err)
	}
}

// with two suites that took as long, the first is the one named: a tie
// does not move the refusal to the later suite.
func TestOfTwoSuitesThatTookAsLongTheFirstIsNamed(t *testing.T) {
	stub := stubGo(t)
	root := moduleOf(t, map[string]string{
		"a/a.go": "package a\n\nfunc F() int { return 1 + 1 }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\n" +
			"func TestNothing(t *testing.T) {}\n",
		"b/b.go": "package b\n\nfunc F() int { return 1 + 1 }\n",
		"b/b_test.go": "package b\n\nimport \"testing\"\n\n" +
			"func TestNothing(t *testing.T) {}\n",
	})
	stub.answer(t, count.One, "ok\n", count.Nothing)
	stub.answer(t, count.Next(count.One), "ok\n", count.Nothing)
	files, _ := disk(t)
	clock := scripted(suiteTook, count.Nothing, suiteTook)
	opts := askFor(count.One, oneJustShort).Nowed(clock.now).Writing(files)
	_, err := runBefore(t, context.Background(), root, opts)
	if err == nil || !strings.Contains(err.Error(), "suite of a took 10.0s") {
		t.Errorf("the first of two equal suites was not named: %v", err)
	}
}
