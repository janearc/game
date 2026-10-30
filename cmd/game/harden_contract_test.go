package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

func TestHardenRefusesUnreadableModuleMetadata(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Symlink("go.mod", filepath.Join(dir, "go.mod")); err != nil {
		t.Fatal(err)
	}
	stand := &asked{}
	if _, err := hardenGo(dir, stand.run); err == nil {
		t.Fatal("unreadable module metadata was treated as no module")
	}
	if count.Positive(len(stand.said)) {
		t.Fatalf("analyzers ran after metadata failed: %v", stand.said)
	}
	var out bytes.Buffer
	if err := hardenAll(dir, stand.run, &out); err == nil {
		t.Fatal("harden reported clean after module metadata failed")
	}
	if out.Len() != count.Nothing {
		t.Fatalf("metadata failure produced a success report: %q", out.String())
	}
}

func TestHardenRefusesMissingSourceDirectory(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "missing")
	var out bytes.Buffer
	if err := hardenAll(dir, (&asked{}).run, &out); err == nil {
		t.Fatal("missing source directory was reported clean")
	}
	if out.Len() != count.Nothing {
		t.Fatalf("missing directory produced a report: %q", out.String())
	}
}

func TestHardenVerbRejectsAWord(t *testing.T) {
	t.Parallel()
	work := repoAt(t.TempDir())
	err := viaPzm(t, "harden", hardenVerb, work, blankConfig(), []string{"unknown"})
	if err == nil || errors.Is(err, errPzmRefused) {
		t.Fatalf("a word to harden: %v", err)
	}
}

func TestHardenAnalyzerFailuresStayFindings(t *testing.T) {
	standTools(t, "gosec", "govulncheck")
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module example.invalid/check\n")
	stand := &asked{fail: "gosec"}
	found, err := hardenGo(dir, stand.run)
	if err != nil || len(found) != count.One ||
		!strings.Contains(found[count.First].Text(), "gosec failed: no:") {
		t.Fatalf("gosec failure was lost: %v %v", found, err)
	}
	stand = &asked{fail: "govulncheck"}
	found = govulnFindings(dir, stand.run)
	if len(found) != count.One ||
		!strings.Contains(found[count.First].Text(), "govulncheck failed: no:") {
		t.Fatalf("govulncheck failure was lost: %v", found)
	}
}

func TestHardenRelativePathsKeepOutsideAndRelativeNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.go")
	if got := relTo(dir, outside); got != outside {
		t.Fatalf("outside path changed to %q", got)
	}
	if got := relTo(dir, "relative.go"); got != "relative.go" {
		t.Fatalf("relative path changed to %q", got)
	}
	if got := relTo("relative-root", outside); got != outside {
		t.Fatalf("incompatible paths changed to %q", got)
	}
}
