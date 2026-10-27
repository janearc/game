package main

import (
	"errors"
	"io"
	"path/filepath"
	"testing"

	"go.uber.org/mock/gomock"
)

type failedFetch struct{ err error }

func (get failedFetch) fetch(string) ([]byte, error) { return nil, get.err }

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

type binaryCheck func(string, runner, relativePath) error

func aslrRelativeCheck(dir string, by runner, relative relativePath) error {
	return aslrBinUsing(dir, by, io.Discard, relative)
}

func hardenRelativeCheck(dir string, by runner, relative relativePath) error {
	_, err := hardenMachUsing(dir, by, relative)
	return err
}

func leakRelativeCheck(dir string, by runner, relative relativePath) error {
	return leakBinUsing(dir, by, io.Discard, relative)
}

func TestLinkHardenedStopsAtSigningFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("signing refused")
	gomock.InOrder(
		commands.EXPECT().Run(dir, cCompiler, gomock.Any()).Return("compiled", nil),
		commands.EXPECT().Run(dir, signer, "-s", "-", "-o", "runtime",
			"-f", "bin/sample").Return("", failure),
	)
	argv, program, err := cArgv("", []string{"sample.c"})
	if err != nil {
		t.Fatal(err)
	}
	said, err := linkHardened(dir, argv, program, nil, commands.Run, noFetch)
	if !errors.Is(err, failure) || said != "compiled" {
		t.Fatalf("signing failure %v, compiler output %q", err, said)
	}
}

func TestLeaksCStopsAtNormalBuildFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("normal compiler refused")
	gomock.InOrder(
		commands.EXPECT().Run(dir, "xcrun", "--show-sdk-path").Return("/sdk", nil),
		commands.EXPECT().Run(dir, cCompiler, gomock.Any()).Return("", nil),
		commands.EXPECT().Run(dir, "bin/t-asan").Return("", nil),
		commands.EXPECT().Run(dir, cCompiler, gomock.Any()).Return("", failure),
	)
	err := leaksCBy(dir, []string{"t.c"}, commands.Run, noFetch,
		io.Discard, io.Discard)
	if !errors.Is(err, failure) {
		t.Fatalf("normal build failure lost: %v", err)
	}
}

func TestLeaksXcodeStopsAtDependencyFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("dependency fetch refused")
	get := failedFetch{err: failure}
	err := leaksXcodeBy(dir, []string{"-scheme", "Sample", "-with", "theft"},
		commands.Run, get.fetch, io.Discard, io.Discard)
	if !errors.Is(err, failure) {
		t.Fatalf("dependency failure lost: %v", err)
	}
}

func TestLeakBuildStopsAtDependencyFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("dependency fetch refused")
	get := failedFetch{err: failure}
	program, err := buildLeakProgram(dir, "", []string{"sample.c"},
		[]string{"theft"}, commands.Run, get.fetch)
	if !errors.Is(err, failure) || program != "" {
		t.Fatalf("dependency failure produced %q: %v", program, err)
	}
}

func TestBinaryChecksPropagateRelativePathFailure(t *testing.T) {
	t.Parallel()
	failure := errors.New("relative path refused")
	checks := []struct {
		name  string
		check binaryCheck
	}{
		{"aslr", aslrRelativeCheck},
		{"harden", hardenRelativeCheck},
		{"leaks", leakRelativeCheck},
	}
	for _, each := range checks {
		dir := t.TempDir()
		relative := relativeFailure{base: dir, err: failure}.relative
		writeMach(t, dir, "bin/sample")
		commands := NewMockcommandRunner(gomock.NewController(t))
		if err := each.check(dir, commands.Run, relative); !errors.Is(err, failure) {
			t.Fatalf("%s relative path failure lost: %v", each.name, err)
		}
	}
	finding := &findingMachO{bin: t.TempDir(),
		relative: relativeFailure{err: failure}.relative}
	if err := finding.visit("elsewhere", nil, nil); !errors.Is(err, failure) {
		t.Fatalf("walk relative path failure lost: %v", err)
	}
}
