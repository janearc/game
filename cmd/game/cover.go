package main

// game cover: how much of a project its Go tests run, file by file.
//
// Every file at 100% is what the bar asks; anything less is listed and
// is the exit code.

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/puzzlemouth/pzm"
)

// covered is one file's lines, or statements for Go: how many could run,
// and how many the tests ran.
type covered struct {
	path       string
	count, ran int
	// missed is the first line of each Go block the tests never ran, in
	// order. only Go fills it.
	missed []int
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

// coverVerb is game cover: Go, with no word.
func coverVerb(here repo.Repo, _ config.Config, call pzm.Call) error {
	return cover(here.Dir(), call.Args(), call.On("html"), run, call.Out())
}

// cover is coverVerb in dir, running tools with by.
func cover(dir string, args []string, html bool, by runner,
	out io.Writer) error {
	if word, _, said := verbWord(args); said {
		return fmt.Errorf("cover: %q is a word; it takes none", word)
	}
	failed := coverGo(dir, by, out)
	short := errors.Is(failed, errNotFull)
	if !html || failed != nil && !short {
		return failed
	}
	// the report is wanted most when a file is short.
	if err := coverGoHTML(dir, by, out); err != nil {
		return err
	}
	return failed
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

// goHTML is where game cover --html keeps Go's html report.
const goHTML = "bin/.cover/go.html"

// goMeasured runs the Go tests in dir with a cover profile, by by, and
// reads each file's statements from it. game cover reports them and game
// check holds them to the floor.
func goMeasured(dir string, by runner) ([]covered, error) {
	err := os.MkdirAll(filepath.Join(dir, "bin", ".cover"), folderMode)
	if err != nil {
		return nil, err
	}
	if _, err := by(dir, "go", "test", "-count=1",
		"-coverprofile="+goProfile, "./..."); err != nil {
		return nil, err
	}
	profile, err := os.ReadFile(filepath.Join(dir, goProfile))
	if err != nil {
		return nil, err
	}
	return goCovered(string(profile))
}

// coverGo measures the Go tests in dir, by by, and reports each file's
// statements.
func coverGo(dir string, by runner, out io.Writer) error {
	files, err := goMeasured(dir, by)
	if err != nil {
		return err
	}
	return report(files, out)
}

// coverGoHTML turns the profile coverGo kept into go's html report and
// says where it is.
func coverGoHTML(dir string, by runner, out io.Writer) error {
	if _, err := by(dir, "go", "tool", "cover", "-html="+goProfile,
		"-o", goHTML); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "cover: html report at %s\n",
		filepath.Join(dir, goHTML))
	return err
}

// startLine is the line a profile's span begins on, from "12.3,14.2", and
// nothing for a span it cannot read.
func startLine(span string) int {
	line, _, _ := strings.Cut(span, ".")
	number, _ := strconv.Atoi(line)
	return number
}

// errProfile is a cover profile line game cannot read.
var errProfile = errors.New("cover: a profile line it cannot read")

// profileFields is what a profile line holds after the file: the span,
// the statement count and the run count, defined by go's profile format.
const profileFields = 3

// goCovered is a Go cover profile's statements, file by file. A block
// listed twice counts once, run if either says so.
func goCovered(profile string) ([]covered, error) {
	type block struct{ count, ran, line int }
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
		seen.line = startLine(span)
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
			if count.Positive(each.count) && count.None(each.ran) {
				summed.missed = append(summed.missed, each.line)
			}
		}
		slices.Sort(summed.missed)
		summed.missed = slices.Compact(summed.missed)
		files = append(files, summed)
	}
	return files, nil
}
