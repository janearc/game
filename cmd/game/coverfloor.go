package main

// the coverage floor of game check: every Go package is measured the way
// game cover measures it, by goMeasured, and one under the floor fails.
//
// the floor is `cover N` in the builder's config, raised by the project's
// own `cover M`. a file the project allows, with a reason, is left out,
// and so are files marked as generated and packages with no statements.

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
)

// errUnderFloor is a package below the coverage floor.
var errUnderFloor = errors.New("cover: a package is under the floor")

// tenths is how many steps make one whole, for one decimal place.
const tenths = 10

// namedLines is how many uncovered lines a package under the floor names.
const namedLines = 3

// under is a package below the floor: its share run, and the first lines
// of it the tests never ran, as file:line.
type under struct {
	pkg     string
	percent float64
	missed  []string
}

// belowFloor is the packages of files below the floor, in package order.
// the files' paths are relative to the module. a file skip says is
// generated, or allowed has, is left out; a package left with no
// statements is not below anything.
func belowFloor(files []covered, floor int, allowed map[string]string,
	skip func(string) bool) []under {
	sums := map[string]covered{}
	lines := map[string][]string{}
	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(one, other covered) int {
		return strings.Compare(one.path, other.path)
	})
	for _, file := range sorted {
		if _, left := allowed[file.path]; left || skip(file.path) {
			continue
		}
		pkg := path.Dir(file.path)
		sum := sums[pkg]
		sum.count += file.count
		sum.ran += file.ran
		sums[pkg] = sum
		for _, line := range slices.Sorted(slices.Values(file.missed)) {
			lines[pkg] = append(lines[pkg],
				fmt.Sprintf("%s:%d", file.path, line))
		}
	}
	var short []under
	for _, pkg := range slices.Sorted(maps.Keys(sums)) {
		sum := sums[pkg]
		if count.None(sum.count) || sum.percent() >= float64(floor) {
			continue
		}
		missed := lines[pkg]
		short = append(short, under{pkg: pkg, percent: sum.percent(),
			missed: missed[:min(namedLines, len(missed))]})
	}
	return short
}

// generatedHeader is the line the go tool reads as a file's mark that a
// program wrote it, as go has defined it.
var generatedHeader = regexp.MustCompile(
	`^// Code generated .* DO NOT EDIT\.$`)

// generatedFile is whether the file at rel in dir carries the generated
// header before its package clause. a file it cannot read is not.
func generatedFile(dir, rel string) bool {
	file, err := os.Open(filepath.Join(dir, rel))
	if err != nil {
		return false
	}
	defer file.Close()
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		text := lines.Text()
		if generatedHeader.MatchString(text) {
			return true
		}
		if strings.HasPrefix(text, "package ") {
			return false
		}
	}
	return false
}

// sayAllowed says each allowed file and its reason, in file order, with
// a note on one the profile has no file for when measured says so.
func sayAllowed(out io.Writer, allowed map[string]string,
	measured map[string]bool) {
	for _, file := range slices.Sorted(maps.Keys(allowed)) {
		note := ""
		if measured != nil && !measured[file] {
			note = " (no such file in the profile)"
		}
		fmt.Fprintf(out, "check: cover allow %s: %s%s\n", file,
			allowed[file], note)
	}
}

// coverGate holds every Go package of dir to the floor the config says.
// with no floor it measures nothing; the allowed files are still said.
func coverGate(dir string, cfg config.Config, by runner,
	out io.Writer) error {
	floor, allowed := cfg.CoverFloor(), cfg.CoverAllowed()
	if count.None(floor) {
		sayAllowed(out, allowed, nil)
		return nil
	}
	files, err := goMeasured(dir, by)
	if err != nil {
		return err
	}
	prefix := config.Module(dir) + "/"
	measured := map[string]bool{}
	for at := range files {
		files[at].path = strings.TrimPrefix(files[at].path, prefix)
		measured[files[at].path] = true
	}
	sayAllowed(out, allowed, measured)
	generated := func(rel string) bool { return generatedFile(dir, rel) }
	short := belowFloor(files, floor, allowed, generated)
	for _, each := range short {
		// cut, not rounded: 99.97 under a floor of 100 is not 100.0.
		shown := math.Floor(each.percent*tenths) / tenths
		fmt.Fprintf(out, "cover: %s: %.1f%% is under the floor "+
			"of %d%%\n", each.pkg, shown, floor)
		for _, line := range each.missed {
			fmt.Fprintf(out, "  not run: %s\n", line)
		}
	}
	if count.Positive(len(short)) {
		return fmt.Errorf("%w: %d below %d%%", errUnderFloor,
			len(short), floor)
	}
	return nil
}
