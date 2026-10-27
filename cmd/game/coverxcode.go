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
)

// xcodeResult is where game cover keeps xcodebuild's result bundle. It is
// cleared before each run, since xcodebuild will not write over one.
const xcodeResult = "bin/.xcode/cover.xcresult"

// coverXcodeBy is game cover xcode -scheme S: the scheme's tests run by
// xcodebuild with coverage on, then each file's lines reported from
// xccov. Swift and Objective-C alike. -configuration, -project or
// -workspace and -destination are as build-xcode takes them.
func coverXcodeBy(dir string, args []string, by runner,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("cover xcode", flag.ContinueOnError)
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
	if err := os.RemoveAll(filepath.Join(dir, "bin", ".xcode",
		"cover.xcresult")); err != nil {
		return err
	}
	err := os.MkdirAll(filepath.Join(dir, "bin", ".xcode"), folderMode)
	if err != nil {
		return err
	}
	if _, err := by(dir, xcodeBuilder, coverXcodeArgv(ask)...); err != nil {
		return err
	}
	said, err := by(dir, "xcrun", "xccov", "view", "--report", "--json",
		xcodeResult)
	if err != nil {
		return err
	}
	files, err := xcodeCovered(said, dir)
	if err != nil {
		return err
	}
	return report(files, out)
}

// coverXcodeArgv is xcodebuild's words for testing ask with coverage on.
func coverXcodeArgv(ask xcodeAsk) []string {
	argv := xcodeNamedArgv(ask)
	return append(argv,
		"-destination", ask.destination,
		"-derivedDataPath", xcodeDerived,
		"test", "-enableCodeCoverage", "YES", "-resultBundlePath",
		xcodeResult)
}

// errXccovJSON is an xccov report game cannot read.
var errXccovJSON = errors.New("cover: an xccov report it cannot read")

// xcodeCovered is xccov's report, file by file, with paths made relative
// to dir where they are under it, and only the files counted. A file in
// more than one target counts once, with the most of it run.
func xcodeCovered(report, dir string) ([]covered, error) {
	var read struct {
		Targets []struct {
			Files []struct {
				Path                          string
				CoveredLines, ExecutableLines int
			}
		}
	}
	if err := json.Unmarshal([]byte(report), &read); err != nil {
		return nil, fmt.Errorf("%w: %v", errXccovJSON, err)
	}
	best := map[string]covered{}
	for _, target := range read.Targets {
		for _, file := range target.Files {
			path := underDir(file.Path, dir)
			if !counted(path) {
				continue
			}
			seen, ok := best[path]
			if !ok || file.CoveredLines > seen.ran {
				best[path] = covered{path: path,
					count: file.ExecutableLines,
					ran:   file.CoveredLines}
			}
		}
	}
	files := make([]covered, count.Nothing, len(best))
	for _, file := range best {
		files = append(files, file)
	}
	return files, nil
}
