package mutants_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

// settling is what a run handed over when it settled: the report, and what
// was in the temp folder at that moment.
type settling struct {
	mu      sync.Mutex
	calls   int
	report  mutants.Report
	inPlace []string
	place   string
}

// settle notes the report and looks in the temp folder.
func (note *settling) settle(report mutants.Report) {
	note.mu.Lock()
	defer note.mu.Unlock()
	note.calls++
	note.report = report
	entries, _ := os.ReadDir(note.place)
	for _, each := range entries {
		note.inPlace = append(note.inPlace, each.Name())
	}
}

// placed points the temp folder at a folder of the test's own and gives
// a note that looks in it. it sets TMPDIR, so a test that uses it runs
// alone.
func placed(t *testing.T) *settling {
	t.Helper()
	place := t.TempDir()
	t.Setenv("TMPDIR", place)
	return &settling{place: place}
}

// the run hands over its report while the copies of the tree are still
// there. it has removed them when it returns. the report is the whole
// run's.
func TestTheReportIsSettledBeforeTheCopiesGo(t *testing.T) {
	note := placed(t)
	root := fixture(t, "")
	report, err := runBefore(t, context.Background(), root,
		quick(t).Settling(note.settle))
	if err != nil {
		t.Fatal(err)
	}
	if note.calls != count.One {
		t.Fatalf("settled %d times, want once", note.calls)
	}
	if len(note.inPlace) != count.One ||
		!strings.HasPrefix(note.inPlace[count.First], "game-mutants-") {
		t.Errorf("at settling the temp folder held %v, want the copies",
			note.inPlace)
	}
	settled, returned := note.report.Outcomes(), report.Outcomes()
	if len(settled) != len(returned) || len(settled) != len(tinyNames) {
		t.Errorf("settled %d outcomes, returned %d, want %d",
			len(settled), len(returned), len(tinyNames))
	}
	left, _ := os.ReadDir(note.place)
	if len(left) != count.Nothing {
		t.Errorf("the run returned with %d entries left", len(left))
	}
}

// benches have no outcomes to settle, so they settle nothing.
func TestABenchSettlesNothing(t *testing.T) {
	note := placed(t)
	root := fixture(t, "")
	opts := quick(t).Settling(note.settle)
	if _, err := benchedWith(t, root, opts); err != nil {
		t.Fatal(err)
	}
	if note.calls != count.Nothing {
		t.Errorf("a bench settled %d times", note.calls)
	}
}

// benchedWith is a bench of the root with the options given.
func benchedWith(t *testing.T, root string, opts mutants.Options) (
	mutants.Report, error) {
	t.Helper()
	return runBefore(t, context.Background(), root, opts.Benchmarking())
}

// cancelled before it began the run settles a report in which every
// mutant is stopped. its tree is not copied. what stops it is not a tree
// that cannot be copied.
func TestACancelledRunSettlesWhatItHasAndCopiesNothing(t *testing.T) {
	note := placed(t)
	root := fixture(t, "")
	unreadable(t, root, "secret.txt")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := runBefore(t, ctx, root, quick(t).Settling(note.settle))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled run returned %v", err)
	}
	broken := report.Broken()
	if count.Positive(len(broken)) {
		t.Errorf("a stopped run is broken: %q", broken)
	}
	settled := countsOf(note.report)
	if note.calls != count.One || settled["stopped"] != len(tinyNames) {
		t.Errorf("settled %d times with %v", note.calls, settled)
	}
}

// cancelled before it began the bench has run nothing and holds no
// outcomes. it is stopped. the tree it did not copy does not break it.
func TestACancelledBenchHoldsNothing(t *testing.T) {
	placed(t)
	root := fixture(t, "")
	unreadable(t, root, "secret.txt")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := runBefore(t, ctx, root, quick(t).Benchmarking())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled bench returned %v", err)
	}
	broken, outcomes := report.Broken(), report.Outcomes()
	if count.Positive(len(broken)) || count.Positive(len(outcomes)) {
		t.Errorf("broken %q, outcomes %d", broken, len(outcomes))
	}
}

// cancelOnTheFirstRun cancels once go test has run, which is the unchanged
// suite.
func cancelOnTheFirstRun(stub goStub, cancel context.CancelFunc) {
	for stub.calls() < count.One {
		time.Sleep(pollEvery)
	}
	cancel()
}

// cancelled while the unchanged suite runs the run has tried no mutant.
// each is stopped and none is broken. the report is still settled.
func TestARunCancelledInTheUnchangedSuiteStopsEveryMutant(t *testing.T) {
	note := placed(t)
	stub := stubGo(t)
	root := fixture(t, "")
	stub.pause(t, count.One, longPause)
	ctx, cancel := context.WithCancel(context.Background())
	go cancelOnTheFirstRun(stub, cancel)
	report, err := runBefore(t, ctx, root, quick(t).Settling(note.settle))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled run returned %v", err)
	}
	counts, broken := countsOf(report), report.Broken()
	if counts["stopped"] != len(tinyNames) || count.Positive(len(broken)) ||
		note.calls != count.One {
		t.Errorf("stopped run: %v broken %q settled %d", counts, broken,
			note.calls)
	}
}

// cancelled before it began, the run copies nothing even where the tree
// would copy whole: the copies are gone when the report is settled.
func TestACancelledRunStopsTheCopyOfATreeThatWouldCopy(t *testing.T) {
	note := placed(t)
	root := fixture(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runBefore(t, ctx, root, quick(t).Settling(note.settle))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled run returned %v", err)
	}
	if count.Positive(len(note.inPlace)) {
		t.Errorf("a cancelled run left copies at its settling: %q", note.inPlace)
	}
}
