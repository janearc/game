package mutants_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

// the sizes of the fixtures, the waits of the stub and what it exits with,
// defined here.
const (
	fiftyFunctions  = 50                     // defined here: functions making 150 mutants
	twentyFunctions = 20                     // defined here: functions making 60 mutants
	mutantsOfFifty  = 150                    // defined here: three changes to each of fifty
	mutantsOfTwenty = 60                     // defined here: three changes to each of twenty
	sixCalls        = 6                      // defined here: go test runs of two functions' mutants
	exitOK          = 0                      // defined by the shell: success
	exitFailed      = 1                      // defined by the shell: failure
	longPause       = "5"                    // defined here: seconds, past any bound
	fiveFunctions   = 5                      // defined here: functions making fifteen mutants
	pollEvery       = 10 * time.Millisecond  // defined here: a poll
	shortTimeout    = 300 * time.Millisecond // defined here: a short bound
	byTheSecond     = 20 * time.Second       // defined here: a long bound
	bound           = 200 * time.Millisecond // defined here: a bound
)

// moduleOf is a module in a root of its own with these files, its links
// resolved.
func moduleOf(t *testing.T, files map[string]string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "go.mod", "module m\n\ngo 1.27\n")
	for name, body := range files {
		write(t, root, name, body)
	}
	return root
}

// writes is what a run wrote, in order, to the files it was given.
type writes struct {
	mu     sync.Mutex
	bodies []string
	paths  []string
	modes  []fs.FileMode
}

// disk is files that note each write and succeed, writing nothing.
func disk(t *testing.T) (*MockFiles, *writes) {
	t.Helper()
	noted := &writes{}
	files := NewMockFiles(gomock.NewController(t))
	owner := os.FileMode(0o644)
	files.EXPECT().Chmod(gomock.Any(), owner).Return(nil).AnyTimes()
	files.EXPECT().WriteFile(gomock.Any(), gomock.Any(), owner).
		DoAndReturn(noted.write).AnyTimes()
	return files, noted
}

// write notes one write.
func (noted *writes) write(path string, body []byte, mode fs.FileMode) error {
	noted.mu.Lock()
	defer noted.mu.Unlock()
	noted.paths = append(noted.paths, path)
	noted.bodies = append(noted.bodies, string(body))
	noted.modes = append(noted.modes, mode)
	return nil
}

