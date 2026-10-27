package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/janearc/game/internal/count"
)

// hello is a scheme asked for with every default.
var hello = xcodeAsk{scheme: "hello", configuration: "Release",
	destination: "platform=macOS"}

// A scheme alone builds with the defaults, into bin.
func TestXcodeArgvForASchemeAlone(t *testing.T) {
	t.Parallel()
	argv, err := xcodeArgv(hello, "/tree/bin")
	want := []string{"-scheme", "hello", "-configuration", "Release",
		"-derivedDataPath", "bin/.xcode", "-destination", "platform=macOS",
		"build", "CONFIGURATION_BUILD_DIR=/tree/bin"}
	if err != nil || !slices.Equal(argv, want) {
		t.Errorf("argv %q, err %v", argv, err)
	}
}

// A project or a workspace is passed on.
func TestXcodeArgvTakesAProjectOrAWorkspace(t *testing.T) {
	t.Parallel()
	project := hello
	project.project = "app/App.xcodeproj"
	argv, err := xcodeArgv(project, "/tree/bin")
	if err != nil || flagged(argv, "-project") != "app/App.xcodeproj" {
		t.Errorf("a project: argv %q, err %v", argv, err)
	}
	workspace := hello
	workspace.workspace = "App.xcworkspace"
	argv, err = xcodeArgv(workspace, "/tree/bin")
	if err != nil || flagged(argv, "-workspace") != "App.xcworkspace" {
		t.Errorf("a workspace: argv %q, err %v", argv, err)
	}
}

// Any scheme and configuration land where xcodebuild reads them, and the
// products always go to bin.
func TestXcodeArgvKeepsWhatItWasAsked(t *testing.T) {
	t.Parallel()
	rapid.Check(t, keepsWhatItWasAsked)
}

// flagged is the word after the flag in an argv, or none.
func flagged(argv []string, flag string) string {
	at := slices.Index(argv, flag)
	if at < count.First || count.Next(at) >= len(argv) {
		return ""
	}
	return argv[count.Next(at)]
}

// keepsWhatItWasAsked is the property that any scheme and configuration
// land where xcodebuild reads them, and the products always go to bin.
func keepsWhatItWasAsked(t *rapid.T) {
	ask := hello
	ask.scheme = rapid.StringMatching(`[A-Za-z][A-Za-z0-9-]{0,11}`).
		Draw(t, "scheme")
	ask.configuration = rapid.SampledFrom(
		[]string{"Debug", "Release", "Profile"}).Draw(t, "configuration")
	argv, err := xcodeArgv(ask, "/tree/bin")
	last := count.Last(len(argv))
	if err != nil || flagged(argv, "-scheme") != ask.scheme ||
		flagged(argv, "-configuration") != ask.configuration ||
		argv[last] != "CONFIGURATION_BUILD_DIR=/tree/bin" ||
		argv[count.Last(last)] != "build" {
		t.Fatalf("ask %+v: argv %q, err %v", ask, argv, err)
	}
}

// anti-tests: no scheme, a project and a workspace both, and files of the
// wrong kind are each refused.
func TestXcodeArgvRefusesWhatItCannotBuild(t *testing.T) {
	t.Parallel()
	none := hello
	none.scheme = ""
	both := hello
	both.project, both.workspace = "A.xcodeproj", "A.xcworkspace"
	notProject := hello
	notProject.project = "A.xcworkspace"
	notWorkspace := hello
	notWorkspace.workspace = "A.xcodeproj"
	for ask, want := range map[xcodeAsk]error{
		none: errNoScheme, both: errProjectAndWorkspace,
		notProject: errNotXcode, notWorkspace: errNotXcode,
	} {
		if _, err := xcodeArgv(ask, "/tree/bin"); !errors.Is(err, want) {
			t.Errorf("%+v: %v, want %v", ask, err, want)
		}
	}
}

// build-xcode runs xcodebuild in the tree with xcodeArgv's words, makes
// bin/ first, and says what it built.
func TestBuildXcodeAsksXcodebuildAndSaysWhatItBuilt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	builder := &cc{said: "** BUILD SUCCEEDED **\n"}
	var out, errs bytes.Buffer
	err := buildXcodeBy(dir, []string{"-scheme", "hello"}, builder.run, noFetch,
		&out, &errs)
	want, _ := xcodeHardenedArgv(hello, filepath.Join(dir, "bin"))
	if err != nil || builder.dir != dir || builder.name != "xcodebuild" ||
		!slices.Equal(builder.args, want) {
		t.Errorf("err %v; asked in %q: %s %q", err, builder.dir,
			builder.name, builder.args)
	}
	if out.String() != "built hello (Release) into bin/\n" {
		t.Errorf("said %q", out.String())
	}
	if info, err := os.Stat(filepath.Join(dir, "bin")); err != nil ||
		!info.IsDir() {
		t.Errorf("bin/: %v", err)
	}
}

