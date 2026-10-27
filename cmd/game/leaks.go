package main

// game leaks: memory errors and leaks in the programs game builds.
//
// A C or Objective-C program (.c, .m) is built once under the address
// sanitizer and run to catch the memory errors, then built without
// optimising and run under Apple's leaks tool to catch what it leaked.
//
// An Xcode scheme is tested under the address sanitizer, its products
// built into bin/, and any command-line executable it left there run
// under leaks.
//
// A Go program is run under leaks too, where it catches what the
// program held outside the Go heap, in cgo and the system frameworks;
// the Go heap itself is measured with pprof, not leaks.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
)

// leaksTool is the program game runs a binary under to find its leaks.
const leaksTool = "leaks"

// asanSanitize is what a memory-error build is compiled with: the address
// sanitizer alone. On macOS the address sanitizer catches what is out of
// bounds and what is used after it is freed; leaks has the leaks.
var asanSanitize = []string{"-fsanitize=address"}

// leakFlags are what a program run under leaks is built with: no
// optimising, so dead code cannot erase the allocation that would have
// leaked, and frame pointers kept so the report names its source line.
var leakFlags = []string{"-std=c11", "-O0", "-g", "-fno-omit-frame-pointer",
	"-Wall", "-Wextra", "-Werror", "-pedantic"}

// errLeakLanguage is leaks with a word that is not c, xcode or go.
var errLeakLanguage = errors.New("leaks: name c, xcode or go")

// errLeakWords is a word game leaks does not take.
var errLeakWords = errors.New("leaks: takes flags only")

// leaksVerb is game leaks c ...; leaks xcode ...; leaks go.
func leaksVerb(here repo.Repo, _ config.Config, args []string) error {
	return leaksBy(here.Dir(), args, run, httpFetch, os.Stdout, os.Stderr)
}

// leaksBy is leaksVerb in dir, building with by and fetching with get.
func leaksBy(dir string, args []string, by runner, get fetcher, out,
	errs io.Writer) error {
	word, rest, _ := verbWord(args)
	switch word {
	case "c":
		return leaksCBy(dir, rest, by, get, out, errs)
	case "xcode":
		return leaksXcodeBy(dir, rest, by, get, out, errs)
	case "go":
		return leaksGoBy(dir, rest, by, out)
	}
	return fmt.Errorf("%w", errLeakLanguage)
}

// leaksCBy is game leaks c, as
//
//	game leaks c [-o NAME] [-with LIB] SOURCE... [-- ARG...]
//
// the sources built under the address sanitizer and run with the
// arguments, then built without optimising and run under leaks. The
// program is the tests: it exits 0 when they pass.
func leaksCBy(dir string, args []string, by runner, get fetcher, out,
	errs io.Writer) error {
	fs := flag.NewFlagSet("leaks c", flag.ContinueOnError)
	fs.SetOutput(errs)
	name := fs.String("o", "",
		"the program's name; the first source's, by default")
	with := libFlag(fs)
	if err := parsedForeign(fs, args); err != nil {
		return err
	}
	sources, programArgs := splitAtDashes(fs.Args())
	asked := sanitized{name: *name, suffix: "asan", sanitize: asanSanitize,
		sources: sources}
	asan, err := sanitizedBuild(dir, asked, *with, by, get)
	if err != nil {
		return err
	}
	if _, err := by(dir, asan, programArgs...); err != nil {
		return fmt.Errorf("leaks: %w", err)
	}
	normal, err := buildLeakProgram(dir, *name, sources, *with, by, get)
	if err != nil {
		return err
	}
	return runLeaks(by, dir, normal, programArgs, out)
}

// leaksXcodeBy is game leaks xcode -scheme S [-with LIB]: the scheme's
// tests run under the address sanitizer, its products built into bin/,
// then each command-line executable it left there run under leaks.
func leaksXcodeBy(dir string, args []string, by runner, get fetcher,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("leaks xcode", flag.ContinueOnError)
	fs.SetOutput(errs)
	var ask xcodeAsk
	fs.StringVar(&ask.scheme, "scheme", "", "the scheme to test")
	fs.StringVar(&ask.configuration, "configuration", "",
		"the build configuration; the scheme's test one, by default")
	fs.StringVar(&ask.project, "project", "",
		"an .xcodeproj, if not the one here")
	fs.StringVar(&ask.workspace, "workspace", "",
		"an .xcworkspace to test in")
	fs.StringVar(&ask.destination, "destination", "platform=macOS",
		"where it is tested")
	with := libFlag(fs)
	if err := parsedForeign(fs, args); err != nil {
		return err
	}
	if word, _, said := verbWord(fs.Args()); said {
		return fmt.Errorf("%w; %q is a word", errLeakWords, word)
	}
	if err := checkXcodeAsk(ask); err != nil {
		return err
	}
	libs, err := depsFor(*with)
	if err != nil {
		return err
	}
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, folderMode); err != nil {
		return err
	}
	if err := fetchAll(dir, libs, get, out); err != nil {
		return err
	}
	build := xcodeBuildArgv(ask, bin)
	if _, err := by(dir, xcodeBuilder, build...); err != nil {
		return err
	}
	if _, err := by(dir, xcodeBuilder, leaksXcodeArgv(ask)...); err != nil {
		return err
	}
	return leakBinBy(dir, by, out)
}

// leaksXcodeArgv is xcodebuild's words for testing ask with the address
// sanitizer on.
func leaksXcodeArgv(ask xcodeAsk) []string {
	argv := xcodeNamedArgv(ask)
	return append(argv,
		"-destination", ask.destination,
		"-derivedDataPath", xcodeDerived,
		"test", "-enableAddressSanitizer", "YES")
}

// leaksGoBy is game leaks go: every cmd/* built into bin/, then each run
// under leaks, which catches what a program held outside the Go heap.
func leaksGoBy(dir string, args []string, by runner, out io.Writer) error {
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

// leakBinBy runs each command-line executable already in bin/ under leaks.
func leakBinBy(dir string, by runner, out io.Writer) error {
	return leakBinUsing(dir, by, out, filepath.Rel)
}

// leakBinUsing is leakBinBy with the path arithmetic given.
func leakBinUsing(
	dir string, by runner, out io.Writer, relative relativePath,
) error {
	bins, err := machOBinaries(dir)
	if err != nil {
		return err
	}
	for _, program := range bins {
		rel, err := relative(dir, program)
		if err != nil {
			return err
		}
		slashed := filepath.ToSlash(rel)
		if !strings.Contains(slashed, ".app/") &&
			!strings.Contains(slashed, ".framework/") {
			if err := runLeaks(by, dir, rel, nil, out); err != nil {
				return err
			}
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

// buildLeakProgram builds the C sources into bin/ with leakFlags, as
// build-c builds them but without optimising, and gives that binary's
// path. It is the build run under leaks: an optimiser that can see a dead
// allocation would erase the leak before leaks found it.
func buildLeakProgram(dir, name string, sources, with []string, by runner,
	get fetcher) (string, error) {
	argv, program, err := cArgv(name, sources)
	if err != nil {
		return "", err
	}
	libs, err := depsFor(with)
	if err != nil {
		return "", err
	}
	err = os.MkdirAll(filepath.Join(dir, "bin"), folderMode)
	if err != nil {
		return "", err
	}
	add, err := cDeps(dir, libs, get, by)
	if err != nil {
		return "", err
	}
	words := withLinked(argv, add)
	words = slices.Concat(leakFlags, words[len(cFlags):])
	if _, err := by(dir, cCompiler, words...); err != nil {
		return "", err
	}
	return program, nil
}
