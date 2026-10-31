package mutants_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

// emptied is whether the folder holds nothing.
func emptied(t *testing.T, place string) bool {
	t.Helper()
	left, err := os.ReadDir(place)
	if err != nil {
		t.Fatal(err)
	}
	return len(left) == count.Nothing
}

// the tests of a mutant are given a temp folder of their own, inside
// their worker's, inside the run's copies. no two go tests share one. it
// is not the folder the run was started with, and nothing is left when
// the run ends.
func TestEachGoTestGetsATempFolderInsideTheRunsCopies(t *testing.T) {
	note := placed(t)
	stub := stubGo(t)
	root := fixture(t, "")
	stub.answer(t, count.One, "ok\n", count.Nothing)
	runBefore(t, context.Background(), root, quick(t))
	temps := stub.tempDirs(t)
	if len(temps) != len(tinyNames)+count.One {
		t.Fatalf("go test ran with %d temp folders: %v", len(temps), temps)
	}
	seen := map[string]bool{}
	for _, temp := range temps {
		inside, err := filepath.Rel(note.place, temp)
		parts := strings.Split(inside, string(filepath.Separator))
		if err != nil || len(parts) != len([]string{"run", "worker", "test"}) ||
			!strings.HasPrefix(parts[count.Nothing], "game-mutants-") ||
			!strings.HasPrefix(parts[count.One], "tmp-") || seen[temp] {
			t.Errorf("go test was given TMPDIR %s, not a folder of its own in "+
				"its worker's", temp)
		}
		seen[temp] = true
	}
	if !emptied(t, note.place) {
		t.Error("the run left its temp folders behind")
	}
}

// TestNoGoTestsTempFolderOutlivesIt: each go test leaves a folder in its
// TMPDIR, as a test binary killed at its first failure does, and the next
// go test finds none of the earlier ones still there. a long run does not
// pile them up until it ends.
func TestNoGoTestsTempFolderOutlivesIt(t *testing.T) {
	placed(t)
	stub := stubGo(t)
	root := fixture(t, "")
	stub.answer(t, count.One, "ok\n", count.Nothing)
	for call := range len(tinyNames) {
		stub.answer(t, call+count.Next(count.One),
			"--- FAIL: TestX (0.00s)\nFAIL\n", count.One)
	}
	one := mutants.Ask(count.One, secondsHeld(t, "test-timeout-seconds")).
		Nowed(time.Now).QuietAfter(quiet).Writing(mutants.Disk())
	runBefore(t, context.Background(), root, one)
	if ran := stub.tempDirs(t); len(ran) <= count.One {
		t.Fatalf("too few go tests to tell: %v", ran)
	}
	if outlived := stub.outlived(); len(outlived) != count.Nothing {
		t.Errorf("go tests' temp folders outlived them: %v", outlived)
	}
}

// stopped while a mutant is under test the run leaves nothing in the
// folder its copies were made in. go test was killed and could not clean
// up after itself.
func TestAStoppedRunLeavesNoCopies(t *testing.T) {
	note := placed(t)
	stub := stubGo(t)
	root := fixture(t, "")
	stub.answer(t, count.One, "ok\n", count.Nothing)
	stub.pause(t, count.Next(count.One), longPause)
	ctx, cancel := context.WithCancel(context.Background())
	go cancelOnTheSecondRun(stub, cancel)
	_, err := runBefore(t, ctx, root, quick(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled run returned %v", err)
	}
	if !emptied(t, note.place) {
		left, _ := os.ReadDir(note.place)
		t.Errorf("a stopped run left %v", left)
	}
}

// finished runs remove their copies and keep their results. the report
// they gave back is whole once the folder is empty.
func TestAFinishedRunKeepsItsResultsWhenTheCopiesGo(t *testing.T) {
	note := placed(t)
	root := fixture(t, "")
	report, err := runBefore(t, context.Background(), root, quick(t))
	if err != nil {
		t.Fatal(err)
	}
	if !emptied(t, note.place) {
		t.Error("a finished run left its copies")
	}
	lines, suites := report.Lines(), report.Suites()
	if len(lines) != len(tinyNames) || len(suites) != count.One {
		t.Fatalf("the results after the copies went: %d lines, %d suites",
			len(lines), len(suites))
	}
	if took := suites[count.First].Took(); took <= noTime {
		t.Errorf("the suite took %s", took)
	}
}

// noTime is no time at all.
var noTime time.Duration

// runs told where to make their copies make them there. they make the
// folder if it is not there. they leave it empty when the run is done.
// they say where, so whoever looks for the copies knows.
func TestCopiesAreMadeWhereTheRunIsTold(t *testing.T) {
	stub := stubGo(t)
	root := fixture(t, "")
	stub.answer(t, count.One, "ok\n", count.Nothing)
	told := filepath.Join(t.TempDir(), "not", "yet", "there")
	heard := &heard{}
	opts := quick(t).Placing(told).Telling(heard.tell)
	if !strings.Contains(opts.String(), ", copies in "+told) {
		t.Errorf("the options say %q, not where the copies go", opts.String())
	}
	if _, err := runBefore(t, context.Background(), root, opts); err != nil {
		t.Fatal(err)
	}
	for _, temp := range stub.tempDirs(t) {
		inside, err := filepath.Rel(told, temp)
		if err != nil || !strings.HasPrefix(inside, "game-mutants-") {
			t.Errorf("go test ran in %s, not in a copy under %s", temp, told)
		}
	}
	if !emptied(t, told) {
		t.Errorf("the run left its copies in %s", told)
	}
	said := strings.Join(heard.lines, "\n")
	if !strings.Contains(said, ", copies in "+told+";") {
		t.Errorf("the run did not say where its copies were:\n%s", said)
	}
}

// places that cannot be made are the copy's failure. they are said as the
// tree that would not copy.
func TestAPlaceThatCannotBeMadeIsABrokenCopy(t *testing.T) {
	root := fixture(t, "")
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	opts := quick(t).Placing(filepath.Join(file, "under"))
	report, err := runBefore(t, context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if broken := report.Broken(); len(broken) != count.One ||
		!strings.HasPrefix(broken[count.First], "copying the tree: ") ||
		!strings.HasSuffix(broken[count.First], file+": not a directory") {
		t.Errorf("broken %q", broken)
	}
}
