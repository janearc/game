package main

import (
	"errors"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"go.uber.org/mock/gomock"
)

func TestMissingReleaseTagUsesPlaceholder(t *testing.T) {
	t.Parallel()
	if got := firstTag([]string{"diff", "stack", "push"}); got != "TAG" {
		t.Fatalf("missing tag became %q", got)
	}
}

func TestReleaseValuesIncludeEachConfiguredValue(t *testing.T) {
	t.Parallel()
	cfg := dotted(t, "set ONE first", "set TWO second")
	got := values(cfg)
	slices.Sort(got)
	if !slices.Equal(got, []string{"first", "second"}) {
		t.Fatalf("release values changed: %v", got)
	}
}

func TestMutantOptionsReadRepeatSelection(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "again")
	write(t, path, "")
	options, err := mutantOptions("", path, count.One, time.Second)
	if err != nil || !strings.Contains(options.String(), ", 0 again") {
		t.Fatalf("repeat file ignored: %s %v", options, err)
	}
}

func TestHardenRejectsTruncatedMachHeader(t *testing.T) {
	t.Parallel()
	found := headerFindings("MH_MAGIC_64 ARM64", "bin/sample")
	if len(found) != count.One || found[count.First].Text() != errNoHeader.Error() {
		t.Fatalf("truncated Mach header accepted: %v", found)
	}
}

func TestHardenMachKeepsToolFailures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("inspection refused")
	gomock.InOrder(
		commands.EXPECT().Run(dir, otool, "-hv", "bin/sample").Return("", failure),
		commands.EXPECT().Run(dir, otool, "-l", "bin/sample").Return("", failure),
		commands.EXPECT().Run(dir, "codesign", "-d", "--entitlements", "-",
			"bin/sample").Return("", failure),
		commands.EXPECT().Run(dir, shell, "-c",
			`exec codesign -d -vv -- "$1" 2>&1`, shell, "bin/sample").Return("", failure),
	)
	found := hardenOne(commands.Run, dir, "bin/sample")
	if len(found) != count.Next(count.One) ||
		!strings.Contains(found[count.First].Text(), "otool -hv: inspection refused") ||
		found[count.Next(count.First)].Text() != "not codesigned" {
		t.Fatalf("inspection failures lost: %v", found)
	}
}

func TestHardenXcodeStopsAtSourceFailure(t *testing.T) {
	t.Parallel()
	commands := NewMockcommandRunner(gomock.NewController(t))
	dir := t.TempDir()
	write(t, filepath.Join(dir, "broken.go"), "package {\n")
	err := hardenXcodeBy(dir, []string{"-scheme", "Sample"}, commands.Run,
		io.Discard, io.Discard)
	if err == nil {
		t.Fatal("xcode audit accepted unparseable source")
	}
}

func TestHardenXcodeStopsAtSettingsFailure(t *testing.T) {
	standTools(t, "semgrep")
	dir := t.TempDir()
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("settings refused")
	gomock.InOrder(
		commands.EXPECT().Run(dir, gomock.Any(), "--json", "--config", "auto", ".").
			Return(`{"results":[]}`, nil),
		commands.EXPECT().Run(dir, xcodeBuilder, gomock.Any()).Return("", failure),
	)
	err := hardenXcodeBy(dir, []string{"-scheme", "Sample"}, commands.Run,
		io.Discard, io.Discard)
	if !errors.Is(err, failure) {
		t.Fatalf("settings failure lost: %v", err)
	}
}

func TestHardenBuildStopsBeforeAuditingFailedBuild(t *testing.T) {
	t.Parallel()
	work := repoAt(t.TempDir())
	write(t, work.Path("cmd", "sample", "main.go"), "package main\n")
	commands := NewMockcommandRunner(gomock.NewController(t))
	failure := errors.New("build refused")
	commands.EXPECT().Run(work.Dir(), "go", gomock.Any()).Return("", failure)
	found, err := hardenBuilt(work, blankConfig(), commands.Run, io.Discard)
	if !errors.Is(err, failure) || len(found) != count.Nothing {
		t.Fatalf("failed build was audited: %v %v", found, err)
	}
}
