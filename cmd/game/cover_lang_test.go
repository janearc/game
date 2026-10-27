package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// failsAt stands in for the tools and fails the call numbered at.
type failsAt struct {
	tools
	at  int
	err error
}

// run is failsAt's runner.
func (stand *failsAt) run(dir, name string, args ...string) (string,
	error) {
	_, _ = stand.tools.run(dir, name, args...)
	if len(stand.calls) == stand.at {
		return "", stand.err
	}
	return stand.said[name], nil
}

// cExport is llvm-cov's export for files, each filename with its lines
// summary as llvm-cov writes it.
func cExport(files map[string]string) string {
	var each []string
	for name, lines := range files {
		each = append(each, `{"filename":"`+name+`","summary":{"lines":`+
			lines+`}}`)
	}
	slices.Sort(each)
	return `{"data":[{"files":[` + strings.Join(each, ",") + `]}]}`
}

// game cover c builds with coverFlags, runs the program with its profile
// named, merges it, exports it, and reports the project's own files.
func TestCoverCRunsTheChainAndReports(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stand := &tools{said: map[string]string{"xcrun": cExport(map[string]string{
		filepath.Join(dir, "t.c"):                       `{"count":4,"covered":4}`,
		filepath.Join(dir, "bin/.deps/theft-x/src/a.c"): `{"count":9,"covered":1}`,
	})}}
	var out, errs bytes.Buffer
	err := coverBy(dir, []string{"c", "t.c", "--", "fast"}, stand.run,
		noFetch, &out, &errs)
	want := []call{
		{dir, "cc", slices.Concat(cFlags, coverFlags,
			[]string{"t.c", "-o", "bin/t-cover"})},
		{dir, "env", []string{"LLVM_PROFILE_FILE=bin/.cover/t-cover.profraw",
			"bin/t-cover", "fast"}},
		{dir, "xcrun", []string{"llvm-profdata", "merge", "-sparse",
			"bin/.cover/t-cover.profraw", "-o", "bin/.cover/t-cover.profdata"}},
		{dir, "xcrun", []string{"llvm-cov", "export", "-summary-only",
			"bin/t-cover", "-instr-profile=bin/.cover/t-cover.profdata"}},
	}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) {
		t.Errorf("err %v, calls %+v", err, stand.calls)
	}
	if out.String() != " 100.0%  t.c (4 of 4)\n 100.0%  total (4 of 4)\n" {
		t.Errorf("said %q", out.String())
	}
}

// anti-tests: each step's failure stops every step after it.
func TestCoverCStopsAtTheFirstFailure(t *testing.T) {
	t.Parallel()
	broken := errors.New("broken")
	// the build, the run, the merge and the export
	steps := countHeld(t, "cover-c-steps")
	for at := count.One; at <= steps; at++ {
		stand := &failsAt{at: at, err: broken}
		var out, errs bytes.Buffer
		err := coverBy(t.TempDir(), []string{"c", "t.c"}, stand.run, noFetch,
			&out, &errs)
		if !errors.Is(err, broken) || len(stand.calls) != at {
			t.Errorf("failing at %d: err %v, %d calls", at, err,
				len(stand.calls))
		}
	}
}

