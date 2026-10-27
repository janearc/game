package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
)

// globber finds the paths a pattern matches.
type globber func(pattern string) ([]string, error)

// errNoFuzzRuntime is a fuzz with no libFuzzer runtime to link.
var errNoFuzzRuntime = errors.New("fuzz: no libFuzzer runtime; " +
	"brew install llvm@20, or name one with -runtime")

// errSwiftFuzz is fuzz xcode: Apple's Swift has no fuzzer on macOS.
var errSwiftFuzz = errors.New("fuzz xcode: Apple's Swift has no " +
	"fuzzer on macOS; Swift fuzzing needs Linux")

// errWhichLanguage is fuzz or race without c or xcode.
var errWhichLanguage = errors.New("name c or xcode")

// fuzzVerb is game fuzz c ...; fuzz xcode is refused with why.
func fuzzVerb(here repo.Repo, _ config.Config, args []string) error {
	return fuzzBy(here.Dir(), args, run, httpFetch, filepath.Glob,
		os.Stdout, os.Stderr)
}

// halfAMinute is how long a fuzz runs by default, in seconds: thirty,
// defined here as enough to find the easy ones.
const halfAMinute = 30

// fuzzBy is fuzzVerb in dir.
func fuzzBy(dir string, args []string, by runner, get fetcher, glob globber,
	out, errs io.Writer) error {
	word, rest, _ := verbWord(args)
	switch word {
	case "c":
		return fuzzCBy(dir, rest, by, get, glob, out, errs)
	case "xcode":
		return errSwiftFuzz
	}
	return fmt.Errorf("fuzz: %w", errWhichLanguage)
}

// fuzzCBy is game fuzz c, as
//
//	game fuzz c [-o NAME] [-with LIB] [-time SECONDS] [-runtime PATH]
//	            SOURCE...
//
// the sources, which define LLVMFuzzerTestOneInput, built by cc with
// fuzzSanitize and libFuzzer into bin/NAME-fuzz, then run for -time
// seconds (30 by default).
//
// Its corpus is kept in bin/.fuzz/NAME-fuzz/corpus, and an input that
// crashes it is written beside that and is the error. .m sources make
// it an Objective-C build.
func fuzzCBy(dir string, args []string, by runner, get fetcher,
	glob globber, out, errs io.Writer) error {
	fs := flag.NewFlagSet("fuzz c", flag.ContinueOnError)
	fs.SetOutput(errs)
	name := fs.String("o", "",
		"the fuzzer's name; the first source's, by default")
	with := libFlag(fs)
	seconds := fs.Int("time", halfAMinute, "how long to fuzz, in seconds")
	runtime := fs.String("runtime", fuzzRuntime(glob),
		"libFuzzer's runtime")
	if err := parsedForeign(fs, args); err != nil {
		return err
	}
	if *runtime == "" {
		return errNoFuzzRuntime
	}
	ask := sanitized{name: *name, suffix: "fuzz", sanitize: fuzzSanitize,
		sources: fs.Args(),
		add:     linked{libs: []string{*runtime, "-lc++"}}}
	program, err := sanitizedBuild(dir, ask, *with, by, get)
	if err != nil {
		return err
	}
	place := filepath.Join("bin", ".fuzz", filepath.Base(program))
	corpus := filepath.Join(place, "corpus")
	err = os.MkdirAll(filepath.Join(dir, corpus), folderMode)
	if err != nil {
		return err
	}
	if _, err := by(dir, program, "-max_total_time="+strconv.Itoa(*seconds),
		"-artifact_prefix="+place+"/", corpus); err != nil {
		return fmt.Errorf("fuzz: %w; what it found is in %s", err,
			place)
	}
	fmt.Fprintf(out, "fuzzed %s for %d s: nothing found\n", program,
		*seconds)
	return nil
}

// libFlag gives fs a -with flag, repeatable, and the names it is given.
func libFlag(fs *flag.FlagSet) *[]string {
	return depFlag(fs, "a C library to fetch, build and link in: theft")
}

