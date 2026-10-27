package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
	"go.uber.org/mock/gomock"
)

type failedSelf struct{ err error }

func (failure failedSelf) check() (func(string) *exec.Cmd, error) {
	return nil, failure.err
}

func (failure failedSelf) path() (string, error) { return "", failure.err }

type occupiedCommand struct{}

func (occupiedCommand) command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	if name == "git" {
		cmd.Stdout = io.Discard
	}
	return cmd
}

type restoredStdout struct{ previous *os.File }

func (restore restoredStdout) restore() { os.Stdout = restore.previous }

type closedFile struct{ file *os.File }

func (closed closedFile) close() { closed.file.Close() }

func TestBuildStackPropagatesSelfVerificationFailure(t *testing.T) {
	failure := failedSelf{err: errors.New("executable unavailable")}
	err := buildToUsing(cleaned(t, "root", nil), blankConfig(),
		[]string{"stack"}, io.Discard, false, failure.check)
	if !errors.Is(err, failure.err) {
		t.Fatalf("self verification failure lost: %v", err)
	}
}

func TestSelfVerifyPropagatesExecutableFailure(t *testing.T) {
	failure := failedSelf{err: errors.New("executable unavailable")}
	child, err := selfVerifyUsing(failure.path)
	if !errors.Is(err, failure.err) || child != nil {
		t.Fatalf("selfVerify error %v, child present %t", err, child != nil)
	}
}

func TestExportPropagatesPipeFailure(t *testing.T) {
	dir := t.TempDir()
	err := exportUsing(repoAt(dir), "HEAD", dir, occupiedCommand{}.command)
	if err == nil {
		t.Fatal("export accepted an archive whose stdout was already set")
	}
}

func TestSignGoDoesNothingOutsideDarwin(t *testing.T) {
	t.Parallel()
	commands := NewMockcommandRunner(gomock.NewController(t))
	if err := signGoOn(t.TempDir(), "bin/sample", commands.Run, "linux"); err != nil {
		t.Fatalf("non-darwin signing failed: %v", err)
	}
}

func TestRunStepRunsRelativeProgramFromStepDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stubTool(t, filepath.Join(dir, "bin", "run"), "#!/bin/sh\nprintf ran > ran\n")
	var log bytes.Buffer
	step := gamed(t, "run r :: bin/run").Runs()[count.First].Steps()[count.First]
	if err := runStep(dir, step, &log); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "ran"))
	if err != nil || string(body) != "ran" {
		t.Fatalf("relative program result %q, %v", body, err)
	}
}

func TestPaintedShowsArtOnCharacterDevice(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "art", "sample.ansi"), "art\n")
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, noMode)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closedFile{file: null}.close)
	t.Cleanup(restoredStdout{previous: os.Stdout}.restore)
	os.Stdout = null
	t.Setenv("NO_COLOR", "")
	if !painted(dir, "sample") {
		t.Fatal("character device did not receive available art")
	}
}

func TestStackLintRejectsInvalidRules(t *testing.T) {
	t.Parallel()
	cfg := gamed(t, "lint width wide")
	dir := t.TempDir()
	err := stackLint(repoAt(dir), cfg, "HEAD", nil, false, io.Discard)
	if err == nil {
		t.Fatal("stack lint accepted invalid rules")
	}
}

func TestReleaseChecksPropagatesHardeningFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Symlink("go.mod", filepath.Join(dir, "go.mod")); err != nil {
		t.Fatal(err)
	}
	cfg := gamed(t, "lint harden")
	commands := NewMockcommandRunner(gomock.NewController(t))
	err := releaseChecks(repoAt(dir), cfg, false, commands.Run, io.Discard)
	if err == nil {
		t.Fatal("release accepted an unreadable module")
	}
}

func TestVerifyReportsHardeningFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Symlink("go.mod", filepath.Join(dir, "go.mod")); err != nil {
		t.Fatal(err)
	}
	cfg := gamed(t, "lint harden")
	var out bytes.Buffer
	if err := verify(repoAt(dir), cfg, &out); err == nil {
		t.Fatal("verify accepted an unreadable module")
	}
	if !strings.Contains(out.String(), `"step":"harden","state":"fail"`) {
		t.Fatalf("hardening failure not reported: %q", out.String())
	}
}

func TestVerifyReportsHardeningSuccess(t *testing.T) {
	tools := standTools(t, "semgrep")
	write(t, filepath.Join(tools, "semgrep"),
		"#!/bin/sh\nprintf '{\"results\":[]}'\n")
	dir := t.TempDir()
	cfg := gamed(t, "lint harden")
	var out bytes.Buffer
	if err := verify(repoAt(dir), cfg, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"step":"harden","state":"ok"`) {
		t.Fatalf("hardening success not reported: %q", out.String())
	}
}

func TestSweepBinReportsDisappearingDirectory(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	write(t, filepath.Join(bin, "sample"), "tracked\n")
	tools := t.TempDir()
	stubTool(t, filepath.Join(tools, "git"), "#!/bin/sh\n"+
		"if [ \"$1\" = ls-files ]; then\n"+
		"printf 'bin/sample\\n'\n/bin/rm -rf '"+bin+"'\nfi\n")
	t.Setenv("PATH", tools)
	_, err := sweepBin(repoAt(dir), bin)
	if err == nil || !strings.Contains(err.Error(), ", swept:") {
		t.Fatalf("disappearing bin error %v", err)
	}
}