// anti-tests: what cover c cannot build or read is refused.
func TestCoverCRefusesWhatItCannotMeasure(t *testing.T) {
	t.Parallel()
	for name, each := range map[string]struct {
		args []string
		want error
	}{
		"no sources": {nil, errNoSources},
		"a flag":     {[]string{"-loud", "t.c"}, errForeignFlags},
		"nosuch":     {[]string{"-with", "nosuch", "t.c"}, errNoDep},
		"fox":        {[]string{"-with", "fox", "t.c"}, errNotCLib},
	} {
		stand := &tools{}
		var out, errs bytes.Buffer
		if err := coverCBy(t.TempDir(), each.args, stand.run, noFetch, &out,
			&errs); !errors.Is(err, each.want) ||
			count.Positive(len(stand.calls)) {
			t.Errorf("%s: %v, calls %+v", name, err, stand.calls)
		}
	}
	for _, export := range []string{"not json", `{"data":[]}`} {
		stand := &tools{said: map[string]string{"xcrun": export}}
		var out, errs bytes.Buffer
		if err := coverCBy(t.TempDir(), []string{"t.c"}, stand.run, noFetch,
			&out, &errs); !errors.Is(err, errCoverJSON) {
			t.Errorf("%q: %v", export, err)
		}
	}
	full := t.TempDir()
	shutFile := os.FileMode(0o600)
	if err := os.WriteFile(filepath.Join(full, "bin"), nil,
		shutFile); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if err := coverCBy(full, []string{"t.c"}, (&tools{}).run, noFetch, &out,
		&errs); err == nil {
		t.Error("a file where bin/ goes was taken")
	}
}

// The words before -- are the sources; those after go to the program.
func TestSplitAtDashes(t *testing.T) {
	t.Parallel()
	before, after := splitAtDashes([]string{"a.c", "--", "-v", "x"})
	if !slices.Equal(before, []string{"a.c"}) ||
		!slices.Equal(after, []string{"-v", "x"}) {
		t.Errorf("%q %q", before, after)
	}
	before, after = splitAtDashes([]string{"a.c", "b.c"})
	if !slices.Equal(before, []string{"a.c", "b.c"}) || after != nil {
		t.Errorf("no dashes: %q %q", before, after)
	}
}

// xccovReport is xccov's JSON for targets, each a list of paths with
// their lines as xccov writes them.
func xccovReport(targets ...map[string]string) string {
	var each []string
	for _, files := range targets {
		var listed []string
		for path, lines := range files {
			listed = append(listed, `{"path":"`+path+`",`+lines+`}`)
		}
		slices.Sort(listed)
		each = append(each, `{"files":[`+strings.Join(listed, ",")+`]}`)
	}
	return `{"targets":[` + strings.Join(each, ",") + `]}`
}

// game cover xcode clears the old result, tests with coverage on, and
// reports the project's files; a file in two targets counts once, at its
// best; test files are left out.
func TestCoverXcodeTestsAndReports(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	old := filepath.Join(dir, "bin", ".xcode", "cover.xcresult", "old")
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(old, openDir); err != nil {
		t.Fatal(err)
	}
	lib := filepath.Join(dir, "Sources/Sign/Sign.swift")
	tests := filepath.Join(dir, "Tests/SignTests/SignTests.swift")
	stand := &tools{said: map[string]string{"xcrun": xccovReport(
		map[string]string{lib: `"coveredLines":4,"executableLines":6`},
		map[string]string{lib: `"coveredLines":6,"executableLines":6`,
			tests: `"coveredLines":3,"executableLines":3`},
	)}}
	var out, errs bytes.Buffer
	err := coverBy(dir, []string{"xcode", "-scheme", "Sign-Package",
		"-configuration", "Debug", "-project", "S.xcodeproj"}, stand.run,
		noFetch, &out, &errs)
	want := []call{
		{dir, "xcodebuild", []string{"-scheme", "Sign-Package",
			"-configuration", "Debug", "-project", "S.xcodeproj",
			"-destination", "platform=macOS", "-derivedDataPath",
			"bin/.xcode", "test", "-enableCodeCoverage", "YES",
			"-resultBundlePath", "bin/.xcode/cover.xcresult"}},
		{dir, "xcrun", []string{"xccov", "view", "--report", "--json",
			"bin/.xcode/cover.xcresult"}},
	}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) {
		t.Errorf("err %v, calls %+v", err, stand.calls)
	}
	if out.String() != " 100.0%  Sources/Sign/Sign.swift (6 of 6)\n"+
		" 100.0%  total (6 of 6)\n" {
		t.Errorf("said %q", out.String())
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the old result: %v", err)
	}
}

