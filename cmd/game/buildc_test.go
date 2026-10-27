package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/janearc/game/internal/count"
)

// cc is what a test's stand-in compiler was asked, and what it answers.
type cc struct {
	dir, name string
	args      []string
	asked     int
	said      string
	err       error
	then      [][]string // every call after the first, name first
}

// run is the stand-in's runner. the first call is the compiler's and is
// kept whole; what follows it, the signing, is kept in then.
func (compiler *cc) run(dir, name string, args ...string) (string, error) {
	if count.Positive(compiler.asked) {
		call := append([]string{name}, args...)
		compiler.then = append(compiler.then, call)
		compiler.asked++
		return "", compiler.err
	}
	compiler.dir, compiler.name, compiler.args = dir, name, args
	compiler.asked++
	return compiler.said, compiler.err
}

// output is the -o and the program at the end of an argv.
func output(argv []string) []string {
	return argv[len(argv)-len(outputPair):]
}

// outputPair is what ends every argv: the flag and the program, two
// words as cc has defined -o.
var outputPair = []string{"-o", "program"}

// With no -o, the program is named for its first source.
func TestCArgvNamesTheProgramForItsFirstSource(t *testing.T) {
	t.Parallel()
	argv, program, err := cArgv("", []string{"src/groups.c", "util.c"})
	want := slices.Concat(cFlags,
		[]string{"src/groups.c", "util.c", "-o", "bin/groups"})
	if err != nil || program != "bin/groups" || !slices.Equal(argv, want) {
		t.Errorf("argv %q, program %q, err %v", argv, program, err)
	}
}

// -o names the program.
func TestCArgvTakesItsNameFromO(t *testing.T) {
	t.Parallel()
	argv, program, err := cArgv("probe", []string{"a.c"})
	if err != nil || program != "bin/probe" ||
		!slices.Equal(output(argv), []string{"-o", "bin/probe"}) {
		t.Errorf("argv %q, program %q, err %v", argv, program, err)
	}
}

// Every source is kept, in order, after the flags, and the output is last.
func TestCArgvKeepsEverySourceInOrder(t *testing.T) {
	t.Parallel()
	most := countHeld(t, "drawn-most-sources")
	rapid.Check(t, sourcesProperty{most: most}.check)
}

// sourcesProperty is the property that every source is kept, in
// order, after the flags, and the output last.
type sourcesProperty struct{ most int }

// check is the property as rapid runs it.
func (prop sourcesProperty) check(t *rapid.T) {
	names := rapid.SliceOfN(rapid.StringMatching(`[a-z]{1,8}`),
		count.One, prop.most).Draw(t, "names")
	sources := make([]string, len(names))
	for place, name := range names {
		sources[place] = name + ".c"
	}
	argv, program, err := cArgv("", sources)
	flags := len(cFlags)
	if err != nil || program != "bin/"+names[count.First] ||
		!slices.Equal(argv[:flags], cFlags) ||
		!slices.Equal(argv[flags:flags+len(sources)], sources) ||
		!slices.Equal(output(argv), []string{"-o", program}) {
		t.Fatalf("sources %q: argv %q, program %q, err %v",
			sources, argv, program, err)
	}
}

// anti-tests: nothing to build, a source that is not C, and a name that
// would leave bin/ are each refused.
func TestCArgvRefusesWhatItCannotBuild(t *testing.T) {
	t.Parallel()
	if _, _, err := cArgv("", nil); !errors.Is(err, errNoSources) {
		t.Errorf("no sources: %v", err)
	}
	if _, _, err := cArgv("", []string{"a.c", "a.h"}); !errors.Is(err,
		errNotC) || !strings.Contains(err.Error(), "a.h") {
		t.Errorf("a header: %v", err)
	}
	for _, name := range []string{"..", ".", "../up", "bin/deeper"} {
		if _, _, err := cArgv(name, []string{"a.c"}); !errors.Is(err,
			errBadName) {
			t.Errorf("-o %q: %v", name, err)
		}
	}
}

