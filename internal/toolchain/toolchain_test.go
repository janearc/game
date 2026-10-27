package toolchain_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/toolchain"
)

// module is a directory holding a go.mod of the given text.
func module(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(path, []byte(text), openFile); err != nil {
		t.Fatal(err)
	}
	return dir
}

// oldest is a go older than any module asks for, so that whatever a go.mod
// asks for is newer than it and Explain says it.
const oldest = "go1.0"

// asked is the go Explain says a module asks for, or "" when it says
// nothing of the sort. the error it is given is a sentinel, so a reply that
// is the sentinel alone asks for nothing.
func asked(t *testing.T, dir string) string {
	t.Helper()
	sentinel := errors.New("a parse error")
	err := toolchain.Explain(dir, oldest, sentinel)
	if err == sentinel {
		return ""
	}
	_, after, found := strings.Cut(err.Error(), "asks for ")
	if !found {
		t.Fatalf("%v neither is the error alone nor says what is asked", err)
	}
	want, _, _ := strings.Cut(after, " ")
	return want
}

func TestWants(t *testing.T) {
	t.Parallel()
	for _, row := range []struct{ text, want string }{
		{"module x\n\ngo 1.26\n", "go1.26"},
		{"module x\n\ngo 1.26\n\ntoolchain go1.27.0\n", "go1.27.0"},
		{"module x\n\ngo 1.27.1\n\ntoolchain go1.27.0\n", "go1.27.1"},
		{"module x\n\ngo 1.27.0 // a comment\n", "go1.27.0"},
		{"module x\n\ngo nonsense\n", ""},
		{"module x\n\ngo 1.27rc1\n", "go1.27rc1"},
		{"module x\n\ngo 1.26\n\ntoolchain default\n", "go1.26"},
		{"module x\n\ngo\t1.27.0\n", "go1.27.0"},
		{"module x\n", ""},
	} {
		if got := asked(t, module(t, row.text)); got != row.want {
			t.Errorf("%q wants %q, not %q", row.text, row.want, got)
		}
	}
	if got := asked(t, t.TempDir()); got != "" {
		t.Errorf("no go.mod wants %q", got)
	}
}

func TestExplain(t *testing.T) {
	t.Parallel()
	parse := errors.New("app.go:773:26: method must have no type parameters")
	newer := module(t, "module x\n\ngo 1.27.0\n")
	err := toolchain.Explain(newer, "go1.26.4", parse)
	if !errors.Is(err, parse) {
		t.Errorf("the parse error is lost: %v", err)
	}
	for _, want := range []string{
		"asks for go1.27.0", "built with go1.26.4", "build game with go1.27.0",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q does not say %q", err, want)
		}
	}
	same := module(t, "module x\n\ngo 1.26.4\n")
	older := module(t, "module x\n\ngo 1.25\n")
	for _, dir := range []string{same, older, t.TempDir()} {
		if got := toolchain.Explain(dir, "go1.26.4", parse); got != parse {
			t.Errorf("%s: %v, not the error alone", dir, got)
		}
	}
	if got := toolchain.Explain(newer, "devel go1.27-0d3b2a1", parse); got != parse {
		t.Errorf("a devel go: %v, not the error alone", got)
	}
	if got := toolchain.Explain(newer, "go1.26.4", nil); got != nil {
		t.Errorf("nil became %v", got)
	}
}
