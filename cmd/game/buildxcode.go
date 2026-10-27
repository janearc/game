package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
)

// xcodeBuilder is the program build-xcode runs.
const xcodeBuilder = "xcodebuild"

// xcodeDerived is where xcodebuild keeps its intermediate files: inside
// bin/, so game clean takes them with the rest.
const xcodeDerived = "bin/.xcode"

// errNoScheme is build-xcode without -scheme.
var errNoScheme = errors.New("build-xcode: -scheme names what to build")

// errProjectAndWorkspace is build-xcode given both.
var errProjectAndWorkspace = errors.New(
	"build-xcode: -project or -workspace, not both")

// errNotXcode is a -project or -workspace of the wrong kind.
var errNotXcode = errors.New("is not that kind of Xcode file")

// errXcodeWords is a word build-xcode does not take: everything it
// takes is a flag.
var errXcodeWords = errors.New("build-xcode: takes flags only")

// errXcodeFlags is a flag build-xcode does not take. The flag package has
// said which on errs.
var errXcodeFlags = errors.New("build-xcode: a flag it does not take")

// xcodeAsk is what build-xcode was asked to build.
type xcodeAsk struct {
	scheme, configuration, project, workspace, destination string
}

// buildXcodeVerb is game build-xcode -scheme S [-with LIB]. The scheme
// is built by xcodebuild and its products put in bin/ of the tree game
// was started in.
//
// Each -with names a library game fetches into bin/.deps first, such
// as swiftqc or fox. The project points at it by path.
func buildXcodeVerb(here repo.Repo, _ config.Config, args []string) error {
	return buildXcodeBy(here.Dir(), args, run, httpFetch, os.Stdout,
		os.Stderr)
}

// buildXcodeBy is buildXcodeVerb in dir. It fetches with get and builds
// with by. It says what it fetched and built on out, and what was
// wrong with its flags on errs.
func buildXcodeBy(dir string, args []string, by runner, get fetcher,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("build-xcode", flag.ContinueOnError)
	fs.SetOutput(errs)
	with := depFlag(fs,
		"a library to fetch into bin/.deps first: swiftqc, fox")
	ask := askedXcode(fs, "build")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errXcodeFlags
	}
	if word, _, said := verbWord(fs.Args()); said {
		return fmt.Errorf("%w; %q is a word", errXcodeWords, word)
	}
	bin := filepath.Join(dir, "bin")
	argv, err := xcodeHardenedArgv(*ask, bin)
	if err != nil {
		return err
	}
	libs, err := depsFor(*with)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(bin, folderMode); err != nil {
		return err
	}
	if err := fetchAll(dir, libs, get, out); err != nil {
		return err
	}
	if _, err := by(dir, xcodeBuilder, argv...); err != nil {
		return err
	}
	fmt.Fprintf(out, "built %s (%s) into bin/\n", ask.scheme,
		ask.configuration)
	return nil
}

// askedXcode is the flags an xcode verb takes, bound to what it is
// asked. They name the scheme and its configuration (Release). They
// name a project or a workspace. They name the destination
// (platform=macOS).
//
// doing says what the verb does with the scheme, for the flags' help.
func askedXcode(fs *flag.FlagSet, doing string) *xcodeAsk {
	ask := &xcodeAsk{}
	fs.StringVar(&ask.scheme, "scheme", "", "the scheme to "+doing)
	fs.StringVar(&ask.configuration, "configuration", "Release",
		"the build configuration")
	fs.StringVar(&ask.project, "project", "",
		"an .xcodeproj, if not the one here")
	fs.StringVar(&ask.workspace, "workspace", "",
		"an .xcworkspace to "+doing+" in")
	fs.StringVar(&ask.destination, "destination", "platform=macOS",
		"where it is built for")
	return ask
}

// xcodeArgv is xcodebuild's words for ask, its products put in bin. It
// refuses no scheme. It refuses a project and a workspace both. It
// refuses either of the wrong kind.
func xcodeArgv(ask xcodeAsk, bin string) ([]string, error) {
	if err := checkXcodeAsk(ask); err != nil {
		return nil, err
	}
	return xcodeBuildArgv(ask, bin), nil
}

// xcodeBuildArgv is the build command for an already checked ask.
func xcodeBuildArgv(ask xcodeAsk, bin string) []string {
	argv := xcodeNamedArgv(ask)
	return append(argv, "-derivedDataPath", xcodeDerived,
		"-destination", ask.destination, "build",
		"CONFIGURATION_BUILD_DIR="+bin)
}

// xcodeHardenedArgv is xcodeArgv with the hardened settings said on the
// command line, where they override the project's. They are the ones
// in xcodeSettings: the hardened runtime and strict Objective-C
// dispatch and dead code stripping and warnings as errors.
//
// build-xcode builds this way. leaks xcode does not. The leaks tool
// reaches its program through dyld's environment, which the hardened
// runtime seals.
//
// Signing is the project's. The hardened runtime is a property of a
// signature. So a scheme that signs with nothing gets none, and game
// harden then says so.
func xcodeHardenedArgv(ask xcodeAsk, bin string) ([]string, error) {
	argv, err := xcodeArgv(ask, bin)
	if err != nil {
		return nil, err
	}
	at := len(argv) - productsWords
	words := append([]string{}, argv[:at]...)
	words = append(words, xcodeHardening()...)
	return append(words, argv[at:]...), nil
}

// productsWords is how many words xcodeArgv ends with: the products'
// directory, as one word.
const productsWords = 1 // defined by xcodeArgv, which ends every argv so

// xcodeHardening is each hardened setting as xcodebuild takes it on
// the command line: the key, an equals sign and what it must be. They
// come in the order harden checks them.
func xcodeHardening() []string {
	var out []string
	for _, setting := range xcodeSettings {
		out = append(out, setting.key+"="+setting.want)
	}
	return out
}

// checkXcodeAsk refuses no scheme. It refuses a project and a
// workspace both. It refuses either of the wrong kind.
func checkXcodeAsk(ask xcodeAsk) error {
	switch {
	case ask.scheme == "":
		return errNoScheme
	case ask.project != "" && ask.workspace != "":
		return errProjectAndWorkspace
	case ask.project != "" &&
		!strings.HasSuffix(ask.project, ".xcodeproj"):
		return fmt.Errorf("build-xcode: %s %w", ask.project,
			errNotXcode)
	case ask.workspace != "" &&
		!strings.HasSuffix(ask.workspace, ".xcworkspace"):
		return fmt.Errorf("build-xcode: %s %w", ask.workspace,
			errNotXcode)
	}
	return nil
}

// xcodeFiles is the -project or -workspace words ask names, if any.
func xcodeFiles(ask xcodeAsk) []string {
	switch {
	case ask.project != "":
		return []string{"-project", ask.project}
	case ask.workspace != "":
		return []string{"-workspace", ask.workspace}
	}
	return nil
}
