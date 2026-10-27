package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/janearc/game/internal/count"
)

// A report lists the least run first, then the total, and passes only
// when every file is at 100%.
func TestReportListsTheLeastRunFirst(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	// a profile is how the files' counts are written: statements, then
	// whether a block ran
	files, perr := goCovered("mode: set\n" +
		"b.c:1.1,2.2 4 1\n" +
		"a.c:1.1,2.2 8 1\na.c:3.1,4.2 2 0\n" +
		"c.c:1.1,2.2 0 0\n")
	if perr != nil {
		t.Fatal(perr)
	}
	err := report(files, &out)
	want := "  80.0%  a.c (8 of 10)\n 100.0%  b.c (4 of 4)\n" +
		" 100.0%  c.c (0 of 0)\n  85.7%  total (12 of 14)\n"
	if !errors.Is(err, errNotFull) || !strings.Contains(err.Error(),
		"1 below") || out.String() != want {
		t.Errorf("err %v, said\n%s", err, out.String())
	}
	out.Reset()
	whole, perr := goCovered("mode: set\na.c:1.1,2.2 3 1\n")
	if perr != nil {
		t.Fatal(perr)
	}
	if err := report(whole, &out); err != nil {
		t.Errorf("all at 100%%: %v", err)
	}
}

// anti-test: a report with nothing in it is refused, not passed.
func TestReportRefusesNothingMeasured(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := report(nil, &out); !errors.Is(err, errNothingCovered) {
		t.Errorf("nothing: %v", err)
	}
}

// Whatever the files, the total is their sums, and the report passes just
// when none ran less than it could.
func TestReportTotalsWhatItWasGiven(t *testing.T) {
	t.Parallel()
	prop := totalsProperty{
		mostFiles:      countHeld(t, "drawn-most-files"),
		mostStatements: countHeld(t, "drawn-most-statements"),
	}
	rapid.Check(t, prop.check)
}

// wholePercent is all of a file run, as a percentage has defined it.
const wholePercent = 100

// totalsProperty is the property that whatever the files, the total is
// their sums, and the report passes just when none ran less than it
// could.
type totalsProperty struct {
	mostFiles      int
	mostStatements int
}

// check is the property as rapid runs it.
func (prop totalsProperty) check(t *rapid.T) {
	many := rapid.IntRange(count.One, prop.mostFiles).Draw(t, "files")
	files := make([]covered, many)
	var statements, ran int
	full := true
	for place := range files {
		has := rapid.IntRange(count.Nothing, prop.mostStatements).
			Draw(t, "count")
		runs := rapid.IntRange(count.Nothing, has).Draw(t, "ran")
		name := strings.Repeat("f", count.Next(place)) + ".c"
		files[place] = covered{path: name, count: has, ran: runs}
		statements, ran = statements+has, ran+runs
		full = full && runs == has
	}
	var out bytes.Buffer
	err := report(files, &out)
	total := covered{count: statements, ran: ran}
	said := "total (" + strconv.Itoa(ran) + " of " +
		strconv.Itoa(statements) + ")"
	percent := total.percent()
	if (err == nil) != full || !strings.Contains(out.String(), said) ||
		percent < count.Nothing || percent > wholePercent {
		t.Fatalf("files %+v: err %v, said %s", files, err, out.String())
	}
}

// Libraries game fetched and test files are left out; a project's own
// files are counted.
func TestCountedLeavesOutLibrariesAndTests(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]bool{
		"half.c": true, "Sources/Sign/Sign.swift": true, "src/m.m": true,
		"bin/.deps/theft-62e093d/src/theft.c":        false,
		"/tree/bin/.deps/theft-62e093d/src/a.c":      false,
		"Tests/SignTests/SignTests.swift":            false,
		"/tree/Tests/x/y.m":                          false,
		"github.com/janearc/game/cmd/game/a_test.go": false,
		"Sources/LibTests.swift":                     false,
		"Sources/LibTest.m":                          false,
	} {
		if counted(path) != want {
			t.Errorf("%s: counted %v", path, !want)
		}
	}
}

