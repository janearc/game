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

// asanArgv is the clang words game leaks c builds its sanitizer run with.
func asanArgv(name string, sources []string) []string {
	argv, _, _ := sanitizedArgv(sanitized{name: name, suffix: "asan",
		sdk: "/sdk", sanitize: asanSanitize, sources: sources})
	return argv
}

// leaks c builds with the address sanitizer and runs it, then builds the
// normal program and runs it under leaks, in that order.
func TestLeaksCBuildsRunsAndChecks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stand := &tools{said: map[string]string{"xcrun": "/sdk"}}
	var out, errs bytes.Buffer
	err := leaksCBy(dir, []string{"t.c", "--", "-v"}, stand.run, noFetch,
		&out, &errs)
	want := []call{
		{dir, "xcrun", []string{"--show-sdk-path"}},
		{dir, "cc", asanArgv("", []string{"t.c"})},
		{dir, "bin/t-asan", []string{"-v"}},
		{dir, "cc", slices.Concat(leakFlags, []string{"t.c", "-o", "bin/t"})},
		{dir, "leaks", []string{"-q", "--atExit", "--", "bin/t", "-v"}},
	}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) {
		t.Errorf("err %v, calls %+v", err, stand.calls)
	}
	if !strings.Contains(out.String(), "no leaks in bin/t\n") {
		t.Errorf("said %q", out.String())
	}
}

// a program that leaks is refused with what leaks said.
func TestLeaksRefusesAProgramThatLeaks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	report := "Process 1: 1 leak for 32 total leaked bytes.\n"
	stand := &tools{
		said: map[string]string{"xcrun": "/sdk", "leaks": report},
		fail: map[string]error{"leaks": errors.New("leaks: exit status 1")},
	}
	var out, errs bytes.Buffer
	err := leaksCBy(dir, []string{"t.c"}, stand.run, noFetch, &out, &errs)
	if err == nil || !strings.Contains(err.Error(), "bin/t") {
		t.Errorf("err %v", err)
	}
	if !strings.Contains(out.String(), report) {
		t.Errorf("said %q", out.String())
	}
}

// a sanitizer finding fails the run before leaks is asked.
func TestLeaksStopsWhenTheSanitizerFinds(t *testing.T) {
	t.Parallel()
	broken := errors.New("AddressSanitizer: heap-buffer-overflow")
	dir := t.TempDir()
	stand := &tools{said: map[string]string{"xcrun": "/sdk"},
		fail: map[string]error{"bin/t-asan": broken}}
	var out, errs bytes.Buffer
	err := leaksCBy(dir, []string{"t.c"}, stand.run, noFetch, &out, &errs)
	asks := int(held(t, "leaks-asks-before-leaks"))
	if !errors.Is(err, broken) || len(stand.calls) != asks {
		t.Errorf("err %v, calls %+v", err, stand.calls)
	}
}

// leaks with a word it does not know is refused before any tool runs.
func TestLeaksRefusesWhatItCannotCheck(t *testing.T) {
	t.Parallel()
	for name, each := range map[string]struct {
		args []string
		want error
	}{
		"which":    {nil, errLeakLanguage},
		"rust":     {[]string{"rust"}, errLeakLanguage},
		"c none":   {[]string{"c"}, errNoSources},
		"c header": {[]string{"c", "t.h"}, errNotCOrObjC},
		"go word":  {[]string{"go", "extra"}, nil},
	} {
		stand := &tools{}
		var out, errs bytes.Buffer
		err := leaksBy(t.TempDir(), each.args, stand.run, noFetch, &out, &errs)
		switch name {
		case "go word":
			if err == nil || count.Positive(len(stand.calls)) {
				t.Errorf("go word: %v, calls %+v", err, stand.calls)
			}
		default:
			if !errors.Is(err, each.want) || count.Positive(len(stand.calls)) {
				t.Errorf("%s: %v, calls %+v", name, err, stand.calls)
			}
		}
	}
}

// leaks xcode builds, tests under the sanitizer, then runs each command
// line executable it left under leaks.
func TestLeaksXcodeBuildsTestsAndChecks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMach(t, dir, "bin/Tool")
	stand := &tools{}
	var out, errs bytes.Buffer
	err := leaksXcodeBy(dir, []string{"-scheme", "Tool", "-configuration",
		"Debug"}, stand.run, noFetch, &out, &errs)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, each := range stand.calls {
		names = append(names, each.name)
	}
	if !slices.Contains(names, "xcodebuild") || !slices.Contains(names, "leaks") {
		t.Errorf("calls %+v", stand.calls)
	}
	var test bool
	for _, each := range stand.calls {
		if each.name == "xcodebuild" &&
			slices.Contains(each.args, "test") &&
			slices.Contains(each.args, "-enableAddressSanitizer") {
			test = true
		}
	}
	if !test {
		t.Errorf("no sanitizer test run: %+v", stand.calls)
	}
}

// leaks go builds every cmd/* then runs each under leaks.
func TestLeaksGoBuildsThenRunsEach(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "hello"), openDir); err != nil {
		t.Fatal(err)
	}
	stand := &tools{}
	var out bytes.Buffer
	err := leaksGoBy(dir, nil, stand.run, &out)
	want := []call{
		{dir, "go", []string{"build", "-trimpath", "-o", "bin/hello",
			"./cmd/hello"}},
		{dir, "leaks", []string{"-q", "--atExit", "--", "bin/hello"}},
	}
	if err != nil || !slices.EqualFunc(stand.calls, want, sameCall) {
		t.Errorf("err %v, calls %+v", err, stand.calls)
	}
}

// runLeaks says what leaks said and wraps its failure with the program.
func TestRunLeaksSaysAndWraps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	broken := errors.New("leaks: exit status 1")
	stand := &tools{said: map[string]string{"leaks": "1 leak\n"},
		fail: map[string]error{"leaks": broken}}
	var out bytes.Buffer
	err := runLeaks(stand.run, dir, "bin/t", nil, &out)
	if !errors.Is(err, broken) || !strings.Contains(err.Error(), "bin/t") {
		t.Errorf("err %v", err)
	}
	if out.String() != "1 leak\n" {
		t.Errorf("said %q", out.String())
	}
}