// build-c runs cc in the tree with cArgv's words, says what cc said, then
// what it built, and makes bin/ first.
func TestBuildCAsksCCAndSaysWhatItBuilt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	compiler := &cc{said: "a warning cc chose to print\n"}
	var out, errs bytes.Buffer
	err := buildCBy(dir, []string{"-o", "probe", "a.c"}, compiler.run, noFetch,
		&out, &errs)
	argv, _, _ := cArgv("probe", []string{"a.c"})
	want := sealed(argv)
	if err != nil || compiler.dir != dir || compiler.name != "cc" ||
		!slices.Equal(compiler.args, want) {
		t.Errorf("err %v; cc asked in %q: %s %q", err, compiler.dir,
			compiler.name, compiler.args)
	}
	sign := [][]string{{"codesign", "-s", "-", "-o", "runtime", "-f",
		"bin/probe"}}
	if !reflect.DeepEqual(compiler.then, sign) {
		t.Errorf("after cc: %q", compiler.then)
	}
	if out.String() != "a warning cc chose to print\nbuilt bin/probe\n" {
		t.Errorf("said %q", out.String())
	}
	if info, err := os.Stat(filepath.Join(dir, "bin")); err != nil ||
		!info.IsDir() {
		t.Errorf("bin/: %v", err)
	}
}

// anti-test: when cc fails, its error comes back and nothing is said to be
// built.
func TestBuildCSaysNothingBuiltWhenCCFails(t *testing.T) {
	t.Parallel()
	failed := errors.New("cc: a.c:1: error")
	compiler := &cc{err: failed}
	var out, errs bytes.Buffer
	err := buildCBy(t.TempDir(), []string{"a.c"}, compiler.run, noFetch, &out, &errs)
	if !errors.Is(err, failed) || strings.Contains(out.String(), "built") {
		t.Errorf("err %v, said %q", err, out.String())
	}
}

// anti-test: a tree where bin/ cannot be made is refused before cc runs.
func TestBuildCRefusesATreeWithNoRoomForBin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	shutFile := os.FileMode(0o600)
	if err := os.WriteFile(filepath.Join(dir, "bin"), nil, shutFile); err != nil {
		t.Fatal(err)
	}
	compiler := &cc{}
	var out, errs bytes.Buffer
	err := buildCBy(dir, []string{"a.c"}, compiler.run, noFetch, &out, &errs)
	if err == nil || count.Positive(compiler.asked) {
		t.Errorf("err %v, cc asked %d times", err, compiler.asked)
	}
}

// anti-tests: --help is help, a flag it does not know is refused, and
// sources it cannot build are refused; cc is never asked.
func TestBuildCRefusesBeforeAskingCC(t *testing.T) {
	t.Parallel()
	compiler := &cc{}
	var out, errs bytes.Buffer
	if err := buildCBy(t.TempDir(), []string{"--help"}, compiler.run, noFetch,
		&out, &errs); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("--help: %v", err)
	}
	if err := buildCBy(t.TempDir(), []string{"-loud", "a.c"},
		compiler.run, noFetch, &out, &errs); !errors.Is(err, errCFlags) {
		t.Errorf("-loud: %v", err)
	}
	if err := buildCBy(t.TempDir(), nil, compiler.run, noFetch, &out,
		&errs); !errors.Is(err, errNoSources) {
		t.Errorf("no sources: %v", err)
	}
	if count.Positive(compiler.asked) {
		t.Errorf("cc asked %d times", compiler.asked)
	}
}

// End to end with the system's cc: a real program is built into bin/ and
// runs.
func TestBuildCBuildsARealProgram(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skip("no cc on this machine")
	}
	dir := t.TempDir()
	hello := "#include <stdio.h>\nint main(void) { puts(\"hello\"); " +
		"return 0; }\n"
	shutFile := os.FileMode(0o600)
	if err := os.WriteFile(filepath.Join(dir, "hello.c"), []byte(hello),
		shutFile); err != nil {
		t.Fatal(err)
	}
	if err := buildCVerb(repoAt(dir), blankConfig(),
		[]string{"hello.c"}); err != nil {
		t.Fatal(err)
	}
	said, err := exec.Command(filepath.Join(dir, "bin", "hello")).Output()
	if err != nil || string(said) != "hello\n" {
		t.Errorf("bin/hello said %q, %v", said, err)
	}
}
