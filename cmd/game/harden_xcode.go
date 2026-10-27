package main

// game harden xcode runs the source, binary and module passes. Then it
// holds the scheme's build settings to the hardened default.
//
// The release build is a finding when it turns off the hardened
// runtime. The same for strict Objective-C dispatch, dead code
// stripping and warnings as errors. Injected base entitlements too.

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/janearc/game/internal/lint"
)

// hardenXcodeBy is game harden xcode -scheme S, plus the settings audit.
func hardenXcodeBy(dir string, args []string, by runner,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("harden xcode", flag.ContinueOnError)
	fs.SetOutput(errs)
	var ask xcodeAsk
	fs.StringVar(&ask.scheme, "scheme", "", "the scheme to audit")
	fs.StringVar(&ask.configuration, "configuration", "Release",
		"the build configuration to audit")
	fs.StringVar(&ask.project, "project", "",
		"an .xcodeproj, if not the one here")
	fs.StringVar(&ask.workspace, "workspace", "",
		"an .xcworkspace to audit in")
	fs.StringVar(&ask.destination, "destination", "platform=macOS",
		"where it is built for")
	if err := parsedForeign(fs, args); err != nil {
		return err
	}
	if word, _, said := verbWord(fs.Args()); said {
		return fmt.Errorf("%w; %q is a word", errXcodeWords, word)
	}
	if err := checkXcodeAsk(ask); err != nil {
		return err
	}
	found, err := hardenFinds(dir, by)
	if err != nil {
		return err
	}
	said, err := by(dir, xcodeBuilder, xcodeSettingsArgv(ask)...)
	if err != nil {
		return err
	}
	found = append(found, xcodeSettingsFindings(said)...)
	// the analyzer's diagnostics are read whatever its exit. With
	// warnings as errors, the build that fails is the one with findings.
	analyzed, err := by(dir, xcodeBuilder, xcodeAnalyzeArgv(ask)...)
	diags := diagFindings(analyzed, dir)
	if err != nil && none(diags) {
		diags = failed("xcodebuild analyze", err)
	}
	found = append(found, diags...)
	return sayHarden(out, found)
}

// xcodeAnalyzeArgv is xcodebuild's words for running the static analyzer
// over the scheme, which reads the project's own compiler flags.
func xcodeAnalyzeArgv(ask xcodeAsk) []string {
	argv := xcodeNamedArgv(ask)
	return append(argv, "-destination", ask.destination, "analyze")
}

// xcodeSettingsArgv is xcodebuild's words for reading ask's settings.
func xcodeSettingsArgv(ask xcodeAsk) []string {
	argv := xcodeNamedArgv(ask)
	return append(argv, "-showBuildSettings")
}

// xcodeNamedArgv is xcodebuild's words naming the scheme and the
// project or workspace. The configuration is named when one is asked.
func xcodeNamedArgv(ask xcodeAsk) []string {
	argv := []string{"-scheme", ask.scheme}
	if ask.configuration != "" {
		argv = append(argv, "-configuration", ask.configuration)
	}
	return append(argv, xcodeFiles(ask)...)
}

// xcodeSetting is one hardening the scheme is held to. One that must be
// said has a weak default, so its absence from the build settings is
// the weak side too.
type xcodeSetting struct {
	key  string
	want string
	why  string
	said bool
}

// xcodeSettings are the settings a hardened release keeps.
var xcodeSettings = []xcodeSetting{
	{"ENABLE_HARDENED_RUNTIME", "YES", "the hardened runtime is off", true},
	{"ENABLE_STRICT_OBJC_MSGSEND", "YES",
		"strict Objective-C dispatch is off", false},
	{"DEAD_CODE_STRIPPING", "YES", "dead code stripping is off", false},
	{"GCC_TREAT_WARNINGS_AS_ERRORS", "YES", "warnings are not errors",
		true},
	{"CODE_SIGN_INJECT_BASE_ENTITLEMENTS", "NO",
		"base entitlements are injected", false},
}

// xcodeSettingsFindings reads xcodebuild -showBuildSettings and finds a
// setting that is not what a hardened release keeps. That is one
// present and wrong, or one absent whose default is the wrong side.
func xcodeSettingsFindings(said string) []lint.Finding {
	held := map[string]string{}
	for _, line := range strings.Split(said, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		held[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	var out []lint.Finding
	for _, setting := range xcodeSettings {
		value, seen := held[setting.key]
		switch {
		case seen && value != setting.want:
			out = append(out, hardFinding("xcodebuild",
				setting.key+" is "+value+": "+setting.why))
		case !seen && setting.said:
			out = append(out, hardFinding("xcodebuild", setting.key+
				" is not set, and its default is not "+
				setting.want+": "+setting.why))
		}
	}
	return out
}
