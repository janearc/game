package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"pgregory.net/rapid"

	"github.com/janearc/game/internal/count"
)

// call is one command a stand-in tool was asked to run.
type call struct {
	dir, name string
	args      []string
}

// tools stands in for the mutation tools: it records every call, and
// answers by the tool's name.
type tools struct {
	calls []call
	said  map[string]string
	fail  map[string]error
}

// run is the stand-in's runner.
func (stand *tools) run(dir, name string, args ...string) (string, error) {
	stand.calls = append(stand.calls, call{dir, name, args})
	return stand.said[name], stand.fail[name]
}

// mutate-c builds the test program with Mull's plugin and the clang it
// was named, then runs Mull's runner over it and passes on what it said.
func TestMutateCBuildsWithMullThenRunsIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stand := &tools{said: map[string]string{"mull-runner": "survived: 0\n"}}
	var out, errs bytes.Buffer
	err := mutateCBy(dir, []string{"-plugin", "/p/mull.so", "-clang",
		"/llvm/clang", "t.c"}, stand.run, &out, &errs)
	compile := slices.Concat([]string{"-g", "-grecord-command-line",
		"-fpass-plugin=/p/mull.so"}, cFlags,
		[]string{"t.c", "-o", "bin/t-mutants"})
	want := []call{{dir, "/llvm/clang", compile},
		{dir, "mull-runner", []string{"bin/t-mutants"}}}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) ||
		out.String() != "survived: 0\n" {
		t.Errorf("err %v, calls %+v, said %q", err, stand.calls, out.String())
	}
}

// sameCall holds two calls alike.
func sameCall(one, other call) bool {
	return one.dir == other.dir && one.name == other.name &&
		slices.Equal(one.args, other.args)
}

// anti-tests: no plugin, a source that is not C, and a tree with no room
// for bin/ are refused before any tool runs; a failed build stops before
// Mull's runner.
func TestMutateCRefusesBeforeItsTools(t *testing.T) {
	t.Parallel()
	stand := &tools{}
	var out, errs bytes.Buffer
	if err := mutateCBy(t.TempDir(), []string{"t.c"}, stand.run, &out,
		&errs); !errors.Is(err, errNoPlugin) {
		t.Errorf("no plugin: %v", err)
	}
	if err := mutateCBy(t.TempDir(), []string{"-plugin", "p", "t.h"},
		stand.run, &out, &errs); !errors.Is(err, errNotC) {
		t.Errorf("a header: %v", err)
	}
	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "bin"), nil,
		os.FileMode(0o600)); err != nil {
		t.Fatal(err)
	}
	if err := mutateCBy(full, []string{"-plugin", "p", "t.c"}, stand.run,
		&out, &errs); err == nil {
		t.Error("a file where bin/ goes was taken")
	}
	if count.Positive(len(stand.calls)) {
		t.Errorf("tools asked: %+v", stand.calls)
	}
	broken := errors.New("clang: t.c:1: error")
	failing := &tools{fail: map[string]error{"clang": broken}}
	if err := mutateCBy(t.TempDir(), []string{"-plugin", "p", "t.c"},
		failing.run, &out, &errs); !errors.Is(err, broken) ||
		len(failing.calls) != count.One {
		t.Errorf("a failed build: %v, calls %+v", err, failing.calls)
	}
}

// mutate-xcode runs Muter in a tree that has its config, with the words
// after -- passed on, and passes on what it said.
func TestMutateXcodeRunsMuterByItsConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "muter.conf.yml"), nil,
		os.FileMode(0o600)); err != nil {
		t.Fatal(err)
	}
	stand := &tools{said: map[string]string{"muter": "score: 100%\n"}}
	var out, errs bytes.Buffer
	err := mutateXcodeBy(dir, []string{"--", "--files-to-mutate", "A.swift"},
		stand.run, &out, &errs)
	want := []call{{dir, "muter", []string{"--files-to-mutate", "A.swift"}}}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) ||
		out.String() != "score: 100%\n" {
		t.Errorf("err %v, calls %+v, said %q", err, stand.calls, out.String())
	}
}

// anti-test: a tree with no muter.conf.yml is refused, and Muter is never
// asked.
func TestMutateXcodeRefusesATreeWithNoConfig(t *testing.T) {
	t.Parallel()
	stand := &tools{}
	var out, errs bytes.Buffer
	if err := mutateXcodeBy(t.TempDir(), nil, stand.run, &out,
		&errs); !errors.Is(err, errNoMuterConfig) || count.Positive(len(stand.calls)) {
		t.Errorf("err %v, calls %+v", err, stand.calls)
	}
}

