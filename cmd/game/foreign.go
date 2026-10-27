package main

// game mutants for languages game does not mutate itself: each argument
// hands the work to that language's own mutation tool and passes on what
// it says.
//
//   mutate-c      C, through Mull: the test program built with Mull's
//                 compiler plugin, then Mull's runner over it
//   mutate-xcode  Swift and Xcode projects, through Muter and the
//                 project's muter.conf.yml
//   mutate-text   any file, through universalmutator: its mutants
//                 written, then a test command run against each
//
// The tools' own flags here are as their documentation gave them; the
// tests hold game's side, against stand-ins for the tools.

import (
	"errors"
	"flag"
	"fmt"
	"github.com/janearc/game/internal/count"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// foreign is a mutator for one language: it runs in dir with by, and
// speaks on out and errs.
type foreign func(dir string, args []string, by runner,
	out, errs io.Writer) error

// foreignMutators are game mutants' arguments for other languages.
var foreignMutators = map[string]foreign{
	"mutate-c":     mutateCBy,
	"mutate-xcode": mutateXcodeBy,
	"mutate-text":  mutateTextBy,
}

// errNoPlugin is mutate-c without Mull's compiler plugin.
var errNoPlugin = errors.New("mutate-c: -plugin names Mull's compiler plugin")

// errNoMuterConfig is mutate-xcode in a tree Muter has no config for.
var errNoMuterConfig = errors.New(
	"mutate-xcode: no muter.conf.yml here; muter init writes one")

// errNoTestCommand is mutate-text without a test command after --.
var errNoTestCommand = errors.New(
	"mutate-text: FILE -- TEST..., the command that passes on the original")

// errForeignFlags is a flag a foreign mutator does not take. The flag
// package has said which on errs.
var errForeignFlags = errors.New("mutants: a flag it does not take")

// mutateCBy is game mutants mutate-c, as
//
//	game mutants mutate-c -plugin PATH [-o NAME] SOURCE...
//
// the C test program built with cFlags and Mull's compiler plugin into
// bin/NAME-mutants, then Mull's runner over it.
//
// The program is the tests: it exits 0 when they pass. -clang names the
// clang the plugin was built for, which may not be cc.
func mutateCBy(dir string, args []string, by runner,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("mutants mutate-c", flag.ContinueOnError)
	fs.SetOutput(errs)
	name := fs.String("o", "",
		"the test program's name; the first source's, by default")
	clang := fs.String("clang", "clang", "the clang Mull's plugin is for")
	plugin := fs.String("plugin", "", "Mull's compiler plugin")
	mull := fs.String("runner", "mull-runner", "Mull's runner")
	if err := parsedForeign(fs, args); err != nil {
		return err
	}
	if *plugin == "" {
		return errNoPlugin
	}
	argv, program, err := cArgv(*name, fs.Args())
	if err != nil {
		return err
	}
	program += "-mutants"
	withoutOutput := argv[:count.Last(len(argv))]
	argv = []string{"-g", "-grecord-command-line",
		"-fpass-plugin=" + *plugin}
	argv = append(argv, withoutOutput...)
	argv = append(argv, program)
	err = os.MkdirAll(filepath.Join(dir, "bin"), folderMode)
	if err != nil {
		return err
	}
	if _, err := by(dir, *clang, argv...); err != nil {
		return err
	}
	said, err := by(dir, *mull, program)
	fmt.Fprint(out, said)
	return err
}

// mutateXcodeBy is game mutants mutate-xcode [-muter PATH] [-- WORD...]:
// Muter run in the tree by its muter.conf.yml, with any words after --
// passed on, such as --files-to-mutate.
func mutateXcodeBy(dir string, args []string, by runner,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("mutants mutate-xcode", flag.ContinueOnError)
	fs.SetOutput(errs)
	muter := fs.String("muter", "muter", "the muter to run")
	if err := parsedForeign(fs, args); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "muter.conf.yml")); err != nil {
		return errNoMuterConfig
	}
	said, err := by(dir, *muter, fs.Args()...)
	fmt.Fprint(out, said)
	return err
}

// parsedForeign parses a foreign mutator's flags, keeping help as help.
func parsedForeign(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return errForeignFlags
	}
	return nil
}

// mutateTextBy is game mutants mutate-text, as
//
//	game mutants mutate-text [-as LANGUAGE] FILE -- TEST...
//
// universalmutator writes the file's mutants to bin/mutants/, then runs
// the test against each one in the file's place. -as names the language
// to read the file as, such as c for an Objective-C file.
func mutateTextBy(dir string, args []string, by runner,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("mutants mutate-text", flag.ContinueOnError)
	fs.SetOutput(errs)
	as := fs.String("as", "", "the language to read the file as")
	mutator := fs.String("mutate", "mutate", "universalmutator's mutate")
	analyzer := fs.String("analyze", "analyze_mutants",
		"universalmutator's analyze_mutants")
	if err := parsedForeign(fs, args); err != nil {
		return err
	}
	file, test, err := textWords(fs.Args())
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
		return fmt.Errorf("mutate-text: %w", err)
	}
	mutants := filepath.Join("bin", "mutants", filepath.Base(file))
	err = os.MkdirAll(filepath.Join(dir, mutants), folderMode)
	if err != nil {
		return err
	}
	made := []string{file}
	if *as != "" {
		made = append(made, *as)
	}
	if _, err := by(dir, *mutator, append(made, "--mutantDir",
		mutants)...); err != nil {
		return err
	}
	said, err := by(dir, *analyzer, file, strings.Join(test, " "),
		"--mutantDir", mutants)
	fmt.Fprint(out, said)
	return err
}

// textWords splits FILE -- TEST... into the file and the test command.
func textWords(words []string) (string, []string, error) {
	file, rest, said := verbWord(words)
	dashes, test, cut := verbWord(rest)
	if !said || !cut || dashes != "--" || count.None(len(test)) {
		return "", nil, errNoTestCommand
	}
	return file, test, nil
}
