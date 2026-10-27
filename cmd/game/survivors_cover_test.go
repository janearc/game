package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/janearc/game/internal/count"
	"go.uber.org/mock/gomock"
)

// the statements a fixture file holds and runs, defined here so a file
// can be a quarter run, half run or three quarters run.
const (
	ofFour         = 4  // defined here: statements in a fixture file
	ofTwo          = 2  // defined here: statements in a smaller fixture
	oneRun         = 1  // defined here: one statement run
	twoRun         = 2  // defined here: two statements run
	threeRun       = 3  // defined here: three statements run
	lessExecutable = 10 // defined here: lines a first target says it can run
	moreExecutable = 30 // defined here: lines a later target says it can run
)

// covering is a file with a count of statements, so many of them run.
func covering(path string, statements, ran int) covered {
	return covered{path: path, count: statements, ran: ran}
}

// before is whether one file is ordered before another, after it, or
// neither.
func before(one, other covered) (first, last, same bool) {
	got := leastRunFirst(one, other)
	return got < count.Nothing, count.Positive(got), got == count.Nothing
}

// TestLeastRunFirstOrdersByShareThenByPath: the least run is first
// whatever its path, and files of an equal share go by path.
func TestLeastRunFirstOrdersByShareThenByPath(t *testing.T) {
	t.Parallel()
	low := covering("z.go", ofFour, oneRun)
	high := covering("a.go", ofFour, threeRun)
	if first, _, _ := before(low, high); !first {
		t.Error("a quarter run should come before three quarters")
	}
	if _, last, _ := before(high, low); !last {
		t.Error("three quarters run should come after a quarter")
	}
	half := covering("a.go", ofTwo, oneRun)
	other := covering("b.go", ofFour, twoRun)
	if first, _, _ := before(half, other); !first {
		t.Error("equal shares should go by path, a first")
	}
	if _, last, _ := before(other, half); !last {
		t.Error("equal shares should go by path, b last")
	}
	if _, _, same := before(half, half); !same {
		t.Error("a file is not before itself")
	}
}

// TestCoverGoWithABinThatIsAFileRunsNothing: a bin/ that cannot hold the
// profile's folder is the error of making it, before any test is run.
func TestCoverGoWithABinThatIsAFileRunsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bin"), "a file\n")
	commands := NewMockcommandRunner(gomock.NewController(t))
	var out bytes.Buffer
	err := coverGoBy(dir, commands.Run, &out)
	if err == nil || count.Positive(out.Len()) {
		t.Errorf("bin that is a file: %v, said %q", err, out.String())
	}
}

// failsPermission is a runner's answer that the tool was refused.
func failsPermission(string, string, ...string) (string, error) {
	return "", os.ErrPermission
}

// TestCoverGoRunsTheTestsWhenTheProfileFolderIsMade: the anti-test: with
// a bin/ that is a folder the tests are asked for.
func TestCoverGoRunsTheTestsWhenTheProfileFolderIsMade(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	commands.EXPECT().Run(dir, "go", "test", "-count=1",
		"-coverprofile="+goProfile, "./...").DoAndReturn(failsPermission)
	err := coverGoBy(dir, commands.Run, &bytes.Buffer{})
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("tests that fail: %v", err)
	}
}

// compileWords is a C compile's words: the flags, a source and an output.
func compileWords() []string {
	words := append([]string{}, cFlags...)
	return append(words, "t.c", "-o", "bin/t")
}

// TestBuildCoveredWithABinThatIsAFileCompilesNothing: a bin/ that cannot
// hold the cover folder is that error, before the compiler is asked.
func TestBuildCoveredWithABinThatIsAFileCompilesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bin"), "a file\n")
	commands := NewMockcommandRunner(gomock.NewController(t))
	err := buildCovered(dir, compileWords(), "bin/.cover/t", nil,
		commands.Run, noFetch)
	if err == nil {
		t.Error("a bin that is a file was built into")
	}
}

// unwritable is a folder that cannot be written to or removed from until
// the test ends.
func unwritable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(openedAgain{path}.open)
}

// openedAgain is a folder to give its permissions back.
type openedAgain struct{ path string }

// open gives the folder its permissions back, so its parent can be
// removed.
func (folder openedAgain) open() { os.Chmod(folder.path, 0o755) }

// xcodeScheme is the words that name a scheme and nothing else.
func xcodeScheme() []string { return []string{"-scheme", "S"} }

// TestCoverXcodeStopsWhereItCannotClearTheOldResult: a result bundle
// that cannot be removed is the error of removing it, with no build.
func TestCoverXcodeStopsWhereItCannotClearTheOldResult(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	old := filepath.Join(dir, "bin", ".xcode", "cover.xcresult")
	write(t, filepath.Join(old, "Info.plist"), "old\n")
	unwritable(t, old)
	commands := NewMockcommandRunner(gomock.NewController(t))
	err := coverXcodeBy(dir, xcodeScheme(), commands.Run, &bytes.Buffer{},
		&bytes.Buffer{})
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("a result that cannot be removed: %v", err)
	}
}

// TestCoverXcodeStopsWhereItCannotMakeItsFolder: a bin/ that cannot be
// written into is the error of making .xcode, with no build.
func TestCoverXcodeStopsWhereItCannotMakeItsFolder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	unwritable(t, filepath.Join(dir, "bin"))
	commands := NewMockcommandRunner(gomock.NewController(t))
	err := coverXcodeBy(dir, xcodeScheme(), commands.Run, &bytes.Buffer{},
		&bytes.Buffer{})
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("a bin that cannot be written: %v", err)
	}
}

// TestXcodeCoveredKeepsTheFirstOfEqualRuns: a file in two targets with
// the same lines run is counted once, as the first target had it; the
// one with more run replaces it.
func TestXcodeCoveredKeepsTheFirstOfEqualRuns(t *testing.T) {
	t.Parallel()
	report := `{"targets":[
		{"files":[{"path":"/p/a.swift","coveredLines":5,"executableLines":10}]},
		{"files":[{"path":"/p/a.swift","coveredLines":5,"executableLines":20}]},
		{"files":[{"path":"/p/b.swift","coveredLines":1,"executableLines":10}]},
		{"files":[{"path":"/p/b.swift","coveredLines":2,"executableLines":30}]}]}`
	files, err := xcodeCovered(report, "/p")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]covered{}
	for _, each := range files {
		got[each.path] = each
	}
	if len(got) != ofTwo || got["a.swift"].count != lessExecutable ||
		got["b.swift"].count != moreExecutable {
		t.Errorf("counted %+v", got)
	}
}
