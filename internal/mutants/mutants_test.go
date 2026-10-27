package mutants_test

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

// tiny is the fixture module: Abs, held by TestAbs; Twice, held by
// nothing; Hello, whose one mutant does not compile.
var tiny = filepath.Join("testdata", "tiny")

// tinyNames is every change Of makes to tiny.go, in the order made.
var tinyNames = []string{
	"tiny.go Abs: (x < 0) => (!(x < 0))",
	"tiny.go Abs: if x < 0 { return -x } => if x < 0 { }",
	"tiny.go Abs: x < 0 => x <= 0",
	"tiny.go Abs: x < 0 => x >= 0",
	"tiny.go Abs: x < 0 => x < 1",
	"tiny.go Abs: return -x => return +x",
	"tiny.go Twice: return x * 2 => return x / 2",
	"tiny.go Twice: return x * 2 => return x * 3",
	`tiny.go Hello: return "hi " + name => return "hi " - name`,
}

// twiceSurvivors is tinyNames' Twice entries: the mutants nothing
// holds, which survive.
func twiceSurvivors() []string {
	var survived []string
	for _, name := range tinyNames {
		if strings.Contains(name, "Twice") {
			survived = append(survived, name)
		}
	}
	return survived
}

// the two changes to Abs that make no difference: minus zero is zero,
// and for integers x < 1 is x <= 0. the third line names a change the
// source does not have, which the run must report as stale.
const tinyEquivalents = `# the changes that make no difference
tiny.go Abs: x < 0 => x <= 0 :: minus zero is zero
tiny.go Abs: x < 0 => x < 1 :: for integers, x < 1 is x <= 0
tiny.go Abs: x < 0 => x > 0 :: not in the source any more
`

// fixSource has one of each construct the run changes, and an array
// length it must leave alone.
const fixSource = `package fix

import "math"

// Limit is a bound.
const Limit = 3

var Scale = 2.5

func Sum(xs []int) (total int) {
	for i := 0; i < len(xs); i++ {
		total += xs[i]
	}
	for range xs {
		total++
	}
	var grid [4]int
	_ = grid
	return total
}

func Least(a, b float64) float64 { return math.Min(a, b) }
`

// fixNames is every change Of makes to fixSource.
var fixNames = []string{
	"fix.go package: Limit = 3 => Limit = 4",
	"fix.go package: Scale = 2.5 => Scale = 6",
	"fix.go Sum: i := 0 => i := 1",
	"fix.go Sum: i < len(xs) => i <= len(xs)",
	"fix.go Sum: i < len(xs) => i >= len(xs)",
	"fix.go Sum: i++ => i--",
	"fix.go Sum: total += xs[i] => total -= xs[i]",
	"fix.go Sum: total++ => total--",
	"fix.go Least: return math.Min(a, b) => return math.Max(a, b)",
}

// fixture copies tiny into a root of its own, writes .mutants there when
// given one, and returns the root with its links resolved.
func fixture(t *testing.T, equivalents string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go.mod", "tiny.go", "tiny_test.go"} {
		body, err := os.ReadFile(filepath.Join(tiny, name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, root, name, string(body))
	}
	if equivalents != "" {
		write(t, root, ".mutants", equivalents)
	}
	return root
}

// write puts a file under root, making its directory.
func write(t *testing.T, root, rel, text string) {
	t.Helper()
	path := filepath.Join(root, rel)
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
		t.Fatal(err)
	}
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(path, []byte(text), openFile); err != nil {
		t.Fatal(err)
	}
}

// quick is a run that finishes fast enough for a test.
func quick(t *testing.T) mutants.Options {
	t.Helper()
	workers := int(held(t, "test-workers"))
	timeout := secondsHeld(t, "test-timeout-seconds")
	return mutants.Ask(workers, timeout).Nowed(time.Now).
		QuietAfter(quiet).Writing(mutants.Disk())
}

