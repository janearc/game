package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// globs stands in for filepath.Glob, answering patterns from a map.
type globs map[string][]string

// glob is globs' globber.
func (found globs) glob(pattern string) ([]string, error) {
	return found[pattern], nil
}

// The first libFuzzer runtime found is used; none found is nothing.
func TestFuzzRuntimeTakesTheFirstFound(t *testing.T) {
	t.Parallel()
	// the second place looked is found: the first is not taken for it
	second := fuzzRuntimes[count.Next(count.First)]
	found := globs{second: {"/brew/llvm/libfuzzer.a"}}
	if got := fuzzRuntime(found.glob); got != "/brew/llvm/libfuzzer.a" {
		t.Errorf("got %q", got)
	}
	if got := fuzzRuntime(globs{}.glob); got != "" {
		t.Errorf("none: got %q", got)
	}
}

// A C build drops -O2 for the sanitizer's -O1 and puts each word in its
// place; a build with Objective-C uses objcFlags and Foundation.
func TestSanitizedArgvPutsEachWordInItsPlace(t *testing.T) {
	t.Parallel()
	ask := sanitized{suffix: "fuzz", sdk: "/sdk", sanitize: []string{"-S"},
		sources: []string{"src/t.c"}, add: linked{includes: []string{"-I",
			"inc"}, objects: []string{"x.o"}, libs: []string{"-lm"}}}
	argv, program, err := sanitizedArgv(ask)
	want := slices.Concat(withoutOptimising(cFlags), []string{"-g", "-O1",
		"-S", "-isysroot", "/sdk", "-I", "inc", "src/t.c", "x.o", "-lm",
		"-o", "bin/t-fuzz"})
	if err != nil || program != "bin/t-fuzz" || !slices.Equal(argv, want) {
		t.Errorf("c: %q %q %v", argv, program, err)
	}
	ask.sources, ask.name = []string{"a.c", "b.m"}, "both"
	argv, program, err = sanitizedArgv(ask)
	if err != nil || program != "bin/both-fuzz" ||
		!slices.Equal(argv[:len(objcFlags)], objcFlags) ||
		!slices.Contains(argv, "Foundation") {
		t.Errorf("objc: %q %q %v", argv, program, err)
	}
}

