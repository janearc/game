package main

// game leaks: what a Go program holds outside the Go heap.
//
// A Go program is run under Apple's leaks tool, where it catches what the
// program held outside the Go heap, in cgo and the system frameworks; the
// Go heap itself is measured with pprof, not leaks.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/puzzlemouth/pzm"
)

// leaksTool is the program game runs a binary under to find its leaks.
const leaksTool = "leaks"

// errLeakLanguage is leaks with no word, or a word that is not go.
var errLeakLanguage = errors.New("leaks: name go")

// errLeakWords is a word game leaks does not take.
var errLeakWords = errors.New("leaks: takes flags only")

// leaksVerb is game leaks go.
func leaksVerb(here repo.Repo, _ config.Config, call pzm.Call) error {
	return leaks(here.Dir(), call.Args(), run, call.Out())
}

// leaks is leaksVerb in dir, building with by.
func leaks(dir string, args []string, by runner, out io.Writer) error {
	word, rest, _ := verbWord(args)
	if word == "go" {
		return leaksGo(dir, rest, by, out)
	}
	return errLeakLanguage
}

// leaksGo is game leaks go: every cmd/* built into bin/, then each run
// under leaks, which catches what a program held outside the Go heap.
func leaksGo(dir string, args []string, by runner, out io.Writer) error {
	if word, _, said := verbWord(args); said {
		return fmt.Errorf("%w; %q is a word", errLeakWords, word)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "cmd"))
	if err != nil {
		return fmt.Errorf("leaks go: no cmd/ directory to build")
	}
	err = os.MkdirAll(filepath.Join(dir, "bin"), folderMode)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		program := filepath.Join("bin", entry.Name())
		if _, err := by(dir, "go", "build", "-trimpath", "-o", program,
			"./cmd/"+entry.Name()); err != nil {
			return err
		}
		if err := runLeaks(by, dir, program, nil, out); err != nil {
			return err
		}
	}
	return nil
}

// runLeaks runs program with args under leaks, says what leaks said on
// out, and refuses a run that reported any leaks.
func runLeaks(by runner, dir, program string, args []string,
	out io.Writer) error {
	words := append([]string{"-q", "--atExit", "--", program}, args...)
	said, err := by(dir, leaksTool, words...)
	fmt.Fprint(out, said)
	if err != nil {
		return fmt.Errorf("leaks: %s: %w", program, err)
	}
	fmt.Fprintf(out, "no leaks in %s\n", program)
	return nil
}
