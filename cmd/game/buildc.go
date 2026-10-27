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

// cFlags are the flags every C program is built with. They say C11 and
// optimised. They make every warning an error and allow nothing outside
// the standard. They arm the stack and make the code
// position-independent.
//
// They are the flags of the build-c run in mimic's .game.
var cFlags = []string{
	"-std=c11", "-O2", "-fstack-protector-strong", "-fPIE",
	"-Wall", "-Wextra", "-Werror", "-pedantic",
}

// cCompiler is the compiler build-c runs: the system's cc.
const cCompiler = "cc"

// errNoSources is build-c given nothing to build.
var errNoSources = errors.New("build-c: name the C sources to build")

// errNotC is a source that is not a .c file.
var errNotC = errors.New("is not a .c source")

// errBadName is an -o name that would leave bin/.
var errBadName = errors.New("is not a name in bin/")

// errCFlags is a flag build-c does not take. The flag package has said
// which on errs.
var errCFlags = errors.New("build-c: a flag it does not take")

// buildCVerb is game build-c, as
//
//	game build-c [-o NAME] [-with LIB] SOURCE...
//
// the sources compiled with cFlags into bin/NAME, in the tree game was
// started in, sealed against dyld's environment and signed for the
// hardened runtime.
//
// NAME is the first source's name without .c, unless -o names it. Each
// -with is a C library game fetches, builds and links in, such as theft.
func buildCVerb(here repo.Repo, _ config.Config, args []string) error {
	return buildCBy(here.Dir(), args, run, httpFetch, os.Stdout, os.Stderr)
}

// buildCBy is buildCVerb in dir. It fetches with get and compiles with
// by. It says what the compiler said and what it built on out, and
// what was wrong with its flags on errs.
func buildCBy(dir string, args []string, by runner, get fetcher,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("build-c", flag.ContinueOnError)
	fs.SetOutput(errs)
	name := fs.String("o", "",
		"the program's name in bin/; the first source's, by default")
	with := libFlag(fs)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errCFlags
	}
	argv, program, err := cArgv(*name, fs.Args())
	if err != nil {
		return err
	}
	said, err := linkHardened(dir, argv, program, *with, by, get)
	fmt.Fprint(out, said)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "built %s\n", program)
	return nil
}

// sealFlags are the linker's words that seal dyld's environment: a
// __RESTRICT segment with a __restrict section. With it dyld ignores
// DYLD_INSERT_LIBRARIES and the rest.
//
// build-c alone links with them. The instrumented builds need that
// door. Those are cover and leaks and fuzz and race.
var sealFlags = []string{"-Wl,-sectcreate,__RESTRICT,__restrict,/dev/null"}

// signer is the program that signs what build-c made.
const signer = "codesign"

// outputWords is how many words cArgv ends with: -o and the program.
const outputWords = 2 // defined by cArgv, which ends every argv so

// sealed is argv with the seal before its -o.
func sealed(argv []string) []string {
	at := len(argv) - outputWords
	words := append([]string{}, argv[:at]...)
	words = append(words, sealFlags...)
	return append(words, argv[at:]...)
}

// folderMode is the mode bin/ is made with: the owner writes, everyone
// reads and enters.
const folderMode = 0o755 // rwxr-xr-x, defined by sys/stat.h's bits

// linkHardened builds the C sources argv names into program as a
// hardened binary. The libraries in with are fetched and linked in.
// dyld's environment is sealed. The result is signed ad hoc for the
// hardened runtime, so dyld holds it to library validation.
//
// It gives what the compiler said. build-c and aslr c build this way.
func linkHardened(dir string, argv []string, program string,
	with []string, by runner, get fetcher) (string, error) {
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
	words := withLinked(sealed(argv), add)
	said, err := by(dir, cCompiler, words...)
	if err != nil {
		return said, err
	}
	if _, err := by(dir, signer, "-s", "-", "-o", "runtime", "-f",
		program); err != nil {
		return said, err
	}
	return said, nil
}

// cArgv is cc's words for the sources into bin/NAME, and that path
// relative to the tree. It refuses no sources, a source that is not .c
// and a name that would leave bin/.
func cArgv(name string, sources []string) ([]string, string, error) {
	first, _, said := verbWord(sources)
	if !said {
		return nil, "", errNoSources
	}
	for _, source := range sources {
		if filepath.Ext(source) != ".c" {
			return nil, "", fmt.Errorf("build-c: %s %w", source,
				errNotC)
		}
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(first), ".c")
	}
	if name == "." || name == ".." || strings.ContainsRune(name, '/') {
		return nil, "", fmt.Errorf("build-c: %q %w", name, errBadName)
	}
	program := filepath.Join("bin", name)
	argv := append(append([]string{}, cFlags...), sources...)
	return append(argv, "-o", program), program, nil
}