// anti-tests: no sources, a source neither C nor Objective-C, and a name
// that would leave bin/ are refused.
func TestSanitizedArgvRefusesWhatItCannotBuild(t *testing.T) {
	t.Parallel()
	for name, each := range map[string]struct {
		ask  sanitized
		want error
	}{
		"none":   {sanitized{}, errNoSources},
		"header": {sanitized{sources: []string{"a.h"}}, errNotCOrObjC},
		"up":     {sanitized{name: "../x", sources: []string{"a.c"}}, errBadName},
	} {
		if _, _, err := sanitizedArgv(each.ask); !errors.Is(err, each.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// -O2 is taken out, and nothing else.
func TestWithoutOptimisingTakesOutO2(t *testing.T) {
	t.Parallel()
	if got := withoutOptimising([]string{"-a", "-O2", "-b"}); !slices.Equal(got,
		[]string{"-a", "-b"}) {
		t.Errorf("got %q", got)
	}
}

// fuzz c asks for the SDK, builds with cc, and runs the fuzzer for its
// time with its corpus and where to put what it finds.
func TestFuzzCBuildsAndRunsTheFuzzer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stand := &tools{said: map[string]string{"xcrun": "/sdk\n"}}
	var out, errs bytes.Buffer
	err := fuzzBy(dir, []string{"c", "-time", "5", "-runtime", "/fz.a",
		"t.c"}, stand.run, noFetch, globs{}.glob, &out, &errs)
	argv, _, _ := sanitizedArgv(sanitized{suffix: "fuzz", sdk: "/sdk",
		sanitize: fuzzSanitize, sources: []string{"t.c"},
		add: linked{libs: []string{"/fz.a", "-lc++"}}})
	want := []call{
		{dir, "xcrun", []string{"--show-sdk-path"}},
		{dir, "cc", argv},
		{dir, "bin/t-fuzz", []string{"-max_total_time=5",
			"-artifact_prefix=bin/.fuzz/t-fuzz/", "bin/.fuzz/t-fuzz/corpus"}},
	}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) ||
		out.String() != "fuzzed bin/t-fuzz for 5 s: nothing found\n" {
		t.Errorf("err %v, calls %+v, said %q", err, stand.calls, out.String())
	}
}

// anti-tests: what fuzz cannot start is refused before any tool runs.
func TestFuzzRefusesBeforeItsTools(t *testing.T) {
	t.Parallel()
	for name, each := range map[string]struct {
		args []string
		want error
	}{
		"which":      {nil, errWhichLanguage},
		"rust":       {[]string{"rust"}, errWhichLanguage},
		"swift":      {[]string{"xcode"}, errSwiftFuzz},
		"no runtime": {[]string{"c", "t.c"}, errNoFuzzRuntime},
		"a flag":     {[]string{"c", "-loud"}, errForeignFlags},
		"no sources": {[]string{"c", "-runtime", "/fz.a"}, errNoSources},
		"nosuch": {[]string{"c", "-runtime", "/fz.a", "-with", "nosuch",
			"t.c"}, errNoDep},
		"fox": {[]string{"c", "-runtime", "/fz.a", "-with", "fox", "t.c"},
			errNotCLib},
	} {
		stand := &tools{}
		var out, errs bytes.Buffer
		if err := fuzzBy(t.TempDir(), each.args, stand.run, noFetch,
			globs{}.glob, &out, &errs); !errors.Is(err, each.want) ||
			count.Positive(len(stand.calls)) {
			t.Errorf("%s: %v, calls %+v", name, err, stand.calls)
		}
	}
}

// anti-tests: each step's failure stops what follows it, and a fuzzer
// that finds something says where it put it.
func TestFuzzStopsAtTheFirstFailure(t *testing.T) {
	t.Parallel()
	broken := errors.New("broken")
	// the sdk, the build and the run
	steps := countHeld(t, "fuzz-c-steps")
	for at := count.One; at <= steps; at++ {
		stand := &failsAt{at: at, err: broken}
		var out, errs bytes.Buffer
		err := fuzzBy(t.TempDir(), []string{"c", "-runtime", "/fz.a", "t.c"},
			stand.run, noFetch, globs{}.glob, &out, &errs)
		if !errors.Is(err, broken) || len(stand.calls) != at {
			t.Errorf("failing at %d: %v, %d calls", at, err, len(stand.calls))
		}
		if at == steps && !strings.Contains(err.Error(), "bin/.fuzz/t-fuzz") {
			t.Errorf("a find does not say where: %v", err)
		}
	}
	openDir := os.FileMode(0o755)
	shutFile := os.FileMode(0o600)
	for _, blocked := range []string{"bin", filepath.Join("bin", ".fuzz")} {
		dir := t.TempDir()
		if blocked != "bin" {
			if err := os.Mkdir(filepath.Join(dir, "bin"), openDir); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, blocked), nil,
			shutFile); err != nil {
			t.Fatal(err)
		}
		var out, errs bytes.Buffer
		if err := fuzzBy(dir, []string{"c", "-runtime", "/fz.a", "t.c"},
			(&tools{}).run, noFetch, globs{}.glob, &out, &errs); err == nil {
			t.Errorf("a file at %s was taken for a folder", blocked)
		}
	}
	work := repoAt(t.TempDir())
	if err := fuzzVerb(work, blankConfig(),
		[]string{"xcode"}); !errors.Is(err, errSwiftFuzz) {
		t.Errorf("game fuzz xcode: %v", err)
	}
}