// mutate-text writes the file's mutants to bin/mutants/FILE, read as -as
// says, then runs the test command against them.
func TestMutateTextWritesMutantsThenTestsThem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.m"), nil, os.FileMode(0o600)); err != nil {
		t.Fatal(err)
	}
	stand := &tools{said: map[string]string{"analyze_mutants": "killed 9\n"}}
	var out, errs bytes.Buffer
	err := mutateTextBy(dir, []string{"-as", "c", "a.m", "--", "make",
		"test"}, stand.run, &out, &errs)
	place := filepath.Join("bin", "mutants", "a.m")
	want := []call{
		{dir, "mutate", []string{"a.m", "c", "--mutantDir", place}},
		{dir, "analyze_mutants", []string{"a.m", "make test",
			"--mutantDir", place}},
	}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) ||
		out.String() != "killed 9\n" {
		t.Errorf("err %v, calls %+v, said %q", err, stand.calls, out.String())
	}
	if info, err := os.Stat(filepath.Join(dir, place)); err != nil ||
		!info.IsDir() {
		t.Errorf("%s: %v", place, err)
	}
}

// anti-tests: no test command, a file that is not there, and no room for
// bin/ are refused before any tool runs; a failed mutate stops before the
// tests.
func TestMutateTextRefusesBeforeItsTools(t *testing.T) {
	t.Parallel()
	stand := &tools{}
	var out, errs bytes.Buffer
	for _, args := range [][]string{{"a.c"}, {"a.c", "make"},
		{"a.c", "--"}} {
		if err := mutateTextBy(t.TempDir(), args, stand.run, &out,
			&errs); !errors.Is(err, errNoTestCommand) {
			t.Errorf("%q: %v", args, err)
		}
	}
	if err := mutateTextBy(t.TempDir(), []string{"gone.c", "--", "make"},
		stand.run, &out, &errs); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing file: %v", err)
	}
	full := t.TempDir()
	for name, body := range map[string]string{"bin": "", "a.c": ""} {
		if err := os.WriteFile(filepath.Join(full, name), []byte(body),
			os.FileMode(0o600)); err != nil {
			t.Fatal(err)
		}
	}
	if err := mutateTextBy(full, []string{"a.c", "--", "make"}, stand.run,
		&out, &errs); err == nil {
		t.Error("a file where bin/ goes was taken")
	}
	if count.Positive(len(stand.calls)) {
		t.Errorf("tools asked: %+v", stand.calls)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.c"), nil, os.FileMode(0o600)); err != nil {
		t.Fatal(err)
	}
	broken := errors.New("mutate: no rules for a.c")
	failing := &tools{fail: map[string]error{"mutate": broken}}
	if err := mutateTextBy(dir, []string{"a.c", "--", "make"}, failing.run,
		&out, &errs); !errors.Is(err, broken) || len(failing.calls) != count.One {
		t.Errorf("a failed mutate: %v, calls %+v", err, failing.calls)
	}
}

// Whatever file and test words are drawn, FILE -- TEST... splits back
// into them.
func TestTextWordsSplitsFileFromTest(t *testing.T) {
	t.Parallel()
	most := countHeld(t, "drawn-most-test-words")
	rapid.Check(t, textWordsProperty{most: most}.check)
}

// textWordsProperty is the property that whatever file and test words
// are drawn, FILE -- TEST... splits back into them.
type textWordsProperty struct{ most int }

// check is the property as rapid runs it.
func (prop textWordsProperty) check(t *rapid.T) {
	word := rapid.StringMatching(`[a-z.]{1,8}`)
	file := word.Draw(t, "file")
	test := rapid.SliceOfN(word, count.One, prop.most).Draw(t, "test")
	args := slices.Concat([]string{file, "--"}, test)
	got, words, err := textWords(args)
	if err != nil || got != file || !slices.Equal(words, test) {
		t.Fatalf("%q -- %q: %q %q %v", file, test, got, words, err)
	}
}

// anti-tests: each mutator's --help is help and a flag it does not take is
// refused; and game mutants hands its named argument to the mutator.
func TestForeignMutatorsTakeTheirFlagsOnly(t *testing.T) {
	t.Parallel()
	stand := &tools{}
	var out, errs bytes.Buffer
	for name, mutator := range foreignMutators {
		if err := mutator(t.TempDir(), []string{"--help"}, stand.run, &out,
			&errs); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("%s --help: %v", name, err)
		}
		if err := mutator(t.TempDir(), []string{"-loud"}, stand.run, &out,
			&errs); !errors.Is(err, errForeignFlags) {
			t.Errorf("%s -loud: %v", name, err)
		}
	}
	if count.Positive(len(stand.calls)) {
		t.Errorf("tools asked: %+v", stand.calls)
	}
	work := repoAt(t.TempDir())
	err := mutantsVerb(work, blankConfig(),
		[]string{"mutate-text", "a.c"})
	if !errors.Is(err, errNoTestCommand) {
		t.Errorf("game mutants mutate-text a.c: %v", err)
	}
}

// Any other first word is game's own Go mutator's: in a tree with no Go,
// it finds nothing to mutate and says so, and succeeds.
func TestMutantsKeepsGoForGame(t *testing.T) {
	t.Parallel()
	work := repoAt(t.TempDir())
	if err := mutantsVerb(work, blankConfig(), nil); err != nil {
		t.Errorf("game mutants in a tree with no Go: %v", err)
	}
}
