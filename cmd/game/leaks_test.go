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

// a program that leaks is refused with what leaks said.
func TestLeaksRefusesAProgramThatLeaks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "t"), openDir); err != nil {
		t.Fatal(err)
	}
	report := "Process 1: 1 leak for 32 total leaked bytes.\n"
	stand := &tools{
		said: map[string]string{"leaks": report},
		fail: map[string]error{"leaks": errors.New("leaks: exit status 1")},
	}
	var out bytes.Buffer
	err := leaksGo(dir, nil, stand.run, &out)
	if err == nil || !strings.Contains(err.Error(), "bin/t") {
		t.Errorf("err %v", err)
	}
	if !strings.Contains(out.String(), report) {
		t.Errorf("said %q", out.String())
	}
}

// leaks with no word, or a word it does not know, is refused before any
// tool runs.
func TestLeaksRefusesWhatItCannotCheck(t *testing.T) {
	t.Parallel()
	for name, each := range map[string]struct {
		args []string
		want error
	}{
		"which":   {nil, errLeakLanguage},
		"rust":    {[]string{"rust"}, errLeakLanguage},
		"c":       {[]string{"c", "t.c"}, errLeakLanguage},
		"go word": {[]string{"go", "extra"}, nil},
	} {
		stand := &tools{}
		var out bytes.Buffer
		err := argsLeaksBy(t, t.TempDir(), each.args, stand.run, &out)
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
	err := leaksGo(dir, nil, stand.run, &out)
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