// race c builds with the thread sanitizer and runs the program with the
// words after --; race xcode tests with the thread sanitizer on.
func TestRaceBuildsAndRuns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stand := &tools{said: map[string]string{"xcrun": "/sdk"}}
	var out, errs bytes.Buffer
	err := raceBy(dir, []string{"c", "-o", "r", "t.c", "--", "-v"},
		stand.run, noFetch, &out, &errs)
	argv, _, _ := sanitizedArgv(sanitized{name: "r", suffix: "race",
		sdk: "/sdk", sanitize: raceSanitize, sources: []string{"t.c"}})
	want := []call{{dir, "xcrun", []string{"--show-sdk-path"}},
		{dir, "cc", argv}, {dir, "bin/r-race", []string{"-v"}}}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) ||
		out.String() != "no race in bin/r-race\n" {
		t.Errorf("c: err %v, calls %+v, said %q", err, stand.calls,
			out.String())
	}
	stand, out = &tools{}, bytes.Buffer{}
	err = raceBy(dir, []string{"xcode", "-scheme", "S", "-configuration",
		"Debug", "-workspace", "W.xcworkspace"}, stand.run, noFetch, &out, &errs)
	want = []call{{dir, "xcodebuild", []string{"-scheme", "S",
		"-configuration", "Debug", "-workspace", "W.xcworkspace",
		"-destination", "platform=macOS", "-derivedDataPath", "bin/.xcode",
		"test", "-enableThreadSanitizer", "YES"}}}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) ||
		out.String() != "no race in S's tests\n" {
		t.Errorf("xcode: err %v, calls %+v, said %q", err, stand.calls,
			out.String())
	}
}

// anti-tests: a race found, a build that fails, and what race cannot
// start are each refused.
func TestRaceRefusesWhatItCannotPass(t *testing.T) {
	t.Parallel()
	raced := errors.New("WARNING: ThreadSanitizer: data race")
	// a c race is found on its run, after the sdk and the build; an
	// xcode one on its test, the one call
	runs := []struct {
		at   int
		args []string
	}{
		{countHeld(t, "fuzz-c-steps"), []string{"c", "t.c"}},
		{count.One, []string{"xcode", "-scheme", "S"}},
	}
	for _, each := range runs {
		stand := &failsAt{at: each.at, err: raced}
		var out, errs bytes.Buffer
		if err := raceBy(t.TempDir(), each.args, stand.run, noFetch, &out,
			&errs); !errors.Is(err, raced) || count.Positive(out.Len()) {
			t.Errorf("%q: %v, said %q", each.args, err, out.String())
		}
	}
	for name, each := range map[string]struct {
		args []string
		want error
	}{
		"which":      {nil, errWhichLanguage},
		"c flag":     {[]string{"c", "-loud"}, errForeignFlags},
		"c none":     {[]string{"c"}, errNoSources},
		"x flag":     {[]string{"xcode", "-loud"}, errForeignFlags},
		"x word":     {[]string{"xcode", "-scheme", "S", "w"}, errXcodeWords},
		"x noscheme": {[]string{"xcode"}, errNoScheme},
	} {
		stand := &tools{}
		var out, errs bytes.Buffer
		if err := raceBy(t.TempDir(), each.args, stand.run, noFetch, &out,
			&errs); !errors.Is(err, each.want) ||
			count.Positive(len(stand.calls)) {
			t.Errorf("%s: %v, calls %+v", name, err, stand.calls)
		}
	}
	work := repoAt(t.TempDir())
	if err := raceVerb(work, blankConfig(),
		nil); !errors.Is(err, errWhichLanguage) {
		t.Errorf("game race: %v", err)
	}
}

// -with may be given more than once, in order.
func TestLibFlagCollectsEachName(t *testing.T) {
	t.Parallel()
	flags := flag.NewFlagSet("t", flag.ContinueOnError)
	with := libFlag(flags)
	if err := flags.Parse([]string{"-with", "a", "-with", "b"}); err != nil ||
		!slices.Equal(*with, []string{"a", "b"}) {
		t.Errorf("%q %v", *with, err)
	}
}
