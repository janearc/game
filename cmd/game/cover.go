package main

// game cover: how much of a project its tests run, file by file, for Go,
// for C programs built as build-c builds them, and for Xcode schemes,
// Swift and Objective-C alike.
//
// Every file at 100% is what the bar asks; anything less is listed and
// is the exit code. Libraries game fetched, and test files, are not
// counted.

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
)

// covered is one file's lines, or statements for Go: how many could run,
// and how many the tests ran.
type covered struct {
	path       string
	count, ran int
}

// errNotFull is a report with a file below 100%.
var errNotFull = errors.New("cover: not every file is at 100%")

// errNothingCovered is a report with no file in it.
var errNothingCovered = errors.New("cover: no file of the project was measured")

// whole is the whole of a thing as a percentage, defined as a hundred.
const whole = 100

// percent is the file's share run, as a percentage; a file with nothing
// to run counts as all of it.
func (file covered) percent() float64 {
	if count.None(file.count) {
		return whole
	}
	return whole * float64(file.ran) / float64(file.count)
}

// short is whether some of the file was not run.
func (file covered) short() bool {
	return file.ran < file.count
}

// coverVerb is game cover: Go with no word, or c or xcode.
func coverVerb(here repo.Repo, _ config.Config, args []string) error {
	return coverBy(here.Dir(), args, run, httpFetch, os.Stdout, os.Stderr)
}

// coverBy is coverVerb in dir, running tools with by, fetching with get.
func coverBy(dir string, args []string, by runner, get fetcher,
	out, errs io.Writer) error {
	word, rest, said := verbWord(args)
	switch {
	case !said:
		return coverGoBy(dir, by, out)
	case word == "c":
		return coverCBy(dir, rest, by, get, out, errs)
	case word == "xcode":
		return coverXcodeBy(dir, rest, by, out, errs)
	}
	return fmt.Errorf("cover: %q is not c or xcode", word)
}

// leastRunFirst orders files by run share, least first, then by path.
func leastRunFirst(one, other covered) int {
	if by := cmp.Compare(one.percent(), other.percent()); by != same {
		return by
	}
	return strings.Compare(one.path, other.path)
}

// same is what a comparison gives for equals, as cmp has defined it.
const same = 0

// report says each file's share on out, the least run first, then the
// total, and refuses a report with nothing in it or a file below 100%.
func report(files []covered, out io.Writer) error {
	if count.None(len(files)) {
		return errNothingCovered
	}
	slices.SortFunc(files, leastRunFirst)
	var all covered
	var short int
	for _, file := range files {
		fmt.Fprintf(out, "%6.1f%%  %s (%d of %d)\n", file.percent(),
			file.path, file.ran, file.count)
		all.count += file.count
		all.ran += file.ran
		if file.short() {
			short++
		}
	}
	fmt.Fprintf(out, "%6.1f%%  total (%d of %d)\n", all.percent(),
		all.ran, all.count)
	if count.Positive(short) {
		return fmt.Errorf("%w: %d below", errNotFull, short)
	}
	return nil
}

// goProfile is where game cover keeps Go's cover profile.
const goProfile = "bin/.cover/go.out"

// coverGoBy runs the Go tests in dir with a cover profile, by by, and
// reports each file's statements.
func coverGoBy(dir string, by runner, out io.Writer) error {
	err := os.MkdirAll(filepath.Join(dir, "bin", ".cover"), folderMode)
	if err != nil {
		return err
	}
	if _, err := by(dir, "go", "test", "-count=1",
		"-coverprofile="+goProfile, "./..."); err != nil {
		return err
	}
	profile, err := os.ReadFile(filepath.Join(dir, goProfile))
	if err != nil {
		return err
	}
	files, err := goCovered(string(profile))
	if err != nil {
		return err
	}
	return report(files, out)
}

// errProfile is a cover profile line game cannot read.
var errProfile = errors.New("cover: a profile line it cannot read")

// profileFields is what a profile line holds after the file: the span,
// the statement count and the run count, defined by go's profile format.
const profileFields = 3

// goCovered is a Go cover profile's statements, file by file. A block
// listed twice counts once, run if either says so.
func goCovered(profile string) ([]covered, error) {
	type block struct{ count, ran int }
	blocks := map[string]map[string]block{}
	for _, line := range strings.Split(profile, "\n") {
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		var span string
		var counted, runs int
		file, rest, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("%w: %q", errProfile, line)
		}
		read, err := fmt.Sscanf(rest, "%s %d %d", &span, &counted,
			&runs)
		if err != nil || read != profileFields {
			return nil, fmt.Errorf("%w: %q", errProfile, line)
		}
		if blocks[file] == nil {
			blocks[file] = map[string]block{}
		}
		seen := blocks[file][span]
		seen.count = counted
		if count.Positive(runs) {
			seen.ran = counted
		}
		blocks[file][span] = seen
	}
	files := make([]covered, count.Nothing, len(blocks))
	for file, spans := range blocks {
		summed := covered{path: file}
		for _, each := range spans {
			summed.count += each.count
			summed.ran += each.ran
		}
		files = append(files, summed)
	}
	return files, nil
}

// counted says whether a file belongs in the report: not a library game
// fetched, and not a test.
func counted(path string) bool {
	slashed := filepath.ToSlash(path)
	base := filepath.Base(slashed)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return !strings.Contains(slashed, "/bin/.deps/") &&
		!strings.HasPrefix(slashed, "bin/.deps/") &&
		!strings.Contains(slashed, "/Tests/") &&
		!strings.HasPrefix(slashed, "Tests/") &&
		!strings.HasSuffix(stem, "_test") &&
		!strings.HasSuffix(stem, "Tests") &&
		!strings.HasSuffix(stem, "Test")
}