// ranNothing is a run of the root that changes no file: every mutant is
// let through to tests that run on the code as it is. one worker, so the
// order is the order the changes were made in.
func ranNothing(t *testing.T, root string) (mutants.Report, *writes) {
	t.Helper()
	files, noted := disk(t)
	opts := askFor(count.One, secondsHeld(t, "test-timeout-seconds")).Writing(files)
	report, err := runBefore(t, context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	return report, noted
}

// outcomeNames is the name of each mutant in the report, one a line.
func outcomeNames(report mutants.Report) string {
	var out []string
	for _, outcome := range report.Outcomes() {
		out = append(out, outcome.Mutant().Name())
	}
	return strings.Join(out, "\n")
}

// withATest is the source in a module with a test that holds nothing, so
// that its package is one the run changes.
func withATest(t *testing.T, name, source string) string {
	t.Helper()
	pkg := regexp.MustCompile(`(?m)^package (\w+)`).FindStringSubmatch(source)
	return moduleOf(t, map[string]string{
		name: source,
		strings.TrimSuffix(name, ".go") + "_test.go": "package " + pkg[count.One] +
			"\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n",
	})
}

func TestARunNamesEveryChange(t *testing.T) {
	quickRoot := fixture(t, "")
	report, noted := ranNothing(t, quickRoot)
	original, err := os.ReadFile(filepath.Join(quickRoot, "tiny.go"))
	if err != nil {
		t.Fatal(err)
	}
	first := int(held(t, "tiny-first-line"))
	last := int(held(t, "tiny-last-line"))
	for _, outcome := range report.Outcomes() {
		mutant := outcome.Mutant()
		if mutant.Line() < first || mutant.Line() > last ||
			mutant.File() != "tiny.go" {
			t.Errorf("%s is placed at %s:%d", mutant.Name(),
				mutant.File(), mutant.Line())
		}
	}
	if got := outcomeNames(report); got != strings.Join(tinyNames, "\n") {
		t.Errorf("mutants of tiny.go:\n%s", got)
	}
	// each change is written, then the original put back
	if len(noted.bodies) != len(tinyNames)*count.Next(count.One) {
		t.Fatalf("%d writes for %d changes", len(noted.bodies), len(tinyNames))
	}
	for place, body := range noted.bodies {
		changed := place%count.Next(count.One) == count.First
		if changed == (body == string(original)) {
			t.Errorf("write %d: a change is not the original, and the "+
				"original put back is", place)
		}
	}
}

func TestARunKnowsEveryConstruct(t *testing.T) {
	report, _ := ranNothing(t, withATest(t, "fix.go", fixSource))
	if got := outcomeNames(report); got != strings.Join(fixNames, "\n") {
		t.Errorf("mutants of fix.go:\n%s", got)
	}
	report, _ = ranNothing(t, withATest(t, "edge.go", edgeSource))
	if got := outcomeNames(report); got != strings.Join(edgeNames, "\n") {
		t.Errorf("mutants of edge.go:\n%s", got)
	}
}

// a method with type parameters is go 1.27's; crush has them, and game
// built on go 1.26 could not parse it, so nothing in the file was
// mutated.
func TestARunReadsGenericMethods(t *testing.T) {
	report, _ := ranNothing(t, withATest(t, "generic.go", "package generic\n\n"+
		"type box struct{ n int }\n\n"+
		"// plus adds v to the box.\n"+
		"func (b box) plus[T ~int](v T) int { return b.n + int(v) }\n"))
	if got := outcomeNames(report); !strings.Contains(got, "b.n - int(v)") {
		t.Errorf("mutants of a generic method:\n%s", got)
	}
}

// a run counts the mutants of the packages that have tests, and names the
// directory of each that has not, and leaves out the directories go
// leaves out.
func TestARunTakesWhatGoListsAndLeavesWhatGoSkips(t *testing.T) {
	laid := map[string]string{
		"sub/go.mod":          "module sub\n\ngo 1.27\n",
		"sub/x.go":            sourceOf("sub"),
		"sub/skip.go":         "//go:build ignore\n\n" + sourceOf("sub"),
		"sub/deep/y.go":       sourceOf("deep"),
		"sub/deep/y_test.go":  "package deep\n\nimport \"testing\"\n\nfunc TestY(t *testing.T) {}\n",
		"sub/testdata/z.go":   sourceOf("z"),
		"sub/.hidden/h.go":    sourceOf("h"),
		"nomod/y.go":          sourceOf("y"),
		"tools/bin/go.mod":    "module bins\n\ngo 1.27\n",
		"tools/bin/b.go":      sourceOf("bins"),
		"tools/bin/b_test.go": "package bins\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n",
		"onlytests/o_test.go": "package onlytests\n",
	}
	// a module go would walk past is walked past, whatever its name: at
	// the root a bin is, and anywhere else it is a module
	for _, dir := range []string{"bin", ".git", ".hidden", "_old", "node_modules",
		"testdata", "vendor"} {
		laid[dir+"/go.mod"] = "module left\n\ngo 1.27\n"
		laid[dir+"/l.go"] = sourceOf("left")
		laid[dir+"/l_test.go"] = "package left\n\nimport \"testing\"\n\nfunc TestL(t *testing.T) {}\n"
	}
	root := moduleOf(t, laid)
	report, _ := ranNothing(t, root)
	var files []string
	seen := map[string]bool{}
	for _, outcome := range report.Outcomes() {
		file := outcome.Mutant().File()
		if !seen[file] {
			seen[file] = true
			files = append(files, file)
		}
	}
	if got := strings.Join(files, ","); got != "sub/deep/y.go,tools/bin/b.go" {
		t.Errorf("files changed: %s", got)
	}
	if got := strings.Join(report.Untested(), ","); got != "nomod,sub" {
		t.Errorf("untested: %s", got)
	}
	broken := report.Broken()
	if count.Positive(len(broken)) {
		t.Errorf("broken: %v", broken)
	}
}

// askFor is a run of that many workers, each mutant given that long, with
// the clock, the quiet and the disk a run is given.
func askFor(workers int, timeout time.Duration) mutants.Options {
	return mutants.Ask(workers, timeout).Nowed(time.Now).
		QuietAfter(quiet).Writing(mutants.Disk())
}

// runWith runs the root with the options and the files given.
func runWith(
	t *testing.T, root string, opts mutants.Options, files mutants.Files,
) mutants.Report {
	t.Helper()
	report, err := runBefore(t, context.Background(), root, opts.Writing(files))
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// runBefore is mutants.Run, which ends the test binary when it has not
// come back in the time a run here is given. a run that never ends would
// hold every test after it, so it runs apart from the test.
func runBefore(t *testing.T, ctx context.Context, root string,
	opts mutants.Options) (mutants.Report, error) {
	t.Helper()
	back := make(chan runCame, count.One)
	go sendRun(ctx, root, opts, back)
	limit := secondsHeld(t, "test-timeout-seconds")
	select {
	case got := <-back:
		return got.report, got.err
	case <-time.After(limit):
		panic("the run never came back")
	}
}

// runCame is what mutants.Run gave back.
type runCame struct {
	report mutants.Report
	err    error
}

// sendRun runs the root and sends back what it gave. a run that never
// ends ends the test binary, since it would hold every test after it, so
// it runs apart from the test.
func sendRun(ctx context.Context, root string, opts mutants.Options,
	back chan<- runCame) {
	report, err := mutants.Run(ctx, root, opts)
	back <- runCame{report: report, err: err}
}

// TestARunNeedsItsCallersClockQuietAndDisk: a run is given what tells the
// time, how long a quiet may be and what changes files, and is refused
// without any of the three.
func TestARunNeedsItsCallersClockQuietAndDisk(t *testing.T) {
	t.Parallel()
	root := fixture(t, "")
	files, _ := disk(t)
	whole := mutants.Ask(count.One, time.Second).Nowed(time.Now).
		QuietAfter(quiet).Writing(files)
	bare := mutants.Ask(count.One, time.Second)
	for name, opts := range map[string]mutants.Options{
		"no clock": bare.QuietAfter(quiet).Writing(files),
		"no quiet": bare.Nowed(time.Now).Writing(files),
		"no disk":  bare.Nowed(time.Now).QuietAfter(quiet),
		"nothing":  bare,
	} {
		if _, err := runBefore(t, context.Background(), root, opts); err == nil {
			t.Errorf("%s: the run went", name)
		}
	}
	if _, err := runBefore(t, context.Background(), root,
		whole.Benchmarking()); err != nil {
		t.Errorf("a run given all three: %v", err)
	}
}

// the .mutants file wants a name and a why on every line it has, and a
// run says which line is not so; a name it holds that the source has not
// is stale.
func TestTheEquivalentsFileWantsNameAndWhy(t *testing.T) {
	t.Parallel()
	root := fixture(t, "# fine\n\na b: c => d :: because\nno why here\n")
	if _, err := benched(t, root); err == nil ||
		!strings.Contains(err.Error(), ".mutants:4") ||
		!strings.Contains(err.Error(), "NAME :: WHY") {
		t.Errorf("a bad line: %v", err)
	}
	for _, half := range []string{"a b: c => d :: \n", " :: because\n"} {
		if _, err := benched(t, fixture(t, half)); err == nil {
			t.Errorf("half a line was taken: %q", half)
		}
	}
	report, err := benched(t, fixture(t, "a b: c => d :: because\n"))
	if err != nil || strings.Join(report.Stale(), ",") != "a b: c => d" {
		t.Errorf("a good line: stale %v, %v", report.Stale(), err)
	}
	claim := fixture(t, "")
	if err := os.Mkdir(filepath.Join(claim, ".mutants"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := benched(t, claim); err == nil {
		t.Error("a .mutants that cannot be read was taken as empty")
	}
}

// goTestSaid is what a go test prints and how it exits, and the state and
// failing tests the run takes from it.
var goTestSaid = []struct {
	output, state, failing string
	exit                   int
	pause                  string
}{
	{"ok  \ttiny\t0.1s\n", "survived", "", exitOK, ""},
	{"# tiny [tiny.test]\n./x.go:3:1: syntax error\nFAIL\ttiny [build failed]\n",
		"unbuildable", "", exitFailed, ""},
	{"--- FAIL: TestAbs (0.00s)\n    tiny_test.go:8: Abs(-3) = -3\nFAIL\n",
		"killed", "TestAbs", exitFailed, ""},
	{"panic: test timed out after 20s\n", "timed out", "", exitFailed, ""},
	{"--- FAIL: TestX (0.00s)\npanic: boom\nFAIL\n",
		"killed", "TestX,(panicked)", exitFailed, ""},
	{"", "timed out", "", exitFailed, longPause},
	{"--- FAIL: TestX (0.00s)\npanic: test timed out after 20s\n",
		"killed", "TestX,(timed out)", exitFailed, ""},
	{"--- FAIL: TestX (0.00s)\nFAIL\n", "killed", "TestX,(timed out)", exitFailed, longPause},
	{"--- FAIL: TestA (0.00s)\n--- FAIL: TestB (0.00s)\nFAIL\n",
		"killed", "TestA,TestB", exitFailed, ""},
	{"FAIL\ttiny\t0.2s\n", "killed", "", exitFailed, ""},
	{"go: cannot find main module\n", "broken",
		"go: cannot find main module", exitFailed, ""},
	{"# p [p.test]\n./x.go:3: undefined: y\n", "broken",
		"./x.go:3: undefined: y", exitFailed, ""},
	{"", "broken", "exit status 1", exitFailed, ""},
}

// what go test said of a mutant, and how it left, becomes a state and
// the tests that failed: the same words in every way go can say them.
func TestWhatGoTestSaidBecomesAState(t *testing.T) {
	stub := stubGo(t)
	root := withATest(t, "many.go", manyFunctions(fiveFunctions))
	cases := goTestSaid
	stub.answer(t, count.One, "ok\n", count.Nothing)
	for place, row := range cases {
		call := place + count.Next(count.One)
		stub.answer(t, call, row.output, row.exit)
		if row.pause != "" {
			stub.pause(t, call, row.pause)
		}
	}
	files, _ := disk(t)
	opts := askFor(count.One, shortTimeout)
	report := runWith(t, root, opts, files)
	outcomes := report.Outcomes()
	if len(outcomes) < len(cases) {
		t.Fatalf("%d outcomes for %d rows", len(outcomes), len(cases))
	}
	for place, row := range cases {
		got := outcomes[place]
		if got.State() != row.state || got.Failing() != row.failing {
			t.Errorf("%q (exit %d, pause %q): %s %q, want %s %q", row.output,
				row.exit, row.pause, got.State(), got.Failing(), row.state,
				row.failing)
		}
	}
}

// a run says where it has got every fifty mutants, with every count, the
// time so far and about how much is left at the pace so far, and says
// nothing at the end. the clock is the caller's, so the times are too.
func TestARunSaysWhereItHasGotEveryFifty(t *testing.T) {
	stub := stubGo(t)
	root := withATest(t, "many.go", manyFunctions(fiftyFunctions))
	stub.answer(t, count.One, "ok\n", count.Nothing)
	tenth := int(held(t, "of-each-ten"))
	for place := range mutantsOfFifty {
		call := place + count.Next(count.One)
		switch place % tenth {
		case count.First:
			stub.answer(t, call, "ok\n", count.Nothing)
		case count.One:
			stub.answer(t, call, "panic: test timed out after 20s\n", count.One)
		default:
			stub.answer(t, call, "--- FAIL: TestX (0.00s)\nFAIL\n", count.One)
		}
	}
	step := secondsHeld(t, "clock-step-seconds")
	clock := &ticking{rest: step}
	said := &heard{}
	files, _ := disk(t)
	opts := askFor(count.One, byTheSecond).Nowed(clock.now).
		Telling(said.tell)
	report := runWith(t, root, opts, files)
	made := report.Outcomes()
	if len(made) != mutantsOfFifty {
		t.Fatalf("%d outcomes, want %d", len(made), mutantsOfFifty)
	}
	var progress []string
	for _, line := range said.lines {
		if strings.Contains(line, " done") {
			progress = append(progress, line)
		}
	}
	want := []string{
		"mutants: 50 of 150 done: killed 40, survived 5, timed out 5; " +
			"10.0s so far, about 20.0s left",
		"mutants: 100 of 150 done: killed 80, survived 10, timed out 10; " +
			"30.0s so far, about 15.0s left",
	}
	if !slices.Equal(progress, want) {
		t.Errorf("said\n%s\nwant\n%s", strings.Join(progress, "\n"),
			strings.Join(want, "\n"))
	}
}

// with no timeouts the progress line does not mention them.
func TestAProgressLineLeavesOutWhatIsNotThere(t *testing.T) {
	stub := stubGo(t)
	root := withATest(t, "many.go", manyFunctions(twentyFunctions))
	stub.answer(t, count.One, "ok\n", count.Nothing)
	for place := range mutantsOfTwenty {
		stub.answer(t, place+count.Next(count.One),
			"--- FAIL: TestX (0.00s)\nFAIL\n", count.One)
	}
	said := &heard{}
	files, _ := disk(t)
	runWith(t, root, askFor(count.One, byTheSecond).Telling(said.tell),
		files)
	var progress []string
	for _, line := range said.lines {
		if strings.Contains(line, " done") {
			progress = append(progress, line)
		}
	}
	if len(progress) != count.One || !strings.HasPrefix(progress[count.First],
		"mutants: 50 of 60 done: killed 50, survived 0; ") {
		t.Errorf("said %q", progress)
	}
}

// a run that goes quiet speaks, with none done and then with some.
func TestARunSpeaksWhenItIsQuiet(t *testing.T) {
	stub := stubGo(t)
	root := withATest(t, "many.go", manyFunctions(count.One))
	stub.answer(t, count.One, "ok\n", count.Nothing)
	for call := range count.Next(count.Next(count.One)) {
		stub.pause(t, call+count.Next(count.One), "0.35")
	}
	said := &heard{}
	// the unchanged suite takes a millisecond by this clock, and every
	// reading after it an hour
	clock := &ticking{steps: []time.Duration{time.Millisecond}, rest: time.Hour}
	files, _ := disk(t)
	opts := askFor(count.One, byTheSecond).Nowed(clock.now).
		QuietAfter(millisHeld(t, "quiet-for-millis")).Telling(said.tell)
	runWith(t, root, opts, files)
	joined := strings.Join(said.lines, "\n")
	if !strings.Contains(joined, "none of 3 done yet") ||
		!strings.Contains(joined, "1 of 3 done") {
		t.Errorf("a quiet run said:\n%s", joined)
	}
}

// a run is cancelled while a mutant is under test: that mutant and every
// one not reached are stopped, and the run stops feeding.
func TestACancelledRunStopsFeeding(t *testing.T) {
	stub := stubGo(t)
	root := fixture(t, "")
	stub.answer(t, count.One, "ok\n", count.Nothing)
	for call := range len(tinyNames) {
		stub.pause(t, call+count.Next(count.One), longPause)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go cancelOnTheSecondRun(stub, cancel)
	files, _ := disk(t)
	report, err := runBefore(t, ctx, root, quick(t).Writing(files))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled run returned %v", err)
	}
	if countsOf(report)["stopped"] != len(tinyNames) {
		t.Errorf("stopped %v", countsOf(report))
	}
}

// the copy a mutant is tried in leaves out .git and bin at the root and
// every link, keeps the rest, makes directories as an owner makes them
// and keeps each file's own mode.
func TestTheCopyOfTheTreeIsTheTreeLessWhatIsLeftOut(t *testing.T) {
	stub := stubGo(t)
	root := withATest(t, "many.go", manyFunctions(count.One))
	for _, rel := range []string{".git/HEAD", "bin/game", "sub/bin/kept",
		"sub/.git/kept", "keep.txt"} {
		write(t, root, rel, rel+"\n")
	}
	write(t, root, "sub/private.txt", "mine\n")
	if err := os.Chmod(filepath.Join(root, "sub", "private.txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("keep.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	stub.answer(t, count.One, "ok\n", count.Nothing)
	files, _ := disk(t)
	runWith(t, root, quick(t).Benchmarking(), files)
	found := stub.ran(t, count.One)
	for _, want := range []string{"./keep.txt", "./sub/.git/kept", "./sub/bin/kept"} {
		if !strings.Contains(found, want+"\n") {
			t.Errorf("the copy lacks %s:\n%s", want, found)
		}
	}
	for _, left := range []string{"./.git", "./bin", "./link"} {
		if strings.Contains(found, left+"\n") || strings.Contains(found, left+"/") {
			t.Errorf("the copy has %s:\n%s", left, found)
		}
	}
	reference := filepath.Join(t.TempDir(), "reference")
	if err := os.Mkdir(reference, 0o755); err != nil {
		t.Fatal(err)
	}
	made, err := os.Stat(reference)
	if err != nil {
		t.Fatal(err)
	}
	long := stub.long(t, count.One)
	wantDir := made.Mode().Perm().String()[count.One:]
	dirLine := regexp.MustCompile(`(?m)^d(\S{9})\S*\s.* bin$`)
	if match := dirLine.FindStringSubmatch(long); match == nil || match[count.One] != wantDir {
		t.Errorf("a copied directory is %v, want %s:\n%s", match, wantDir, long)
	}
	if !regexp.MustCompile(`(?m)^-rw-------\S*\s.* private\.txt$`).MatchString(long) {
		t.Errorf("a file lost its mode:\n%s", long)
	}
}

// a run makes one tree for each worker, and one when it is given none.
func TestARunMakesOneTreeAWorker(t *testing.T) {
	for workers, want := range map[int]int{
		count.Nothing:                count.One,
		count.One:                    count.One,
		int(held(t, "some-workers")): int(held(t, "some-workers")),
	} {
		stub := stubGo(t)
		two := manyFunctions(count.Next(count.One))
		root := withATest(t, "many.go", two)
		stub.answer(t, count.One, "ok\n", count.Nothing)
		for call := range sixCalls {
			stub.pause(t, call+count.Next(count.One), "0.2")
		}
		files, _ := disk(t)
		runWith(t, root, askFor(workers, byTheSecond), files)
		trees := map[string]bool{}
		for _, where := range stub.where(t) {
			trees[where] = true
		}
		if len(trees) != want {
			t.Errorf("%d workers: %d trees, want %d: %v", workers,
				len(trees), want, trees)
		}
	}
}

// a copy that fails leaves nothing behind, and trees with nowhere to be
// put are the run's failure, said as broken.
func TestACopyThatFailsLeavesNothingBehind(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	root := fixture(t, "")
	unreadable(t, root, "secret.txt")
	files, _ := disk(t)
	report := runWith(t, root, quick(t), files)
	if broken := report.Broken(); len(broken) != count.One ||
		!strings.HasPrefix(broken[count.First], "copying the tree: ") {
		t.Errorf("broken %q", broken)
	}
	if left, _ := os.ReadDir(temp); count.Positive(len(left)) {
		t.Errorf("a failed copy left %d behind", len(left))
	}
	t.Setenv("TMPDIR", filepath.Join(temp, "not-here"))
	whole := fixture(t, "")
	report = runWith(t, whole, quick(t), files)
	if broken := report.Broken(); len(broken) != count.One ||
		!strings.HasPrefix(broken[count.First], "copying the tree: ") {
		t.Errorf("trees with nowhere to go: %q", broken)
	}
}

// a source go lists that the copy does not hold, as a link is not, is a
// mutant that is broken, and says it could not open the file.
func TestAMutantOfAFileTheCopyLacksIsBroken(t *testing.T) {
	outside := t.TempDir()
	write(t, outside, "linked.go", "package m\n\nfunc G() int { return 2 + 2 }\n")
	root := withATest(t, "real.go", "package m\n\nfunc F() int { return 1 + 1 }\n")
	if err := os.Symlink(filepath.Join(outside, "linked.go"),
		filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	files, _ := disk(t)
	linked := regexp.MustCompile(`linked\.go`)
	report := runWith(t, root, quick(t).OnlyWhere(linked), files)
	outcomes := report.Outcomes()
	if len(outcomes) == count.Nothing {
		t.Fatal("no mutant of the linked file")
	}
	for _, got := range outcomes {
		if got.State() != "broken" || !strings.HasPrefix(got.Failing(), "open ") {
			t.Errorf("a mutant of a file not there: %s, %q", got.State(),
				got.Failing())
		}
	}
}

// a file that cannot be made writable, or written, is a mutant that is
// broken with the reason; and the original is put back all the same.
func TestAMutantWhoseFileCannotBeChangedIsBroken(t *testing.T) {
	only := quick(t).OnlyWhere(regexp.MustCompile(`:Hello$`))
	owner := os.FileMode(0o644)
	failure := errors.New("chmod refused")
	files := NewMockFiles(gomock.NewController(t))
	files.EXPECT().Chmod(gomock.Any(), owner).Return(failure)
	report := runWith(t, fixture(t, ""), only, files)
	outcomes := report.Outcomes()
	if len(outcomes) != count.One || outcomes[count.First].State() != "broken" ||
		!strings.Contains(outcomes[count.First].Failing(), failure.Error()) {
		t.Errorf("chmod failure outcome %v", outcomes)
	}
	failure = errors.New("write refused")
	original, err := os.ReadFile(filepath.Join(tiny, "tiny.go"))
	if err != nil {
		t.Fatal(err)
	}
	files = NewMockFiles(gomock.NewController(t))
	gomock.InOrder(
		files.EXPECT().Chmod(gomock.Any(), owner).Return(nil),
		files.EXPECT().WriteFile(gomock.Any(), gomock.Not(original), owner).
			Return(failure),
		files.EXPECT().WriteFile(gomock.Any(), original, owner).Return(nil),
	)
	report = runWith(t, fixture(t, ""), only, files)
	outcomes = report.Outcomes()
	if len(outcomes) != count.One || outcomes[count.First].State() != "broken" ||
		!strings.Contains(outcomes[count.First].Failing(), failure.Error()) {
		t.Errorf("write failure outcome %v", outcomes)
	}
}

// the bounds are the documented ones: an unchanged suite has five times
// a mutant's timeout to go test, and a run kills go test at twice what it
// was given.
func TestTheBoundsAreTheDocumentedOnes(t *testing.T) {
	stub := stubGo(t)
	root := withATest(t, "many.go", manyFunctions(count.One))
	timeout := bound
	second, third := count.Next(count.One), count.Next(count.Next(count.One))
	stub.pause(t, second, "0.3")
	stub.pause(t, third, "0.5")
	files, _ := disk(t)
	report := runWith(t, root, askFor(count.One, timeout), files)
	outcomes := report.Outcomes()
	if len(outcomes) < count.Next(count.One) ||
		outcomes[count.First].State() != "survived" ||
		outcomes[count.One].State() != "timed out" {
		t.Errorf("a mutant has twice its timeout: %v", countsOf(report))
	}
	broken := report.Broken()
	if count.Positive(len(broken)) {
		t.Errorf("a suite within its bound: %v", broken)
	}
	suite, mutant := stub.ran(t, count.One), stub.ran(t, count.Next(count.One))
	if !strings.Contains(suite, " -timeout 1s ") ||
		!strings.Contains(mutant, " -timeout 200ms ") {
		t.Errorf("the suite has five times a mutant's timeout:\n%s\n%s", suite, mutant)
	}
	slow := stubGo(t)
	slow.pause(t, count.One, "2.3")
	report = runWith(t, root, askFor(count.One, timeout), files)
	broken = report.Broken()
	made := report.Outcomes()
	if len(broken) != count.One || count.Positive(len(made)) {
		t.Errorf("a suite past twice its bound: %v %v", broken, countsOf(report))
	}
}

// go list is asked of each module, and what it cannot say is said in its
// words: a package it cannot list, a module it cannot read.
func TestARunSaysWhatGoListSaid(t *testing.T) {
	root := moduleOf(t, map[string]string{
		"bad/go.mod":               "module\n",
		"bad/b.go":                 "package bad\n",
		"good/go.mod":              "module good\n\ngo 1.27\n",
		"good/onlytests/o_test.go": "package onlytests\n",
		"good/two/a.go":            "package a\n",
		"good/two/b.go":            "package b\n",
	})
	files, _ := disk(t)
	report := runWith(t, root, quick(t), files)
	broken := report.Broken()
	second := count.Next(count.First)
	if len(broken) != count.Next(count.One) ||
		!strings.HasPrefix(broken[count.First], "bad: go list: go: errors parsing") ||
		!strings.HasPrefix(broken[second], "good/two: found packages") {
		t.Errorf("broken: %q", broken)
	}
	made := report.Outcomes()
	if count.Positive(len(made)) {
		t.Errorf("outcomes: %v", countsOf(report))
	}
}

// a listing that holds a directory that is not there, a package with an
// error, and one that is fine: the first two are said and the third is
// still run.
func TestAListingKeepsFailuresAndContinues(t *testing.T) {
	stub := stubGo(t)
	root := withATest(t, "valid/source.go", "package valid\n\nfunc F() int { return 1 + 1 }\n")
	write(t, root, "valid/source_test.go", "package valid\n")
	write(t, root, "broken/source.go", "package broken\n")
	stub.listing(t, root+"/missing\tsource.go\t1\t0\t\n"+
		root+"/broken\tsource.go\t1\t0\tunquoted error\n"+
		root+"/valid\tsource.go\t1\t0\t\n")
	stub.answer(t, count.One, "ok\n", count.Nothing)
	files, _ := disk(t)
	report := runWith(t, root, quick(t), files)
	broken := report.Broken()
	if len(broken) != count.Next(count.One) ||
		!strings.Contains(broken[count.First], "missing") ||
		!strings.Contains(broken[count.First], "no such file") ||
		broken[count.Next(count.First)] != "broken: unquoted error" {
		t.Errorf("a listing lost failure details: %v", broken)
	}
	if got := outcomeNames(report); !strings.Contains(got, "valid/source.go F:") {
		t.Errorf("the valid package was not run:\n%s", got)
	}
}

// a source go lists that is not there, or that does not parse, stops the
// run, and one that does not parse in a module asking for a newer go than
// game was built with says so.
func TestASourceThatCannotBeReadStopsTheRun(t *testing.T) {
	stub := stubGo(t)
	root := moduleOf(t, map[string]string{})
	stub.listing(t, root+"\tmissing.go\t1\t0\t\n")
	files, _ := disk(t)
	if _, err := runBefore(t, context.Background(), root,
		quick(t).Writing(files)); err == nil || !strings.Contains(err.Error(), "missing.go") {
		t.Errorf("a missing source was mutated: %v", err)
	}
	write(t, root, "go.mod", "module x\n\ngo 1.99\n")
	write(t, root, "bad.go", "package x\n\nfunc (\n")
	stub.listing(t, root+"\tbad.go\t1\t0\t\n")
	if _, err := runBefore(t, context.Background(), root,
		quick(t).Writing(files)); err == nil || !strings.Contains(err.Error(), "asks for go1.99") {
		t.Errorf("a newer module's parse error: %v", err)
	}
}

// functionOf is the function a mutant is in, as its name says it.
func functionOf(mutant mutants.Mutant) string {
	words := strings.Fields(mutant.Name())
	return strings.TrimSuffix(words[count.One], ":")
}

// a directory go does not look in, because of its name, is still part of
// the tree that is copied, and one that cannot be entered fails the copy.
func TestADirectoryTheCopyCannotEnterIsBroken(t *testing.T) {
	if os.Geteuid() == rootUID {
		t.Skip("root enters every directory")
	}
	root := fixture(t, "")
	shut := filepath.Join(root, "testdata")
	if err := os.Mkdir(shut, count.Nothing); err != nil {
		t.Fatal(err)
	}
	open := os.FileMode(0o755)
	t.Cleanup(shutPath{path: shut, open: open}.reopen)
	files, _ := disk(t)
	report := runWith(t, root, quick(t), files)
	if broken := report.Broken(); len(broken) != count.One ||
		!strings.HasPrefix(broken[count.First], "copying the tree: ") {
		t.Errorf("broken %q", broken)
	}
}

// sourceOf is a go file of a package with one sum in it.
func sourceOf(name string) string {
	return "package " + name + "\n\nfunc F() int { return 1 + 1 }\n"
}

// benched is a bench of the root: the suites run and nothing is changed.
func benched(t *testing.T, root string) (mutants.Report, error) {
	t.Helper()
	return runBefore(t, context.Background(), root, quick(t).Benchmarking())
}

// cancelOnTheSecondRun cancels once go test has run a second time, which
// is a mutant under test, the first being the unchanged suite.
func cancelOnTheSecondRun(stub goStub, cancel context.CancelFunc) {
	for stub.calls() < count.Next(count.One) {
		time.Sleep(pollEvery)
	}
	cancel()
}

// a listing that holds a package with no go files to change, and one
// that does, runs and times only the second, and names the first untested
// nowhere: it has no source and no test.
func TestAPackageWithNothingToChangeIsNotRun(t *testing.T) {
	stub := stubGo(t)
	root := withATest(t, "valid/source.go", "package valid\n\nfunc F() int { return 1 + 1 }\n")
	write(t, root, "valid/source_test.go", "package valid\n")
	write(t, root, "empty/only_test.go", "package empty\n")
	stub.listing(t, root+"/empty\t\t0\t0\t\n"+
		root+"/valid\tsource.go\t1\t0\t\n")
	stub.answer(t, count.One, "ok\n", count.Nothing)
	files, _ := disk(t)
	said := &heard{}
	opts := quick(t).Benchmarking().Telling(said.tell)
	report := runWith(t, root, opts, files)
	suites := report.Suites()
	told := strings.Join(said.lines, "\n")
	if len(suites) != count.One || len(report.Broken()) != count.Nothing ||
		suites[count.First].Dir() != "valid" || len(report.Untested()) != count.Nothing ||
		!strings.Contains(told, "in 1 package at") ||
		strings.Contains(told, "empty") {
		t.Errorf("suites %+v broken %v, told:\n%s", suites,
			report.Broken(), told)
	}
}

// aSwitch has a switch on a tag, a tagless one with compound cases, a case
// body that holds a statement, and a select.
const aSwitch = `package pick

func Pick(a, b, c bool, n, k int) int {
	switch n + 2*k {
	case n * 3, n - 4:
		return n + 5
	}
	switch {
	case a && (b || c):
		return n + 6
	case a || (b && c):
		return 0
	}
	return n
}

// Send picks a case to send in.
func Send(ch chan int, done chan bool, n int) {
	select {
	case ch <- n + 7:
	case <-done:
	}
}
`

// TestAMutantInACaseIsNamedByTheCaseExpression: a change inside an
// expression of a case is named by that expression, whole, before and
// after. Two changes in one have two names. So it is for a switch's tag.
// A change in a case's body is named by its statement, as it is anywhere.
func TestAMutantInACaseIsNamedByTheCaseExpression(t *testing.T) {
	report, _ := ranNothing(t, withATest(t, "pick.go", aSwitch))
	names := outcomeNames(report)
	for _, want := range []string{
		"pick.go Pick: n + 2*k => n - 2*k",
		"pick.go Pick: n + 2*k => n + 2/k",
		"pick.go Pick: n + 2*k => n + 3*k",
		"pick.go Pick: n * 3 => n / 3",
		"pick.go Pick: n * 3 => n * 4",
		"pick.go Pick: n - 4 => n + 4",
		"pick.go Pick: n - 4 => n - 5",
		"pick.go Pick: return n + 5 => return n - 5",
		"pick.go Pick: a && (b || c) => a || (b || c)",
		"pick.go Pick: a && (b || c) => a && (b && c)",
		"pick.go Pick: a || (b && c) => a && (b && c)",
		"pick.go Pick: a || (b && c) => a || (b || c)",
		"pick.go Pick: return n + 6 => return n - 6",
		"pick.go Send: ch <- n + 7 => ch <- n - 7",
	} {
		if !strings.Contains("\n"+names+"\n", "\n"+want+"\n") {
			t.Errorf("no mutant named %q in\n%s", want, names)
		}
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(names, "\n") {
		if seen[line] {
			t.Errorf("two mutants are named %q", line)
		}
		seen[line] = true
	}
}
