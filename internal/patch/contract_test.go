package patch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

func gitFailure(t *testing.T, script string) {
	t.Helper()
	actual, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	path := filepath.Join(bin, "git")
	body := "#!/bin/sh\n" + script + "\nexec \"$PATCH_REAL_GIT\" \"$@\"\n"
	mode := os.FileMode(0o755)
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATCH_REAL_GIT", actual)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestResultsNameThePatchesTheyDescribe(t *testing.T) {
	dir, root := member(t, map[string]string{"first.patch": first})
	results, err := lay(t, dir, root, false)
	if err != nil || len(results) != count.One {
		t.Fatalf("patch results: %v %v", results, err)
	}
	want := filepath.Join(root, "patches", "member", "first.patch")
	if results[count.First].Patch() != want || !results[count.First].Landed() {
		t.Fatalf("patch identity or result changed: %v", results)
	}
}

func TestApplyFailureAfterCheckRollsBack(t *testing.T) {
	dir, root := member(t, map[string]string{"first.patch": first})
	gitFailure(t, "if [ \"$3\" = apply ] && [ \"$4\" != --check ]; then\n"+
		"echo 'apply refused' >&2; exit 1\nfi")
	results, err := lay(t, dir, root, false)
	if err == nil || !strings.Contains(err.Error(), "apply refused") ||
		len(results) != count.One || results[count.First].Landed() {
		t.Fatalf("failed apply: %v %v", results, err)
	}
	if file(t, dir) != "a b c " {
		t.Fatal("failed apply changed the member")
	}
}

func TestDryRunReportsCleanupFailure(t *testing.T) {
	dir, root := member(t, map[string]string{"first.patch": first})
	gitFailure(t, "if [ \"$3\" = clean ]; then\n"+
		"echo 'cleanup refused' >&2; exit 1\nfi")
	results, err := lay(t, dir, root, true)
	if err == nil || !strings.Contains(err.Error(), "patch: cleaning") ||
		!strings.Contains(err.Error(), "cleanup refused") || len(results) != count.One {
		t.Fatalf("cleanup failure: %v %v", results, err)
	}
	if !results[count.First].Landed() || file(t, dir) != "a b c " {
		t.Fatal("cleanup failure lost patch result or skipped tracked-file reset")
	}
}

func TestPatchRefusesMissingReportedRoot(t *testing.T) {
	dir, root := member(t, map[string]string{"first.patch": first})
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("PATCH_REPORTED_ROOT", missing)
	gitFailure(t, "if [ \"$3\" = rev-parse ]; then\n"+
		"printf '%s\\n' \"$PATCH_REPORTED_ROOT\"; exit 0\nfi")
	results, err := lay(t, dir, root, false)
	if !os.IsNotExist(err) || len(results) != count.Nothing {
		t.Fatalf("missing reported root: %v %v", results, err)
	}
	if file(t, dir) != "a b c " {
		t.Fatal("member changed without a verified root")
	}
}
