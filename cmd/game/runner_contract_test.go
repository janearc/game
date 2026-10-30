package main

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/janearc/game/internal/count"
)

// notRun is a runner for a test that must run no tool: a call to it
// fails the test.
func notRun(t *testing.T) runner {
	return func(dir, name string, args ...string) (string, error) {
		t.Helper()
		t.Errorf("a tool was run: %s %v in %s", name, args, dir)
		return "", errors.New("no tool was to be run")
	}
}

// ranStep is one call a test expects of a runner, and what the runner
// answers. anyArgs expects the tool with whatever arguments.
type ranStep struct {
	dir, name string
	args      []string
	anyArgs   bool
	out       string
	err       error
}

// runs is a runner that expects these calls in this order, and fails the
// test for any other call and for any that never comes.
func runs(t *testing.T, steps ...ranStep) runner {
	t.Helper()
	next := count.Nothing
	t.Cleanup(func() {
		if next != len(steps) {
			t.Errorf("%d of %d expected tool runs happened", next,
				len(steps))
		}
	})
	return func(dir, name string, args ...string) (string, error) {
		t.Helper()
		if next >= len(steps) {
			t.Errorf("an unexpected tool run: %s %v in %s", name, args, dir)
			return "", errors.New("no tool was to be run")
		}
		step := steps[next]
		next++
		if dir != step.dir || name != step.name ||
			(!step.anyArgs && !slices.Equal(args, step.args)) {
			t.Errorf("tool run %d: %s %v in %s, want %s %v in %s", next,
				name, args, dir, step.name, step.args, step.dir)
		}
		return step.out, step.err
	}
}

func TestPIEInspectionUsesTheRequestedBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := runs(t, ranStep{dir: dir, name: otool,
		args: []string{"-hv", "bin/sample"}, out: pieHeader})
	var out bytes.Buffer
	if err := checkPIE(commands, dir, "bin/sample", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "aslr: bin/sample is PIE\n" {
		t.Fatalf("inspection result %q", out.String())
	}
}

func TestPIEInspectionPropagatesToolFailureWithoutSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	failure := errors.New("header inspection refused")
	commands := runs(t, ranStep{dir: dir, name: otool,
		args: []string{"-hv", "bin/sample"}, err: failure})
	var out bytes.Buffer
	err := checkPIE(commands, dir, "bin/sample", &out)
	if !errors.Is(err, failure) || out.Len() != count.Nothing {
		t.Fatalf("tool failure %v, report %q", err, out.String())
	}
}

func TestPIEInspectionRejectsMalformedToolOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := runs(t, ranStep{dir: dir, name: otool,
		args: []string{"-hv", "bin/sample"}, out: "not a header"})
	var out bytes.Buffer
	err := checkPIE(commands, dir, "bin/sample", &out)
	if !errors.Is(err, errNoHeader) || out.Len() != count.Nothing {
		t.Fatalf("malformed header %v, report %q", err, out.String())
	}
}

// run keeps the directory and the arguments it is given, and gives the
// output of the tool: what it printed to stdout, not to stderr.
func TestRunKeepsDirectoryArgumentsAndOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	args := []string{"-c", "printf '%s' \"$PWD\"; printf ' error' >&2"}
	actual, err := run(dir, "sh", args...)
	if err != nil || actual != dir {
		t.Fatalf("run gave %q (%v), want %q", actual, err, dir)
	}
}
