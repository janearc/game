package main

import (
	"errors"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
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
	got := slices.Collect(maps.Values(cfg.Set()))
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

func TestHardenStopsAtSourceFailure(t *testing.T) {
	t.Parallel()
	commands := notRun(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "broken.go"), "package {\n")
	err := argsHardenBy(t, dir, nil, commands, io.Discard)
	if err == nil {
		t.Fatal("audit accepted unparseable source")
	}
}

func TestHardenBuildStopsBeforeAuditingFailedBuild(t *testing.T) {
	t.Parallel()
	work := repoAt(t.TempDir())
	write(t, work.Path("cmd", "sample", "main.go"), "package main\n")
	failure := errors.New("build refused")
	commands := runs(t, ranStep{dir: work.Dir(), name: "go", anyArgs: true,
		err: failure})
	found, err := hardenBuilt(work, blankConfig(), commands, io.Discard)
	if !errors.Is(err, failure) || len(found) != count.Nothing {
		t.Fatalf("failed build was audited: %v %v", found, err)
	}
}
