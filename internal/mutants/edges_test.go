package mutants_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

// rootUID is the superuser's user id, as unix has defined it.
const rootUID = 0

// heard is what a run told the person, one sentence a line, kept under
// a lock.
type heard struct {
	lock  sync.Mutex
	lines []string
}

// tell keeps one line.
func (said *heard) tell(line string) {
	said.lock.Lock()
	defer said.lock.Unlock()
	said.lines = append(said.lines, line)
}

// shutPath is a path shut until the test ends, and the mode that opens
// it again.
type shutPath struct {
	path string
	open os.FileMode
}

// reopen opens the path again, so the tree can be removed.
func (shut shutPath) reopen() {
	_ = os.Chmod(shut.path, shut.open)
}

// edgeSource has what Of must pass over: an integer and a float too big
// to read, and a call that is neither Min nor Max; and what it must show
// rightly: a literal in a loop's head and one in its body, a send in an
// if's body, a range's head and body, a loop in a function literal, a
// case around knows nothing of after an array it does not enter, a
// two-digit sum and a float with a fraction.
const edgeSource = `package edge

import "strings"

const Huge = 0x1FFFFFFFFFFFFFFFF

const Vast = 1e400

var Rate = 0.25

func Count(ch chan int, s string, n int) int {
	for i := 0; i < 10; i++ {
		ch <- 1
	}
	if n > 0 {
		ch <- 3
	}
	for range n * 2 {
		ch <- 7
	}
	_ = func() {
		for n < 9 {
		}
	}
	var grid [4]int
	_ = grid
	switch n {
	case 5:
	}
	return len(strings.TrimSpace(s))
}
`

// edgeNames is every change Of makes to edgeSource.
var edgeNames = []string{
	"edge.go package: Rate = 0.25 => Rate = 1.5",
	"edge.go Count: i := 0 => i := 1",
	"edge.go Count: i < 10 => i <= 10",
	"edge.go Count: i < 10 => i >= 10",
	"edge.go Count: i < 10 => i < 11",
	"edge.go Count: i++ => i--",
	"edge.go Count: ch <- 1 => ch <- 2",
	"edge.go Count: (n > 0) => (!(n > 0))",
	"edge.go Count: if n > 0 { ch <- 3 } => if n > 0 { }",
	"edge.go Count: n > 0 => n >= 0",
	"edge.go Count: n > 0 => n <= 0",
	"edge.go Count: n > 0 => n > 1",
	"edge.go Count: ch <- 3 => ch <- 4",
	"edge.go Count: n * 2 => n / 2",
	"edge.go Count: n * 2 => n * 3",
	"edge.go Count: ch <- 7 => ch <- 8",
	"edge.go Count: n < 9 => n <= 9",
	"edge.go Count: n < 9 => n >= 9",
	"edge.go Count: n < 9 => n < 10",
	"edge.go Count: 5 => 6",
}

// unreadable writes a file its owner cannot read. root reads anything,
// so a test that needs one is skipped there.
func unreadable(t *testing.T, root, rel string) {
	t.Helper()
	if os.Geteuid() == rootUID {
		t.Skip("root reads every file")
	}
	write(t, root, rel, "kept from you\n")
	path := filepath.Join(root, rel)
	if err := os.Chmod(path, count.Nothing); err != nil {
		t.Fatal(err)
	}
	open := os.FileMode(0o644)
	t.Cleanup(shutPath{path: path, open: open}.reopen)
}

func TestRunStopsAtWhatItCannotRead(t *testing.T) {
	t.Parallel()
	gone := filepath.Join(t.TempDir(), "gone")
	if _, err := runBefore(t, context.Background(), gone, quick(t)); err == nil ||
		!strings.Contains(err.Error(), gone) {
		t.Errorf("a root that is not there: %v", err)
	}
	// with the run cancelled, only a .mutants it cannot read is an error
	// that is not the cancel
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	root := fixture(t, "no reason given\n")
	if _, err := runBefore(t, stopped, root, quick(t)); err == nil ||
		!strings.Contains(err.Error(), ".mutants:1: ") {
		t.Errorf("a .mutants without reasons: %v", err)
	}
	// go list reads only a file's head, so a body that does not parse is
	// found when the mutants are made
	root = fixture(t, "")
	write(t, root, "torn/torn.go", "package torn\n\nfunc F() {\n")
	write(t, root, "torn/torn_test.go", "package torn\n")
	if _, err := runBefore(t, stopped, root, quick(t)); err == nil ||
		!strings.Contains(err.Error(), "torn.go") {
		t.Errorf("a source that does not parse: %v", err)
	}
}