// quiet is how long a test run may go without a word: more than any test.
const quiet = time.Hour

// checkRun is the first run's report held to the fixture: what it said,
// the suites, and the counts.
func checkRun(
	t *testing.T,
	report mutants.Report,
	said []string,
) {
	t.Helper()
	heard := strings.Join(said, "\n")
	for _, want := range []string{
		"mutants: 9 in 1 package at 2 workers, 30s a mutant;",
		"unchanged: . passes,",
		"estimate: at most about",
	} {
		if !strings.Contains(heard, want) {
			t.Errorf("the run never said %q:\n%s", want, heard)
		}
	}
	workers := int(held(t, "test-workers"))
	suites := report.Suites()
	took := suites[count.First].Took()
	estimate := report.Estimate(workers)
	if len(suites) != count.One ||
		suites[count.First].Mutants() != len(tinyNames) ||
		!count.Positive(int(took)) || !count.Positive(int(estimate)) {
		t.Errorf("suites: %+v", suites)
	}
	want := map[string]int{
		"killed":      int(held(t, "tiny-killed")),
		"equivalent":  int(held(t, "tiny-equivalent")),
		"survived":    int(held(t, "tiny-survived")),
		"unbuildable": int(held(t, "tiny-unbuildable")),
	}
	lines := strings.Join(report.Lines(), "\n")
	if got := countsOf(report); !maps.Equal(got, want) {
		t.Fatalf("counts %v:\n%s", got, lines)
	}
	for _, outcome := range report.Outcomes() {
		switch {
		case outcome.State() == "survived" &&
			functionOf(outcome.Mutant()) != "Twice":
			t.Errorf("survived outside Twice: %s", outcome.Mutant().Name())
		case outcome.State() == "killed" && outcome.Failing() != "TestAbs":
			t.Errorf("%s killed by %q", outcome.Mutant().Name(),
				outcome.Failing())
		case outcome.State() == "unbuildable" &&
			functionOf(outcome.Mutant()) != "Hello":
			t.Errorf("would not build: %s", outcome.Mutant().Name())
		}
	}
}

func TestRunTellsSurvivorsFromEquivalents(t *testing.T) {
	t.Parallel()
	root := fixture(t, tinyEquivalents)
	said := &heard{}
	told := quick(t).Telling(said.tell)
	report, err := runBefore(t, context.Background(), root, told)
	if err != nil {
		t.Fatal(err)
	}
	checkRun(t, report, said.lines)
	untested := report.Untested()
	broken := report.Broken()
	if count.Positive(len(untested)) || count.Positive(len(broken)) {
		t.Errorf("untested %v broken %v", untested, broken)
	}
	lines := strings.Join(report.Lines(), "\n")
	again := mutants.ParseAgain(lines)
	survived := twiceSurvivors()
	if len(again) != len(survived) {
		t.Fatalf("the report reads back as %v", again)
	}
	for _, name := range survived {
		if !again[name] {
			t.Errorf("%s did not survive to read back", name)
		}
	}
	// run again: only the survivors, and the stale check is not made
	only := quick(t).OnlyAgain(again)
	report, err = runBefore(t, context.Background(), root, only)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := report.Outcomes()
	stale := report.Stale()
	if len(outcomes) != len(survived) ||
		countsOf(report)["survived"] != len(survived) ||
		count.Positive(len(stale)) {
		t.Errorf("again: %v stale %v", countsOf(report), stale)
	}
}

// brokenFixture is tiny with an untested package beside it and one
// whose tests always fail, and a .mutants line for each.
func brokenFixture(t *testing.T) string {
	t.Helper()
	badLine := "bad/bad.go F: return x + 1 => return x - 1 :: because\n"
	root := fixture(t, tinyEquivalents+badLine)
	write(t, root, "plain/plain.go", "package plain\n\nvar X = 1\n")
	write(t, root, "bad/bad.go",
		"package bad\n\nfunc F(x int) int { return x + 1 }\n")
	write(t, root, "bad/bad_test.go",
		"package bad\n\nimport \"testing\"\n\n"+
			"func TestBad(t *testing.T) { t.Fatal(\"always\") }\n")
	return root
}