// build-xcode says every hardened setting on the command line, between
// the build action and the products' directory, so the project's own
// settings are overridden and harden's settings pass finds nothing
// build-xcode would not have set.
func TestXcodeHardenedArgvSaysEachSetting(t *testing.T) {
	t.Parallel()
	argv, err := xcodeHardenedArgv(hello, "/tree/bin")
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := xcodeArgv(hello, "/tree/bin")
	if len(argv) != len(plain)+len(xcodeSettings) ||
		argv[count.Last(len(argv))] != "CONFIGURATION_BUILD_DIR=/tree/bin" {
		t.Errorf("argv %q", argv)
	}
	said := strings.Join(argv, " ")
	for _, want := range []string{"build ENABLE_HARDENED_RUNTIME=YES",
		"ENABLE_STRICT_OBJC_MSGSEND=YES", "DEAD_CODE_STRIPPING=YES",
		"GCC_TREAT_WARNINGS_AS_ERRORS=YES",
		"CODE_SIGN_INJECT_BASE_ENTITLEMENTS=NO"} {
		if !strings.Contains(said, want) {
			t.Errorf("no %q in %q", want, said)
		}
	}
	settings := strings.Join(xcodeHardening(), "\n")
	if got := xcodeSettingsFindings(settings); count.Positive(len(got)) {
		t.Errorf("harden finds fault with its own settings: %v", got)
	}
	if _, err := xcodeHardenedArgv(xcodeAsk{}, "/tree/bin"); !errors.Is(err,
		errNoScheme) {
		t.Errorf("no scheme: %v", err)
	}
}

// anti-test: when xcodebuild fails, its error comes back and nothing is
// said to be built.
func TestBuildXcodeSaysNothingBuiltWhenItFails(t *testing.T) {
	t.Parallel()
	failed := errors.New("xcodebuild: ** BUILD FAILED **")
	builder := &cc{err: failed}
	var out, errs bytes.Buffer
	err := buildXcodeBy(t.TempDir(), []string{"-scheme", "hello"},
		builder.run, noFetch, &out, &errs)
	if !errors.Is(err, failed) || strings.Contains(out.String(), "built") {
		t.Errorf("err %v, said %q", err, out.String())
	}
}

// anti-tests: --help is help; a flag it does not take, a bare word, no
// scheme, and a tree with no room for bin/ are refused; xcodebuild is
// never asked.
func TestBuildXcodeRefusesBeforeAskingXcodebuild(t *testing.T) {
	t.Parallel()
	builder := &cc{}
	var out, errs bytes.Buffer
	cases := map[string][]string{
		"help": {"--help"}, "flag": {"-scheme", "a", "-loud"},
		"word": {"-scheme", "a", "again"}, "scheme": nil,
	}
	wants := map[string]error{"help": flag.ErrHelp,
		"flag": errXcodeFlags, "word": errXcodeWords, "scheme": errNoScheme}
	for name, args := range cases {
		err := buildXcodeBy(t.TempDir(), args, builder.run, noFetch, &out, &errs)
		if !errors.Is(err, wants[name]) {
			t.Errorf("%s: %v", name, err)
		}
	}
	full := t.TempDir()
	shutFile := os.FileMode(0o600)
	if err := os.WriteFile(filepath.Join(full, "bin"), nil,
		shutFile); err != nil {
		t.Fatal(err)
	}
	if err := buildXcodeBy(full, []string{"-scheme", "a"}, builder.run, noFetch,
		&out, &errs); err == nil {
		t.Error("a file where bin/ goes was taken")
	}
	if count.Positive(builder.asked) {
		t.Errorf("xcodebuild asked %d times", builder.asked)
	}
}

// End to end with the system's xcodebuild: a one-file Swift package is
// built into bin/ and runs. It takes seconds, so -short leaves it out.
func TestBuildXcodeBuildsARealPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: no real xcodebuild")
	}
	if _, err := exec.LookPath("xcodebuild"); err != nil {
		t.Skip("no xcodebuild on this machine")
	}
	dir := t.TempDir()
	manifest := "// swift-tools-version:5.9\nimport PackageDescription\n" +
		"let package = Package(name: \"hello\", targets: " +
		"[.executableTarget(name: \"hello\", path: \"Sources\")])\n"
	openDir := os.FileMode(0o755)
	shutFile := os.FileMode(0o600)
	if err := os.MkdirAll(filepath.Join(dir, "Sources"), openDir); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"Package.swift": manifest, "Sources/main.swift": "print(\"hello\")\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body),
			shutFile); err != nil {
			t.Fatal(err)
		}
	}
	if err := buildXcodeVerb(repoAt(dir), blankConfig(),
		[]string{"-scheme", "hello"}); err != nil {
		t.Fatal(err)
	}
	said, err := exec.Command(filepath.Join(dir, "bin", "hello")).Output()
	if err != nil || string(said) != "hello\n" {
		t.Errorf("bin/hello said %q, %v", said, err)
	}
}