// depFlag gives fs a -with flag, repeatable, said as help says, and the
// names it is given.
func depFlag(fs *flag.FlagSet, help string) *[]string {
	with := &libraries{}
	fs.Func("with", help, with.add)
	return &with.names
}

// libraries is the -with flags a verb was given, in order.
type libraries struct {
	names []string
}

// add takes one more library's name, as flag asks.
func (with *libraries) add(lib string) error {
	with.names = append(with.names, lib)
	return nil
}

// sanitizedBuild builds ask in dir with cc, against the SDK xcrun names,
// with the libraries named in with fetched by get and linked in after what
// ask already links; it gives the program's path. Its sources are checked
// before anything is fetched or run.
func sanitizedBuild(dir string, ask sanitized, with []string, by runner,
	get fetcher) (string, error) {
	if _, _, err := sanitizedArgv(ask); err != nil {
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
	sdk, err := by(dir, "xcrun", "--show-sdk-path")
	if err != nil {
		return "", err
	}
	ask.sdk = strings.TrimSpace(sdk)
	ask.add = linked{includes: add.includes, objects: add.objects,
		libs: append(add.libs, ask.add.libs...)}
	argv, program, _ := sanitizedArgv(ask)
	if _, err := by(dir, cCompiler, argv...); err != nil {
		return "", err
	}
	return program, nil
}

// raceVerb is game race c ... or game race xcode ...
func raceVerb(here repo.Repo, _ config.Config, args []string) error {
	return raceBy(here.Dir(), args, run, httpFetch, os.Stdout, os.Stderr)
}

// raceBy is raceVerb in dir.
func raceBy(dir string, args []string, by runner, get fetcher,
	out, errs io.Writer) error {
	word, rest, _ := verbWord(args)
	switch word {
	case "c":
		return raceCBy(dir, rest, by, get, out, errs)
	case "xcode":
		return raceXcodeBy(dir, rest, by, out, errs)
	}
	return fmt.Errorf("race: %w", errWhichLanguage)
}

// raceCBy is game race c, as
//
//	game race c [-o NAME] [-with LIB] SOURCE... [-- ARG...]
//
// the program built by cc with the thread sanitizer into bin/NAME-race,
// then run with the arguments. A race it reports fails it, with the
// report as the error. .m sources make it an Objective-C build.
func raceCBy(dir string, args []string, by runner, get fetcher,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("race c", flag.ContinueOnError)
	fs.SetOutput(errs)
	name := fs.String("o", "",
		"the program's name; the first source's, by default")
	with := libFlag(fs)
	if err := parsedForeign(fs, args); err != nil {
		return err
	}
	sources, programArgs := splitAtDashes(fs.Args())
	ask := sanitized{name: *name, suffix: "race", sanitize: raceSanitize,
		sources: sources}
	program, err := sanitizedBuild(dir, ask, *with, by, get)
	if err != nil {
		return err
	}
	if _, err := by(dir, program, programArgs...); err != nil {
		return fmt.Errorf("race: %w", err)
	}
	fmt.Fprintf(out, "no race in %s\n", program)
	return nil
}

// raceXcodeBy is game race xcode -scheme S: the scheme's tests run by
// xcodebuild with the thread sanitizer on. Swift and Objective-C alike.
// -configuration, -project or -workspace and -destination are as cover
// xcode takes them.
func raceXcodeBy(dir string, args []string, by runner,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("race xcode", flag.ContinueOnError)
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
	if err := parsedForeign(fs, args); err != nil {
		return err
	}
	if word, _, said := verbWord(fs.Args()); said {
		return fmt.Errorf("%w; %q is a word", errXcodeWords, word)
	}
	if err := checkXcodeAsk(ask); err != nil {
		return err
	}
	argv := xcodeNamedArgv(ask)
	argv = append(argv, "-destination", ask.destination,
		"-derivedDataPath", xcodeDerived, "test",
		"-enableThreadSanitizer", "YES")
	if _, err := by(dir, xcodeBuilder, argv...); err != nil {
		return fmt.Errorf("race: %w", err)
	}
	fmt.Fprintf(out, "no race in %s's tests\n", ask.scheme)
	return nil
}
