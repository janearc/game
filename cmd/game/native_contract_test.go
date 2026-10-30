package main

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

func TestNativeGoChecksRefuseMissingCommands(t *testing.T) {
	t.Parallel()
	commands := notRun(t)
	if err := aslrGo(t.TempDir(), nil, commands, io.Discard); err == nil {
		t.Fatal("ASLR accepted missing command sources")
	}
	if err := leaksGo(t.TempDir(), nil, commands, io.Discard); err == nil {
		t.Fatal("leak checking accepted missing command sources")
	}
}

func TestLeaksRejectsInvalidArgumentsBeforeTools(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"xcode", "-unknown"}, {"xcode", "word"}, {"xcode"}, {"-unknown"},
		{"c", "t.c"}, {"go", "word"},
	} {
		commands := notRun(t)
		err := argsLeaksBy(t, t.TempDir(), args, commands, io.Discard)
		if err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
}

func TestHardenRejectsInvalidArgumentsBeforeTools(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"xcode", "-unknown"}, {"xcode", "word"}, {"xcode"}, {"-unknown"},
	} {
		commands := notRun(t)
		err := argsHardenBy(t, t.TempDir(), args, commands, io.Discard)
		if err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
}

func TestASLRGoStopsAtBuildFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cmd", "sample", "main.go"), "package main\n")
	write(t, filepath.Join(dir, "cmd", "notes"), "not a command\n")
	failure := errors.New("compiler refused")
	commands := runs(t, ranStep{dir: dir, name: "go",
		args: []string{"build", "-trimpath", "-o", "bin/sample",
			"./cmd/sample"}, err: failure})
	var out bytes.Buffer
	err := argsAslrBy(t, dir, []string{"go"}, commands, &out)
	if !errors.Is(err, failure) || out.Len() != count.Nothing {
		t.Fatalf("build failure %v, report %q", err, out.String())
	}
}

func TestASLRGoRefusesFileInPlaceOfBin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cmd", "notes"), "not a command\n")
	write(t, filepath.Join(dir, "bin"), "occupied")
	commands := notRun(t)
	if err := aslrGo(dir, nil, commands, io.Discard); err == nil {
		t.Fatal("file in place of bin accepted")
	}
}

func TestASLRRefusesUnknownFlagsBeforeRunningTools(t *testing.T) {
	t.Parallel()
	commands := notRun(t)
	err := argsAslrBy(t, t.TempDir(), []string{"-unknown"}, commands,
		io.Discard)
	if err == nil {
		t.Fatal("unknown flag accepted")
	}
}

func TestASLRBinStopsAtInspectionFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMach(t, dir, "bin/sample")
	failure := errors.New("header refused")
	commands := runs(t, ranStep{dir: dir, name: otool,
		args: []string{"-hv", "bin/sample"}, err: failure})
	if err := aslrBin(dir, commands, io.Discard, filepath.Rel); !errors.Is(err, failure) {
		t.Fatalf("inspection failure lost: %v", err)
	}
}

func TestASLRBinRefusesMissingBinaries(t *testing.T) {
	t.Parallel()
	commands := notRun(t)
	err := aslrBin(t.TempDir(), commands, io.Discard, filepath.Rel)
	if !errors.Is(err, errNoBinaries) {
		t.Fatalf("missing binaries: %v", err)
	}
}

// relativeFailure is a path arithmetic that fails, for the base given or
// for every base when none is.
type relativeFailure struct {
	base string
	err  error
}

func (failure relativeFailure) relative(base, target string) (string, error) {
	if failure.base == "" || failure.base == base {
		return "", failure.err
	}
	return filepath.Rel(base, target)
}

func TestASLRBinAndTheWalkPropagateRelativePathFailure(t *testing.T) {
	t.Parallel()
	failure := errors.New("relative path refused")
	dir := t.TempDir()
	relative := relativeFailure{base: dir, err: failure}.relative
	writeMach(t, dir, "bin/sample")
	commands := notRun(t)
	if err := aslrBin(dir, commands, io.Discard, relative); !errors.Is(err, failure) {
		t.Fatalf("relative path failure lost: %v", err)
	}
	finding := &findingMachO{bin: t.TempDir(),
		relative: relativeFailure{err: failure}.relative}
	if err := finding.visit("elsewhere", nil, nil); !errors.Is(err, failure) {
		t.Fatalf("walk relative path failure lost: %v", err)
	}
}

func TestVerbsRefuseAWordBeforeAnyTool(t *testing.T) {
	t.Parallel()
	work := repoAt(t.TempDir())
	for _, each := range []struct {
		name string
		do   verb
		want string
	}{
		{"aslr", aslrVerb, "name go"},
		{"leaks", leaksVerb, "name go"},
		{"cover", coverVerb, "takes none"},
	} {
		err := viaPzm(t, each.name, each.do, work, blankConfig(),
			[]string{"rust"})
		if err == nil || !strings.Contains(err.Error(), each.want) {
			t.Errorf("%s rust: %v", each.name, err)
		}
	}
}

func TestMachOInspectionRejectsMissingFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing")
	if isMachO(path) {
		t.Fatal("missing binary identified as Mach-O")
	}
}

func TestMachOWalkIgnoresHiddenFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMach(t, dir, "bin/.hidden")
	if _, err := machOBinaries(dir); !errors.Is(err, errNoBinaries) {
		t.Fatalf("hidden binary was inspected: %v", err)
	}
}

func TestLeaksGoStopsAtBuildFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cmd", "sample", "main.go"), "package main\n")
	write(t, filepath.Join(dir, "cmd", "notes"), "not a command\n")
	failure := errors.New("compiler refused")
	commands := runs(t, ranStep{dir: dir, name: "go",
		args: []string{"build", "-trimpath", "-o", "bin/sample",
			"./cmd/sample"}, err: failure})
	var out bytes.Buffer
	err := argsLeaksBy(t, dir, []string{"go"}, commands, &out)
	if !errors.Is(err, failure) || out.Len() != count.Nothing {
		t.Fatalf("build failure %v, report %q", err, out.String())
	}
}

func TestLeaksGoStopsAtLeakFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cmd", "sample", "main.go"), "package main\n")
	failure := errors.New("allocation leaked")
	commands := runs(t,
		ranStep{dir: dir, name: "go", args: []string{"build", "-trimpath",
			"-o", "bin/sample", "./cmd/sample"}},
		ranStep{dir: dir, name: leaksTool, args: []string{"-q",
			"--atExit", "--", "bin/sample"}, out: "leak report\n",
			err: failure},
	)
	var out bytes.Buffer
	if err := leaksGo(dir, nil, commands, &out); !errors.Is(err, failure) {
		t.Fatalf("leak failure lost: %v", err)
	}
	if out.String() != "leak report\n" {
		t.Fatalf("failure report changed: %q", out.String())
	}
}

func TestLeaksGoRefusesFileInPlaceOfBin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cmd", "notes"), "not a command\n")
	write(t, filepath.Join(dir, "bin"), "occupied")
	commands := notRun(t)
	if err := leaksGo(dir, nil, commands, io.Discard); err == nil {
		t.Fatal("file in place of bin accepted")
	}
}
