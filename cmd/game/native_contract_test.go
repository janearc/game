package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/janearc/game/internal/count"
	"go.uber.org/mock/gomock"
)

func TestNativeGoChecksRefuseMissingCommands(t *testing.T) {
	t.Parallel()
	commands := NewMockcommandRunner(gomock.NewController(t))
	if err := aslrGoBy(t.TempDir(), nil, commands.Run, io.Discard); err == nil {
		t.Fatal("ASLR accepted missing command sources")
	}
	if err := leaksGoBy(t.TempDir(), nil, commands.Run, io.Discard); err == nil {
		t.Fatal("leak checking accepted missing command sources")
	}
}

func TestLeaksXcodeRejectsInvalidArgumentsBeforeTools(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"xcode", "-unknown"}, {"xcode", "word"}, {"xcode"},
		{"xcode", "-scheme", "Sample", "-with", "unknown-library"},
	} {
		commands := NewMockcommandRunner(gomock.NewController(t))
		err := leaksBy(t.TempDir(), args, commands.Run, noFetch,
			io.Discard, io.Discard)
		if err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
}

func TestLeaksCRejectsInvalidFlagsBeforeTools(t *testing.T) {
	t.Parallel()
	commands := NewMockcommandRunner(gomock.NewController(t))
	err := leaksCBy(t.TempDir(), []string{"-unknown"}, commands.Run,
		noFetch, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("invalid C flag accepted")
	}
}

func TestLeaksXcodeStopsAtBuildFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("xcode build refused")
	commands.EXPECT().Run(dir, xcodeBuilder, gomock.Any()).Return("", failure)
	err := leaksXcodeBy(dir, []string{"-scheme", "Sample"}, commands.Run,
		noFetch, io.Discard, io.Discard)
	if !errors.Is(err, failure) {
		t.Fatalf("xcode build failure: %v", err)
	}
}

func TestLeaksXcodeStopsAtSanitizerFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("xcode sanitizer refused")
	gomock.InOrder(
		commands.EXPECT().Run(dir, xcodeBuilder, gomock.Any()).Return("", nil),
		commands.EXPECT().Run(dir, xcodeBuilder, gomock.Any()).Return("", failure),
	)
	err := leaksXcodeBy(dir, []string{"-scheme", "Sample"}, commands.Run,
		noFetch, io.Discard, io.Discard)
	if !errors.Is(err, failure) {
		t.Fatalf("xcode sanitizer failure: %v", err)
	}
}

func TestLeaksXcodeRefusesFileInPlaceOfBin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bin"), "occupied")
	commands := NewMockcommandRunner(gomock.NewController(t))
	err := leaksXcodeBy(dir, []string{"-scheme", "Sample"}, commands.Run,
		noFetch, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("file in place of bin accepted")
	}
}

func TestHardenXcodeRejectsInvalidArgumentsBeforeTools(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"xcode", "-unknown"}, {"xcode", "word"}, {"xcode"},
	} {
		commands := NewMockcommandRunner(gomock.NewController(t))
		err := hardenBy(t.TempDir(), args, commands.Run, io.Discard, io.Discard)
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
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("compiler refused")
	commands.EXPECT().Run(dir, "go", "build", "-trimpath", "-o",
		"bin/sample", "./cmd/sample").Return("", failure)
	var out bytes.Buffer
	err := aslrBy(dir, []string{"go"}, commands.Run, noFetch, &out, io.Discard)
	if !errors.Is(err, failure) || out.Len() != count.Nothing {
		t.Fatalf("build failure %v, report %q", err, out.String())
	}
}

func TestASLRGoRefusesFileInPlaceOfBin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cmd", "notes"), "not a command\n")
	write(t, filepath.Join(dir, "bin"), "occupied")
	commands := NewMockcommandRunner(gomock.NewController(t))
	if err := aslrGoBy(dir, nil, commands.Run, io.Discard); err == nil {
		t.Fatal("file in place of bin accepted")
	}
}

func TestASLRRefusesCFlagsBeforeRunningTools(t *testing.T) {
	t.Parallel()
	commands := NewMockcommandRunner(gomock.NewController(t))
	err := aslrCBy(t.TempDir(), []string{"-unknown"}, commands.Run,
		noFetch, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("unknown C flag accepted")
	}
}

func TestASLRXcodePropagatesBuildFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("xcode refused")
	commands.EXPECT().Run(dir, xcodeBuilder, gomock.Any()).Return("", failure)
	err := aslrBy(dir, []string{"xcode", "-scheme", "Sample"}, commands.Run,
		noFetch, io.Discard, io.Discard)
	if !errors.Is(err, failure) {
		t.Fatalf("xcode failure lost: %v", err)
	}
}

func TestASLRBinStopsAtInspectionFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMach(t, dir, "bin/sample")
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("header refused")
	commands.EXPECT().Run(dir, otool, "-hv", "bin/sample").Return("", failure)
	if err := aslrBinBy(dir, commands.Run, io.Discard); !errors.Is(err, failure) {
		t.Fatalf("inspection failure lost: %v", err)
	}
}

func TestBuildCProgramStopsAtLinkFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("link refused")
	commands.EXPECT().Run(dir, cCompiler, gomock.Any()).Return("", failure)
	program, err := buildCProgram(dir, "", []string{"sample.c"}, nil,
		commands.Run, noFetch)
	if !errors.Is(err, failure) || program != "" {
		t.Fatalf("link failure produced %q: %v", program, err)
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
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("compiler refused")
	commands.EXPECT().Run(dir, "go", "build", "-trimpath", "-o",
		"bin/sample", "./cmd/sample").Return("", failure)
	var out bytes.Buffer
	err := leaksBy(dir, []string{"go"}, commands.Run, noFetch, &out, io.Discard)
	if !errors.Is(err, failure) || out.Len() != count.Nothing {
		t.Fatalf("build failure %v, report %q", err, out.String())
	}
}

func TestLeaksGoStopsAtLeakFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "cmd", "sample", "main.go"), "package main\n")
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("allocation leaked")
	gomock.InOrder(
		commands.EXPECT().Run(dir, "go", "build", "-trimpath", "-o",
			"bin/sample", "./cmd/sample").Return("", nil),
		commands.EXPECT().Run(dir, leaksTool, "-q", "--atExit", "--",
			"bin/sample").Return("leak report\n", failure),
	)
	var out bytes.Buffer
	if err := leaksGoBy(dir, nil, commands.Run, &out); !errors.Is(err, failure) {
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
	commands := NewMockcommandRunner(gomock.NewController(t))
	if err := leaksGoBy(dir, nil, commands.Run, io.Discard); err == nil {
		t.Fatal("file in place of bin accepted")
	}
}

func TestLeakBinRefusesMissingBinaries(t *testing.T) {
	t.Parallel()
	commands := NewMockcommandRunner(gomock.NewController(t))
	err := leakBinBy(t.TempDir(), commands.Run, io.Discard)
	if !errors.Is(err, errNoBinaries) {
		t.Fatalf("missing binaries: %v", err)
	}
}

func TestLeakBinStopsAtLeakFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMach(t, dir, "bin/sample")
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("leaks refused")
	commands.EXPECT().Run(dir, leaksTool, "-q", "--atExit", "--",
		"bin/sample").Return("", failure)
	if err := leakBinBy(dir, commands.Run, io.Discard); !errors.Is(err, failure) {
		t.Fatalf("leak failure lost: %v", err)
	}
}

func TestLeakBuildRefusesInvalidSource(t *testing.T) {
	t.Parallel()
	commands := NewMockcommandRunner(gomock.NewController(t))
	program, err := buildLeakProgram(t.TempDir(), "", nil, nil,
		commands.Run, noFetch)
	if !errors.Is(err, errNoSources) || program != "" {
		t.Fatalf("invalid source produced %q: %v", program, err)
	}
}

func TestLeakBuildRefusesUnknownLibrary(t *testing.T) {
	t.Parallel()
	commands := NewMockcommandRunner(gomock.NewController(t))
	program, err := buildLeakProgram(t.TempDir(), "", []string{"sample.c"},
		[]string{"unknown-library"}, commands.Run, noFetch)
	if err == nil || program != "" {
		t.Fatalf("unknown library produced %q: %v", program, err)
	}
}

func TestLeakBuildRefusesFileInPlaceOfBin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bin"), "occupied")
	commands := NewMockcommandRunner(gomock.NewController(t))
	program, err := buildLeakProgram(dir, "", []string{"sample.c"}, nil,
		commands.Run, noFetch)
	if err == nil || program != "" {
		t.Fatalf("unwritable output produced %q: %v", program, err)
	}
}

func TestLeakBuildStopsAtCompilerFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("compiler refused")
	commands.EXPECT().Run(dir, cCompiler, gomock.Any()).Return("", failure)
	program, err := buildLeakProgram(dir, "", []string{"sample.c"}, nil,
		commands.Run, noFetch)
	if !errors.Is(err, failure) || program != "" {
		t.Fatalf("failed compiler produced %q: %v", program, err)
	}
}

func TestHardenPropagatesUnreadableBinaryTree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, os.FileMode(0o755)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bin, count.Nothing); err != nil {
		t.Fatal(err)
	}
	open := os.FileMode(0o755)
	t.Cleanup(shutDir{dir: bin, open: open}.reopen)
	commands := NewMockcommandRunner(gomock.NewController(t))
	if _, err := hardenBeyondSource(dir, commands.Run); err == nil {
		t.Fatal("unreadable binary tree passed hardening")
	}
}
