package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/janearc/game/internal/count"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// coverFlags are what a C program is built with for game cover: no
// optimising, so each line is where it was written, and clang's profile
// and coverage map.
var coverFlags = []string{"-O0", "-fprofile-instr-generate",
	"-fcoverage-mapping"}

// coverCBy is game cover c, as
//
//	game cover c [-o NAME] [-with LIB] SOURCE... [-- ARG...]
//
// the C test program built as build-c builds it, with coverFlags, into
// bin/NAME-cover; run with the arguments; then its profile merged by
// llvm-profdata and each source's lines reported from llvm-cov.
//
// The program is the tests: it exits 0 when they pass. Libraries from
// -with are linked in and not counted.
func coverCBy(dir string, args []string, by runner, get fetcher,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("cover c", flag.ContinueOnError)
	fs.SetOutput(errs)
	name := fs.String("o", "",
		"the test program's name; the first source's, by default")
	with := libFlag(fs)
	if err := parsedForeign(fs, args); err != nil {
		return err
	}
	sources, testArgs := splitAtDashes(fs.Args())
	argv, program, err := cArgv(*name, sources)
	if err != nil {
		return err
	}
	program += "-cover"
	if err := buildCovered(dir, argv, program, *with, by, get); err != nil {
		return err
	}
	said, err := profiled(dir, program, testArgs, by)
	if err != nil {
		return err
	}
	files, err := cCovered(said, dir)
	if err != nil {
		return err
	}
	return report(files, out)
}

// buildCovered builds the test program with coverFlags, the libraries
// with fetched and linked in, into program.
func buildCovered(dir string, argv []string, program string,
	with []string, by runner, get fetcher) error {
	libs, err := depsFor(with)
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Join(dir, "bin", ".cover"), folderMode)
	if err != nil {
		return err
	}
	add, err := cDeps(dir, libs, get, by)
	if err != nil {
		return err
	}
	words := withLinked(argv, add)
	flags, rest := words[:len(cFlags)], words[len(cFlags):]
	middle := rest[:count.Last(len(rest))]
	words = slices.Concat(flags, coverFlags, middle, []string{program})
	_, err = by(dir, cCompiler, words...)
	return err
}

// profiled runs the test program with its profile written, merges the
// profile and exports llvm-cov's summary of it.
func profiled(dir, program string, testArgs []string,
	by runner) (string, error) {
	profile := filepath.Join("bin", ".cover", filepath.Base(program))
	raw, data := profile+".profraw", profile+".profdata"
	ran := slices.Concat([]string{"LLVM_PROFILE_FILE=" + raw, program},
		testArgs)
	if _, err := by(dir, "env", ran...); err != nil {
		return "", err
	}
	if _, err := by(dir, "xcrun", "llvm-profdata", "merge", "-sparse", raw,
		"-o", data); err != nil {
		return "", err
	}
	return by(dir, "xcrun", "llvm-cov", "export", "-summary-only",
		program, "-instr-profile="+data)
}

// splitAtDashes is the words before --, and those after it.
func splitAtDashes(words []string) ([]string, []string) {
	at := slices.Index(words, "--")
	if at == notFound {
		return words, nil
	}
	return words[:at], words[count.Next(at):]
}

// notFound is what slices gives for a place it did not find, as it has
// defined it.
const notFound = -1

// exported is where llvm-cov's export keeps the files: the first data
// set, as llvm has defined the shape.
const exported = 0

// errCoverJSON is a coverage export game cannot read.
var errCoverJSON = errors.New("cover: an export it cannot read")

// cCovered is llvm-cov's export, file by file, with paths made relative
// to dir where they are under it, and only the files counted.
func cCovered(export, dir string) ([]covered, error) {
	var read struct {
		Data []struct {
			Files []struct {
				Filename string
				Summary  struct {
					Lines struct{ Count, Covered int }
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(export), &read); err != nil ||
		count.None(len(read.Data)) {
		return nil, fmt.Errorf("%w: %v", errCoverJSON, err)
	}
	var files []covered
	for _, file := range read.Data[exported].Files {
		path := underDir(file.Filename, dir)
		if counted(path) {
			files = append(files, covered{path: path,
				count: file.Summary.Lines.Count,
				ran:   file.Summary.Lines.Covered})
		}
	}
	return files, nil
}

// underDir is path relative to dir when it is inside it, or as it is.
func underDir(path, dir string) string {
	relative, err := filepath.Rel(dir, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return path
	}
	return relative
}
