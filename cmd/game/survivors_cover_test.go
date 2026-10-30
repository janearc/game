package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/janearc/game/internal/count"
)

// the statements a fixture file holds and runs, defined here so a file
// can be a quarter run, half run or three quarters run.
const (
	ofFour   = 4 // defined here: statements in a fixture file
	ofTwo    = 2 // defined here: statements in a smaller fixture
	oneRun   = 1 // defined here: one statement run
	twoRun   = 2 // defined here: two statements run
	threeRun = 3 // defined here: three statements run
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
	commands := notRun(t)
	var out bytes.Buffer
	err := coverGo(dir, commands, &out)
	if err == nil || count.Positive(out.Len()) {
		t.Errorf("bin that is a file: %v, said %q", err, out.String())
	}
}

// TestCoverGoRunsTheTestsWhenTheProfileFolderIsMade: the anti-test: with
// a bin/ that is a folder the tests are asked for.
func TestCoverGoRunsTheTestsWhenTheProfileFolderIsMade(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := runs(t, ranStep{dir: dir, name: "go",
		args: []string{"test", "-count=1", "-coverprofile=" + goProfile,
			"./..."}, err: os.ErrPermission})
	err := coverGo(dir, commands, &bytes.Buffer{})
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("tests that fail: %v", err)
	}
}
