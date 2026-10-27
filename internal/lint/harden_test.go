package lint_test

import (
	"os"
	"path/filepath"
	"strconv"
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

// hardenedNativeCases are the C, Objective-C and Swift sources the
// harden rule is asked about.
var hardenedNativeCases = []struct{ name, body, want string }{
	{"setuid.c", "void f(void) { setuid(0); }\n",
		"setuid gives the process privileges"},
	{"chown.c", "void f(void) { chown(\"x\", -1, 0); }\n",
		"chown moves a file to another owner"},
	{"sudo.c", "#include <stdlib.h>\nvoid f(void) { system(\"sudo x\"); }\n",
		"runs a command as another user"},
	{"exec.c", "#include <unistd.h>\n" +
		"void f(void) { execl(\"/usr/bin/sudo\", \"sudo\", NULL); }\n",
		"runs a command as another user"},
	{"chmod.c", "#include <sys/stat.h>\n" +
		"void f(void) { chmod(\"x\", 04755); }\n",
		"chmod sets the setuid or setgid bit"},
	{"open.c", "#include <fcntl.h>\n" +
		"void f(void) { open(\"x\", O_CREAT, 0666); }\n",
		"lets someone other than the owner write"},
	{"mkdir.c", "#include <sys/stat.h>\n" +
		"void f(void) { mkdir(\"x\", 0777); }\n",
		"lets someone other than the owner write"},
	{"setuid.m", "void f(void) { setgid(0); }\n",
		"setgid gives the process privileges"},
	{"root.m", "CFReadStreamSetProperty(r, " +
		"kCFStreamSSLAllowsAnyRoot, kCFBooleanTrue);\n",
		"accepts any certificate any root signs"},
	{"perm.swift", "FileManager.default.createFile(atPath: p, " +
		"contents: d, attributes: [.posixPermissions: 0o666])\n",
		"lets someone other than the owner write"},
	{"proc.swift", "task.launchPath = \"/usr/bin/sudo\"\n",
		"runs a command as another user"},
	{"setuid.swift", "func f() { setuid(0) }\n",
		"setuid gives the process privileges"},
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

// the harden rule reads C, Objective-C and Swift for the same things:
// privilege a process might raise, files opened too widely, and a check
// skipped. clean neighbours and comments are left alone.
func TestHardenedFindsNativePatterns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, each := range hardenedNativeCases {
		writeGo(t, dir, each.name, each.body)
	}
	writeGo(t, dir, "clean.c",
		"void f(void) { open(\"x\", O_CREAT, 0644); "+
			"mkdir(\"x\", 0755); }\n")
	writeGo(t, dir, "clean.swift",
		"FileManager.default.createFile(atPath: p, contents: d, "+
			"attributes: [.posixPermissions: 0o644])\n")
	writeGo(t, dir, "comment.c", "// setuid(0) is not a call here\n")
	writeGo(t, dir, "directive.m", "#define setuid(x) evil(x)\n")

	found, err := lint.Hardened(dir)
	if err != nil {
		t.Fatal(err)
	}
	checkHeld(t, found, hardenedNativeCases,
		[]string{"clean.c", "clean.swift", "comment.c", "directive.m"})
}

// lint harden in a .game holds the native sources too, as game harden
// does, so the lint gate and the verb agree; without it they are of no
// kind to lint at all.
func TestRunWithHardenReadsNativeSources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "setuid.c", "void f(void) { setuid(0); }\n")
	writeGo(t, dir, "a.go", "package a\n")
	hardened := described(t, config.Ruled("harden"))
	found, err := hardened.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !holds(found, "setuid.c", "setuid gives the process privileges") {
		t.Errorf("no native finding in %v", found)
	}
	width := int(held(t, "lint-width"))
	rule := strconv.Itoa(width)
	plain := described(t, config.Ruled("width", rule))
	found, err = plain.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range found {
		if finding.File() == "setuid.c" {
			t.Errorf("read without harden: %v", finding)
		}
	}
}