// anti-tests: what cover xcode cannot test or read is refused, and a
// failed test run stops before xccov.
func TestCoverXcodeRefusesWhatItCannotMeasure(t *testing.T) {
	t.Parallel()
	for name, each := range map[string]struct {
		args []string
		want error
	}{
		"no scheme": {nil, errNoScheme},
		"a word":    {[]string{"-scheme", "S", "again"}, errXcodeWords},
		"a flag":    {[]string{"-loud"}, errForeignFlags},
		"both": {[]string{"-scheme", "S", "-project", "a.xcodeproj",
			"-workspace", "a.xcworkspace"}, errProjectAndWorkspace},
	} {
		stand := &tools{}
		var out, errs bytes.Buffer
		if err := coverXcodeBy(t.TempDir(), each.args, stand.run, &out,
			&errs); !errors.Is(err, each.want) ||
			count.Positive(len(stand.calls)) {
			t.Errorf("%s: %v, calls %+v", name, err, stand.calls)
		}
	}
	broken := errors.New("broken")
	// the test run and xccov
	steps := countHeld(t, "cover-xcode-steps")
	for at := count.One; at <= steps; at++ {
		stand := &failsAt{at: at, err: broken}
		var out, errs bytes.Buffer
		if err := coverXcodeBy(t.TempDir(), []string{"-scheme", "S"},
			stand.run, &out, &errs); !errors.Is(err, broken) ||
			len(stand.calls) != at {
			t.Errorf("failing at %d: %v, %d calls", at, err,
				len(stand.calls))
		}
	}
	stand := &tools{said: map[string]string{"xcrun": "not json"}}
	var out, errs bytes.Buffer
	if err := coverXcodeBy(t.TempDir(), []string{"-scheme", "S"}, stand.run,
		&out, &errs); !errors.Is(err, errXccovJSON) {
		t.Errorf("not json: %v", err)
	}
}

// cover xcode refuses a bin/ that is a file, an old result it cannot
// clear, and a bin/ it cannot make a folder in.
func TestCoverXcodeRefusesWhereItCannotWrite(t *testing.T) {
	t.Parallel()
	var out, errs bytes.Buffer
	openDir := os.FileMode(0o755)
	shutFile := os.FileMode(0o600)
	readOnly := os.FileMode(0o500)
	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "bin"), nil,
		shutFile); err != nil {
		t.Fatal(err)
	}
	if err := coverXcodeBy(full, []string{"-scheme", "S"}, (&tools{}).run,
		&out, &errs); err == nil {
		t.Error("a file where bin/ goes was taken")
	}
	tree := t.TempDir()
	stuck := filepath.Join(tree, "bin", ".xcode", "cover.xcresult",
		"stuck")
	if err := os.MkdirAll(stuck, openDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuck, "f"), nil,
		shutFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stuck, readOnly); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shutDir{dir: stuck, open: openDir}.reopen)
	if err := coverXcodeBy(tree, []string{"-scheme", "S"}, (&tools{}).run,
		&out, &errs); err == nil {
		t.Error("an old result that could not be cleared was ignored")
	}
	shut := t.TempDir()
	bin := filepath.Join(shut, "bin")
	if err := os.Mkdir(bin, readOnly); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shutDir{dir: bin, open: openDir}.reopen)
	if err := coverXcodeBy(shut, []string{"-scheme", "S"}, (&tools{}).run,
		&out, &errs); err == nil {
		t.Error("a bin/ that takes no folder was used")
	}
}

// A path inside the tree is made relative; one outside is kept.
func TestUnderDir(t *testing.T) {
	t.Parallel()
	if got := underDir("/tree/src/a.c", "/tree"); got != "src/a.c" {
		t.Errorf("inside: %q", got)
	}
	if got := underDir("/usr/include/x.h", "/tree"); got != "/usr/include/x.h" {
		t.Errorf("outside: %q", got)
	}
}