func TestBrokenUntestedAndStale(t *testing.T) {
	t.Parallel()
	root := brokenFixture(t)
	report, err := runBefore(t, context.Background(), root, quick(t))
	if err != nil {
		t.Fatal(err)
	}
	outcomes := report.Outcomes()
	if len(outcomes) != len(tinyNames) {
		lines := strings.Join(report.Lines(), "\n")
		t.Errorf("%d outcomes:\n%s", len(outcomes), lines)
	}
	if strings.Join(report.Untested(), ",") != "plain" {
		t.Errorf("untested: %v", report.Untested())
	}
	broken := report.Broken()
	if len(broken) != count.One ||
		!strings.HasPrefix(broken[count.First], "bad: ") {
		t.Errorf("broken: %v", broken)
	}
	// the broken package's line in .mutants is not stale: its mutant
	// exists, it just could not be run
	if strings.Join(report.Stale(), ",") != "tiny.go Abs: x < 0 => x > 0" {
		t.Errorf("stale: %v", report.Stale())
	}
	// with --only, nothing is stale, and a package left out is not broken
	only := quick(t).OnlyWhere(regexp.MustCompile(`:Twice$`))
	report, err = runBefore(t, context.Background(), root, only)
	if err != nil {
		t.Fatal(err)
	}
	outcomes = report.Outcomes()
	stale := report.Stale()
	broken = report.Broken()
	if len(outcomes) != len(twiceSurvivors()) ||
		count.Positive(len(stale)) || count.Positive(len(broken)) {
		t.Errorf("only Twice: %v stale %v broken %v", countsOf(report),
			stale, broken)
	}
}

func TestBenchStopsAfterTheSuites(t *testing.T) {
	t.Parallel()
	root := fixture(t, "")
	bench := quick(t).Benchmarking()
	report, err := runBefore(t, context.Background(), root, bench)
	if err != nil {
		t.Fatal(err)
	}
	suites := report.Suites()
	outcomes := report.Outcomes()
	if count.Positive(len(outcomes)) ||
		len(suites) != count.One ||
		suites[count.First].Mutants() != len(tinyNames) {
		t.Errorf("bench ran %d, suites %+v", len(outcomes), suites)
	}
}

func TestACancelledRunKeepsWhatItHas(t *testing.T) {
	t.Parallel()
	root := fixture(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := runBefore(t, ctx, root, quick(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled run returned %v", err)
	}
	// every mutant is accounted for as stopped, none as anything else
	broken := report.Broken()
	if countsOf(report)["stopped"] != len(tinyNames) ||
		count.Positive(len(broken)) {
		t.Errorf("a run cancelled before it began found %v broken %v",
			countsOf(report), broken)
	}
}

func TestAboutSaysDurationsPlainly(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"1.5s":   "1.5s",
		"59.96s": "1m00s",
		"59.94s": "59.9s",
		"90s":    "1m30s",
		"3h5m":   "3h05m",
		"1h":     "1h00m",
	}
	for given, want := range cases {
		duration, err := time.ParseDuration(given)
		if err != nil {
			t.Fatal(err)
		}
		if got := mutants.About(duration); got != want {
			t.Errorf("About(%s) = %q, want %q", duration, got, want)
		}
	}
}

// countsOf tallies a report's outcomes by state.
func countsOf(report mutants.Report) map[string]int {
	counts := map[string]int{}
	for _, outcome := range report.Outcomes() {
		counts[outcome.State()]++
	}
	return counts
}

// runQuick is a quick run over the tree at root.
func runQuick(t *testing.T, root string) (mutants.Report, error) {
	t.Helper()
	return runBefore(t, context.Background(), root, quick(t))
}