func TestRunStopsAtADirectoryItCannotEnter(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == rootUID {
		t.Skip("root enters every directory")
	}
	root := fixture(t, "")
	shut := filepath.Join(root, "shut")
	if err := os.Mkdir(shut, count.Nothing); err != nil {
		t.Fatal(err)
	}
	openDir := os.FileMode(0o755)
	t.Cleanup(shutPath{path: shut, open: openDir}.reopen)
	if _, err := runBefore(t, context.Background(), root,
		quick(t)); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("a directory it cannot enter: %v", err)
	}
}

func TestATreeThatWillNotCopyIsBroken(t *testing.T) {
	t.Parallel()
	root := fixture(t, "")
	unreadable(t, root, "secret.txt")
	report, err := runBefore(t, context.Background(), root, quick(t))
	if err != nil {
		t.Fatal(err)
	}
	broken := report.Broken()
	outcomes := report.Outcomes()
	if len(broken) != count.One ||
		!strings.HasPrefix(broken[count.First], "copying the tree: ") ||
		count.Positive(len(outcomes)) {
		t.Errorf("broken %q, %d outcomes", broken, len(outcomes))
	}
}

// said is what a run told the person, one sentence a line.
func said(t *testing.T, root string, opts mutants.Options) string {
	t.Helper()
	heard := &heard{}
	opts = opts.Telling(heard.tell)
	if _, err := runBefore(t, context.Background(), root, opts); err != nil {
		t.Fatal(err)
	}
	return strings.Join(heard.lines, "\n")
}

func TestTheEstimateIsSaidForWhatWillRun(t *testing.T) {
	t.Parallel()
	bench := quick(t).Benchmarking()
	one := bench.OnlyWhere(regexp.MustCompile(`:Hello$`))
	if heard := said(t, fixture(t, ""), one); !strings.Contains(heard,
		"estimate: at most about") {
		t.Errorf("one mutant to run, and no estimate:\n%s", heard)
	}
	root := fixture(t, "")
	write(t, root, "tiny_test.go", "package tiny\n\nimport \"testing\"\n\n"+
		"func TestNo(t *testing.T) { t.Fatal(\"no\") }\n")
	if heard := said(t, root, bench); strings.Contains(heard, "estimate:") {
		t.Errorf("nothing to run, and an estimate:\n%s", heard)
	}
}

// twoPackages is a module of two packages, each with a test that holds
// nothing, so a bench of it times two suites with mutants each.
func twoPackages(t *testing.T) string {
	t.Helper()
	nothing := "\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n"
	return moduleOf(t, map[string]string{
		"a/a.go":      "package a\n\nfunc F(x int) int { return x + 1 }\n",
		"a/a_test.go": "package a\n" + nothing,
		"b/b.go": "package b\n\nfunc G(x, y int) int {\n" +
			"\tif x < y {\n\t\treturn x\n\t}\n\treturn y\n}\n",
		"b/b_test.go": "package b\n" + nothing,
	})
}

// a bench times each package's suite, by the package's directory, with
// the mutants that wait on it.
func TestABenchTimesEachSuiteByItsDirectory(t *testing.T) {
	t.Parallel()
	report, err := runBefore(t, context.Background(), twoPackages(t),
		quick(t).Benchmarking())
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]int{}
	suites := report.Suites()
	for _, suite := range suites {
		dirs[suite.Dir()] = suite.Mutants()
		took := int(suite.Took())
		many := time.Duration(suite.Mutants())
		if !count.Positive(suite.Mutants()) || !count.Positive(took) ||
			suite.Alone() != suite.Took()*many {
			t.Errorf("suite %s: %d mutants, took %s, alone %s",
				suite.Dir(), suite.Mutants(), suite.Took(), suite.Alone())
		}
	}
	two := count.Next(count.One)
	if len(dirs) != len(suites) || len(dirs) != two ||
		!count.Positive(dirs["a"]) || !count.Positive(dirs["b"]) ||
		dirs["b"] == dirs["a"] {
		t.Errorf("suites by directory: %v", dirs)
	}
}

// the estimate is every mutant's suite one after another, over the
// workers, and no workers is one worker.
func TestEstimateIsEveryMutantsSuiteOverTheWorkers(t *testing.T) {
	t.Parallel()
	report, err := runBefore(t, context.Background(), twoPackages(t),
		quick(t).Benchmarking())
	if err != nil {
		t.Fatal(err)
	}
	var serial time.Duration
	for _, suite := range report.Suites() {
		serial += suite.Alone()
	}
	if !count.Positive(int(serial)) {
		t.Fatalf("nothing to estimate: %+v", report.Suites())
	}
	for workers, want := range map[int]time.Duration{
		count.Nothing:         serial,
		count.One:             serial,
		count.Next(count.One): serial / time.Duration(count.Next(count.One)),
	} {
		if got := report.Estimate(workers); got != want {
			t.Errorf("at %d workers: %s, want %s", workers, got, want)
		}
	}
}
