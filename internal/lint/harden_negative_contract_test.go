package lint_test

import (
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
)

func TestHardenFindsSetuidWithOthersExecute(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "chmod.go", "package main\nimport \"os\"\n"+
		"func f(){ os.Chmod(\"x\", 06001) }\n")
	found, err := lint.Hardened(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !holds(found, "chmod.go", "sets the setuid or setgid bit") {
		t.Fatalf("setuid with others execute not found: %v", found)
	}
}

func TestHardenLeavesLookalikePackagesAlone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "clean.go", "package main\n"+
		"func f(){ own.Command(\"sudo\"); own.Chmod(\"x\", 0666) }\n")
	writeGo(t, dir, "plain.go", "package main\nimport \"os\"\n"+
		"func g(){ os.Chmod(\"x\", 0001) }\n")
	found, err := lint.Hardened(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != count.Nothing {
		t.Fatalf("lookalike packages produced %v", found)
	}
}

func TestNativeFindingsHaveSourceLineNumbers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "privilege.c", "void f(void) { setuid(0); }\n"+
		"void g(void) { setgid(0); }\n")
	found, err := lint.Hardened(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != count.Next(count.One) {
		t.Fatalf("findings %v", found)
	}
	for index, finding := range found {
		want := count.Next(index)
		if lineOf(finding) != want {
			t.Errorf("finding %d has line %d, want %d", index, lineOf(finding), want)
		}
	}
}

func TestRunAppliesGoHardening(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "setuid.go", "package main\nimport \"syscall\"\n"+
		"func f(){ syscall.Setuid(0) }\n")
	linter := described(t, config.Ruled("harden"))
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !holds(found, "setuid.go", "syscall.Setuid gives the process privileges") {
		t.Fatalf("Go hardening not applied: %v", found)
	}
}

func TestNumberedRulesRejectZeroAndNonNumbers(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"0", "-1", "1x", "ten"} {
		_, err := lint.Describe([]config.Rule{config.Ruled("width", value)})
		if err == nil {
			t.Errorf("width %q was accepted", value)
		}
	}
}

func TestWideCommentsAreFixesAndWideCodeIsLookedAt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "wide.go", "package main\n// xxxxxxxxxxxxxxx\n"+
		"func f(){ _ = \"xxxxxxxxxxxxx\" }\n")
	linter := described(t, config.Ruled("width", "12"))
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	var widths []lint.Finding
	for _, finding := range found {
		if finding.Rule() == "width" {
			widths = append(widths, finding)
		}
	}
	if len(widths) != count.Next(count.One) {
		t.Fatalf("width findings %v", found)
	}
	if widths[count.First].Look() || !widths[count.One].Look() {
		t.Fatalf("width severities %v", widths)
	}
}
