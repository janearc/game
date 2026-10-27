package patch_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/patch"
)

// TestSeriesRefusesARootThatIsNotAbsolute: the patches are laid from the
// member's clone, so a series made from a relative root, or from none as
// happens when the working directory cannot be read, would point nowhere.
func TestSeriesRefusesARootThatIsNotAbsolute(t *testing.T) {
	t.Parallel()
	for _, root := range []string{"root", ".", ""} {
		_, err := patch.Series(root, nil, "member")
		if err == nil || !strings.Contains(err.Error(), "the stack's root") {
			t.Errorf("root %q: %v", root, err)
		}
	}
}

// TestSeriesTakesAnAbsoluteRoot: an absolute root is read as it is.
func TestSeriesTakesAnAbsoluteRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeIn(t, filepath.Join(root, "patches", "member"), "01.patch", "x")
	series, err := patch.Series(root, nil, "member")
	want := filepath.Join(root, "patches", "member", "01.patch")
	if err != nil || !slices.Equal(series, []string{want}) {
		t.Fatalf("an absolute root: %v, %v", series, err)
	}
}

// TestBothPlacesAreNamedFromTheStackRoot: the refusal names each place
// from the root of the stack, not the machine.
func TestBothPlacesAreNamedFromTheStackRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeIn(t, filepath.Join(root, "patches", "member"), "a.patch", "x")
	writeIn(t, filepath.Join(root, "elsewhere", "member"), "b.patch", "x")
	_, err := patch.Series(root, []string{"elsewhere"}, "member")
	if err == nil || !strings.Contains(err.Error(),
		"both patches/member and elsewhere/member") ||
		strings.Contains(err.Error(), root) {
		t.Fatalf("places named: %v", err)
	}
}

func TestLayPropagatesSecondPathResolutionFailure(t *testing.T) {
	root := t.TempDir()
	clone := filepath.Join(root, "clone")
	writeIn(t, clone, "source", "text")
	tools := t.TempDir()
	stub := filepath.Join(tools, "git")
	writeIn(t, tools, "git", "#!/bin/sh\n"+
		"if [ \"$3\" = rev-parse ]; then\n"+
		"printf '%s\\n' '"+root+"'\n/bin/rm -rf \"$2\"\nfi\n")
	mode := os.FileMode(0o755)
	if err := os.Chmod(stub, mode); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools)
	if _, err := patch.Lay(clone, []string{"unused"}, false); err == nil {
		t.Fatal("second path resolution failure was discarded")
	}
}