// A Go profile's blocks are summed by file; a block listed twice counts
// once, run if either says so.
func TestGoCoveredSumsEachFile(t *testing.T) {
	t.Parallel()
	files, err := goCovered("mode: set\n" +
		"x/a.go:1.1,2.2 3 1\nx/a.go:3.1,4.2 2 0\nx/b.go:1.1,2.2 4 0\n" +
		"x/b.go:1.1,2.2 4 1\n")
	var out bytes.Buffer
	_ = report(files, &out)
	for _, want := range []string{"x/a.go (3 of 5)", "x/b.go (4 of 4)"} {
		if err != nil || !strings.Contains(out.String(), want) {
			t.Errorf("files %+v, err %v: no %q in\n%s", files, err, want,
				out.String())
		}
	}
}

// anti-test: a line that is not a profile line is refused.
func TestGoCoveredRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	for _, profile := range []string{"no colon here\n", "x/a.go:1.1 3\n",
		"x/a.go:1.1,2.2 three 1\n"} {
		if _, err := goCovered(profile); !errors.Is(err, errProfile) {
			t.Errorf("%q: %v", profile, err)
		}
	}
}

// goTests stands in for go test: it writes the profile it was given, as
// go test -coverprofile would, unless it fails.
type goTests struct {
	tools
	profile string
	mode    os.FileMode
}

// run is goTests' runner.
func (tests *goTests) run(dir, name string, args ...string) (string,
	error) {
	_, _ = tests.tools.run(dir, name, args...)
	if tests.fail["go"] != nil {
		return "", tests.fail["go"]
	}
	if tests.profile != "" {
		return "", os.WriteFile(filepath.Join(dir, goProfile),
			[]byte(tests.profile), tests.mode)
	}
	return "", nil
}

// game cover with no word runs the Go tests with a profile and reports it.
func TestCoverGoReportsTheProfile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	shutFile := os.FileMode(0o600)
	tests := &goTests{profile: "mode: set\nx/a.go:1.1,2.2 2 1\n",
		mode: shutFile}
	var out, errs bytes.Buffer
	err := coverBy(dir, nil, tests.run, noFetch, &out, &errs)
	if err != nil || out.String() != " 100.0%  x/a.go (2 of 2)\n"+
		" 100.0%  total (2 of 2)\n" || len(tests.calls) != count.One ||
		tests.calls[count.First].name != "go" {
		t.Errorf("err %v, said %q, calls %+v", err, out.String(),
			tests.calls)
	}
}

// anti-tests: failing tests, no profile written, a profile it cannot
// read, no room for bin/, and a word that is not c or xcode.
func TestCoverGoRefusesWhatItCannotReport(t *testing.T) {
	t.Parallel()
	var out, errs bytes.Buffer
	shutFile := os.FileMode(0o600)
	failed := errors.New("go test: FAIL")
	failing := &goTests{tools: tools{fail: map[string]error{"go": failed}}}
	if err := coverBy(t.TempDir(), nil, failing.run, noFetch, &out,
		&errs); !errors.Is(err, failed) {
		t.Errorf("failing tests: %v", err)
	}
	if err := coverBy(t.TempDir(), nil, (&goTests{}).run, noFetch, &out,
		&errs); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no profile: %v", err)
	}
	if err := coverBy(t.TempDir(), nil,
		(&goTests{profile: "nonsense\n", mode: shutFile}).run, noFetch,
		&out,
		&errs); !errors.Is(err, errProfile) {
		t.Errorf("a bad profile: %v", err)
	}
	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "bin"), nil,
		shutFile); err != nil {
		t.Fatal(err)
	}
	if err := coverBy(full, nil, (&goTests{}).run, noFetch, &out,
		&errs); err == nil {
		t.Error("a file where bin/ goes was taken")
	}
	if err := coverBy(t.TempDir(), []string{"rust"}, (&goTests{}).run,
		noFetch, &out, &errs); err == nil ||
		!strings.Contains(err.Error(), "not c or xcode") {
		t.Errorf("rust: %v", err)
	}
}
