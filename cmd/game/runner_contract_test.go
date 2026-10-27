package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/janearc/game/internal/count"
	"go.uber.org/mock/gomock"
)

type commandRunner interface {
	Run(dir, name string, args ...string) (string, error)
}

func TestPIEInspectionUsesTheRequestedBinary(t *testing.T) {
	t.Parallel()
	commands := NewMockcommandRunner(gomock.NewController(t))
	dir := t.TempDir()
	commands.EXPECT().Run(dir, otool, "-hv", "bin/sample").Return(pieHeader, nil)
	var boundary commandRunner = commands
	var out bytes.Buffer
	if err := checkPIE(boundary.Run, dir, "bin/sample", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "aslr: bin/sample is PIE\n" {
		t.Fatalf("inspection result %q", out.String())
	}
}

func TestPIEInspectionPropagatesToolFailureWithoutSuccess(t *testing.T) {
	t.Parallel()
	commands := NewMockcommandRunner(gomock.NewController(t))
	dir := t.TempDir()
	failure := errors.New("header inspection refused")
	commands.EXPECT().Run(dir, otool, "-hv", "bin/sample").Return("", failure)
	var out bytes.Buffer
	err := checkPIE(commands.Run, dir, "bin/sample", &out)
	if !errors.Is(err, failure) || out.Len() != count.Nothing {
		t.Fatalf("tool failure %v, report %q", err, out.String())
	}
}

func TestPIEInspectionRejectsMalformedToolOutput(t *testing.T) {
	t.Parallel()
	commands := NewMockcommandRunner(gomock.NewController(t))
	dir := t.TempDir()
	commands.EXPECT().Run(dir, otool, "-hv", "bin/sample").Return("not a header", nil)
	var out bytes.Buffer
	err := checkPIE(commands.Run, dir, "bin/sample", &out)
	if !errors.Is(err, errNoHeader) || out.Len() != count.Nothing {
		t.Fatalf("malformed header %v, report %q", err, out.String())
	}
}

func TestRunnerAdapterKeepsDirectoryArgumentsAndOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	args := []string{"-c", "printf '%s' \"$PWD\"; printf ' error' >&2"}
	commands.EXPECT().Run(dir, "sh", "-c", args[count.One]).Return(dir, nil)
	actual, actualErr := run(dir, "sh", args...)
	modeled, modeledErr := commands.Run(dir, "sh", args...)
	if actualErr != nil || modeledErr != nil || actual != modeled {
		t.Fatalf("runner %q/%v, mock %q/%v", actual, actualErr, modeled, modeledErr)
	}
}
