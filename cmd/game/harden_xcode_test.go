package main

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

func TestXcodeSettingsFindings(t *testing.T) {
	t.Parallel()
	good := "ENABLE_HARDENED_RUNTIME = YES\n" +
		"ENABLE_STRICT_OBJC_MSGSEND = YES\n" +
		"DEAD_CODE_STRIPPING = YES\n" +
		"GCC_TREAT_WARNINGS_AS_ERRORS = YES\n" +
		"CODE_SIGN_INJECT_BASE_ENTITLEMENTS = NO\n"
	if got := xcodeSettingsFindings(good); count.Positive(len(got)) {
		t.Errorf("good: %v", got)
	}
	weak := []string{"ENABLE_HARDENED_RUNTIME = NO",
		"GCC_TREAT_WARNINGS_AS_ERRORS = NO"}
	got := xcodeSettingsFindings(strings.Join(weak, "\n") + "\n")
	if len(got) != len(weak) {
		t.Fatalf("got %d findings: %v", len(got), got)
	}
	for _, finding := range got {
		if finding.File() != "xcodebuild" || finding.Rule() != "harden" {
			t.Errorf("finding: %+v", finding)
		}
	}
}

// a setting whose default is the weak side is a finding when it is not
// said at all; one whose default is the strong side is not.
func TestXcodeSettingsFindingsHoldTheDefaults(t *testing.T) {
	t.Parallel()
	got := xcodeSettingsFindings("DEAD_CODE_STRIPPING = YES\n")
	wants := []string{"ENABLE_HARDENED_RUNTIME is not set",
		"GCC_TREAT_WARNINGS_AS_ERRORS is not set"}
	if len(got) != len(wants) {
		t.Fatalf("got %d findings: %v", len(got), got)
	}
	for _, want := range wants {
		if !hasFinding(got, "xcodebuild", want) {
			t.Errorf("no %q in %v", want, got)
		}
	}
}

func TestXcodeSettingsArgvNamesTheScheme(t *testing.T) {
	t.Parallel()
	got := xcodeSettingsArgv(xcodeAsk{scheme: "App", configuration: "Release",
		project: "App.xcodeproj"})
	want := []string{"-scheme", "App", "-configuration", "Release",
		"-project", "App.xcodeproj", "-showBuildSettings"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v", got)
	}
}

// xcodeSays is xcodebuild as a stand-in: its settings, and what analyze
// says and whether it fails.
type xcodeSays struct {
	settings string
	analyze  string
	fails    error
}

// run is the runner.
func (says xcodeSays) run(_, name string, args ...string) (string, error) {
	switch {
	case name == "xcodebuild" && slices.Contains(args, "analyze"):
		return says.analyze, says.fails
	case name == "xcodebuild":
		return says.settings, nil
	}
	return "", nil
}

func TestHardenXcodeAddsTheSettingsPass(t *testing.T) {
	standTools(t) // the analyzers stand out, so only the settings remain
	var out bytes.Buffer
	weak := xcodeSays{settings: "ENABLE_HARDENED_RUNTIME = NO\n"}
	err := hardenXcodeBy(t.TempDir(), []string{"-scheme", "App"}, weak.run,
		&out, &bytes.Buffer{})
	if err == nil {
		t.Error("a hardening off was passed")
	}
	if !strings.Contains(out.String(), "ENABLE_HARDENED_RUNTIME is NO") {
		t.Errorf("said %q", out.String())
	}
}

// the analyzer's diagnostics are read when it fails, which with
// warnings as errors is exactly when it found something; and an
// analyzer that fell over saying nothing is a finding of its own.
func TestHardenXcodeReadsAFailedAnalyze(t *testing.T) {
	standTools(t)
	dir := t.TempDir()
	good := "ENABLE_HARDENED_RUNTIME = YES\nGCC_TREAT_WARNINGS_AS_ERRORS = YES\n"
	found := xcodeSays{settings: good,
		analyze: dir + "/a.m:12:5: warning: use after free\n",
		fails:   errors.New("xcodebuild: ANALYZE FAILED")}
	var out bytes.Buffer
	err := hardenXcodeBy(dir, []string{"-scheme", "App"}, found.run, &out,
		&bytes.Buffer{})
	if err == nil || !strings.Contains(out.String(),
		"a.m:12: harden: warning: use after free") {
		t.Errorf("err %v, said %q", err, out.String())
	}
	mute := xcodeSays{settings: good,
		fails: errors.New("xcodebuild: no such scheme")}
	out.Reset()
	err = hardenXcodeBy(dir, []string{"-scheme", "App"}, mute.run, &out,
		&bytes.Buffer{})
	if err == nil || !strings.Contains(out.String(),
		"xcodebuild analyze failed") {
		t.Errorf("err %v, said %q", err, out.String())
	}
}
