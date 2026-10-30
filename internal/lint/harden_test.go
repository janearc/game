package lint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/lint"
)

// writeGo writes one source file into dir.
func writeGo(t *testing.T, dir, name, body string) {
	t.Helper()
	openFile := os.FileMode(0o644)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), openFile); err != nil {
		t.Fatal(err)
	}
}

// lint harden turns the rule on, and a described harden is not empty.
func TestDescribeHarden(t *testing.T) {
	t.Parallel()
	linter, err := lint.Describe([]config.Rule{config.Ruled("harden")})
	if err != nil {
		t.Fatal(err)
	}
	if !linter.Harden() || linter.Empty() {
		t.Errorf("described harden: %+v", linter)
	}
	other, err := lint.Describe(nil)
	if err != nil || other.Harden() || !other.Empty() {
		t.Errorf("no harden described: %+v %v", other, err)
	}
}

// hardenedGoCases are the go sources the harden rule is asked about,
// each with what it should be found for.
var hardenedGoCases = []struct{ name, body, want string }{
	{"setuid.go",
		"package main\nimport \"syscall\"\nfunc f(){ syscall.Setuid(0) }\n",
		"syscall.Setuid gives the process privileges"},
	{"setgid.go",
		"package main\nimport \"golang.org/x/sys/unix\"\n" +
			"func f(){ unix.Setgid(0) }\n",
		"unix.Setgid gives the process privileges"},
	{"sudo.go",
		"package main\nimport \"os/exec\"\n" +
			"func f(){ exec.Command(\"sudo\", \"x\") }\n",
		"exec.Command(\"sudo\") runs a command as another user"},
	{"su.go",
		"package main\nimport \"os/exec\"\n" +
			"func f(ctx int){ exec.CommandContext(ctx, \"su\", " +
			"\"-c\", \"x\") }\n",
		"exec.CommandContext(\"su\") runs a command as another user"},
	{"chmod.go",
		"package main\nimport \"os\"\n" +
			"func f(){ os.Chmod(\"x\", 04755) }\n",
		"os.Chmod sets the setuid or setgid bit"},
	{"write.go",
		"package main\nimport \"os\"\n" +
			"func f(){ os.WriteFile(\"x\", nil, 0666) }\n",
		"os.WriteFile lets someone other than the owner write"},
	{"mkdir.go",
		"package main\nimport \"os\"\n" +
			"func f(){ os.MkdirAll(\"x\", 0777) }\n",
		"os.MkdirAll lets someone other than the owner write"},
	{"tls.go",
		"package main\nimport \"crypto/tls\"\n" +
			"func f(){ _ = tls.Config{InsecureSkipVerify: true} }\n",
		"tls.Config skips the certificate check"},
}

// holds says whether a finding for file carries text.
func holds(found []lint.Finding, file, text string) bool {
	for _, finding := range found {
		if finding.File() == file && finding.Rule() == "harden" &&
			strings.Contains(finding.Text(), text) {
			return true
		}
	}
	return false
}

// checkHeld is the cases each found and the clean neighbours left
// alone, and exactly one finding a case.
func checkHeld(
	t *testing.T,
	found []lint.Finding,
	cases []struct{ name, body, want string },
	clean []string,
) {
	t.Helper()
	for _, each := range cases {
		if !holds(found, each.name, each.want) {
			t.Errorf("%s: no %q in findings", each.name, each.want)
		}
	}
	for _, finding := range found {
		for _, name := range clean {
			if finding.File() == name {
				t.Errorf("%s was flagged: %v", name, finding)
			}
		}
	}
	if len(found) != len(cases) {
		t.Errorf("got %d findings, want %d: %v", len(found), len(cases),
			found)
	}
}

// the harden rule finds each thing it is asked to, and its clean
// neighbours are left alone.
func TestHardenedFindsEachPattern(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, each := range hardenedGoCases {
		writeGo(t, dir, each.name, each.body)
	}
	writeGo(t, dir, "clean.go",
		"package main\nimport \"os\"\n"+
			"func f(){ os.Mkdir(\"x\", 0755); os.WriteFile(\"y\", nil, 0600) }\n")
	writeGo(t, dir, "own.go",
		"package main\nfunc f(){ own.Setuid(0) }\n")
	writeGo(t, dir, "skipped_test.go",
		"package main\nimport \"syscall\"\n"+
			"func TestF(t *testing.T){ syscall.Setuid(0) }\n")

	found, err := lint.Hardened(dir)
	if err != nil {
		t.Fatal(err)
	}
	checkHeld(t, found, hardenedGoCases,
		[]string{"clean.go", "own.go", "skipped_test.go"})
}

// source that is not Go is of no kind to the harden rule, whether it is
// asked by game harden or by lint harden in a .game.
func TestHardenLeavesOtherLanguagesAlone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "setuid.c", "void f(void) { setuid(0); }\n")
	writeGo(t, dir, "setuid.swift", "func f() { setuid(0) }\n")
	writeGo(t, dir, "a.go", "package a\n")
	found, err := lint.Hardened(dir)
	if err != nil || len(found) != 0 {
		t.Errorf("game harden: %v, %v", found, err)
	}
	found, err = described(t, config.Ruled("harden")).Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range found {
		if finding.Rule() == "harden" {
			t.Errorf("read without a Go file: %v", finding)
		}
	}
}
