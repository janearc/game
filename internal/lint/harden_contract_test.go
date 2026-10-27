package lint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
)

func TestHardenDoesNotGuessDynamicOrIncompleteExpressions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := "package sample\nfunc inspect() {\n" +
		"holder.command.Run(); exec.Command(); exec.Command(name);\n" +
		"exec.Command(1); os.Chmod(); os.Chmod(name); os.Chmod(name, mode);\n" +
		"os.Chmod(name, 1.5); os.Chmod(name, 0600);\n" +
		"_ = tls.Other{InsecureSkipVerify: true}; _ = local{};\n" +
		"_ = other.Config{InsecureSkipVerify: true};\n" +
		"_ = tls.Config{false}; _ = tls.Config{Other: true};\n" +
		"_ = tls.Config{InsecureSkipVerify: false};\n" +
		"_ = tls.Config{InsecureSkipVerify: check()};\n" +
		"_ = tls.Config{\"InsecureSkipVerify\": true};\n}\n"
	writeGo(t, dir, "sample.go", body)
	found, err := lint.Hardened(dir)
	if err != nil || len(found) != count.Nothing {
		t.Fatalf("dynamic or incomplete expressions guessed: %v %v", found, err)
	}
}

func TestHardenDoesNotReadUnrelatedFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "notes.txt", "setuid(0)\n")
	writeGo(t, dir, "safe.mm", "void safe(void) {}\n")
	found, err := lint.Hardened(dir)
	if err != nil || len(found) != count.Nothing {
		t.Fatalf("non-source text was inspected: %v %v", found, err)
	}
}

func TestHardenRefusesMissingDirectory(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := lint.Hardened(missing); err == nil {
		t.Fatal("missing source directory was reported clean")
	}
}

func TestHardenRefusesMalformedGo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "broken.go", "package broken\nfunc {\n")
	if _, err := lint.Hardened(dir); err == nil {
		t.Fatal("malformed Go was reported clean")
	}
}

func TestHardenRefusesUnreadableNativeSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.c")
	if err := os.Symlink("absent", path); err != nil {
		t.Fatal(err)
	}
	if _, err := lint.Hardened(dir); err == nil {
		t.Fatal("unreadable native source was reported clean")
	}
}

func TestHardenFindsDescriptorSetIDAndOverflowingModes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeGo(t, dir, "descriptor.go", "package sample\n"+
		"func inspect() { os.Fchmod(fd, 04755) }\n")
	writeGo(t, dir, "overflow.go", "package sample\n"+
		"func inspect() { os.Chmod(name, 18446744073709551615) }\n")
	found, err := lint.Hardened(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != count.One ||
		!holds(found, "descriptor.go", "sets the setuid or setgid bit") {
		t.Fatalf("descriptor mode or overflow handled incorrectly: %v", found)
	}
}

func TestDescribeRefusesUnknownRules(t *testing.T) {
	t.Parallel()
	_, err := lint.Describe([]config.Rule{config.Ruled("not-a-rule")})
	if err == nil || !strings.Contains(err.Error(), "unknown lint") {
		t.Fatalf("unknown rule: %v", err)
	}
}
