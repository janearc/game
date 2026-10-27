package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// game harden reports a finding as a fix and fails with its count, the
// tools standing in and saying nothing so the source alone is counted.
func TestHardenReportsAndFails(t *testing.T) {
	standTools(t, "semgrep")
	dir := t.TempDir()
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(filepath.Join(dir, "a.go"),
		[]byte("package main\nimport \"os\"\n"+
			"func f(){ os.Chmod(\"x\", 04755) }\n"), openFile); err != nil {
		t.Fatal(err)
	}
	stand := &asked{gives: map[string]string{"semgrep": `{"results":[]}`}}
	var out bytes.Buffer
	err := hardenBy(dir, nil, stand.run, &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "1 to fix") {
		t.Errorf("err %v", err)
	}
	if !strings.Contains(out.String(), "os.Chmod sets the setuid or setgid bit") {
		t.Errorf("said %q", out.String())
	}
}

// game harden with nothing to find says clean, and a tool not standing
// is itself a finding rather than a pass.
func TestHardenSaysClean(t *testing.T) {
	standTools(t, "semgrep")
	dir := t.TempDir()
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(filepath.Join(dir, "a.go"),
		[]byte("package main\nfunc main() {}\n"), openFile); err != nil {
		t.Fatal(err)
	}
	stand := &asked{gives: map[string]string{"semgrep": `{"results":[]}`}}
	var out bytes.Buffer
	if err := hardenBy(dir, nil, stand.run, &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "harden: clean\n" {
		t.Errorf("said %q", out.String())
	}
	standTools(t)
	out.Reset()
	err := hardenBy(dir, nil, stand.run, &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(out.String(), "semgrep did not run") {
		t.Errorf("semgrep out: %v, said %q", err, out.String())
	}
}

// game harden is a verb, and refuses a word that is not xcode.
func TestHardenVerbIsRegistered(t *testing.T) {
	t.Parallel()
	if verbs["harden"] == nil {
		t.Fatal("harden is no verb")
	}
	err := hardenBy(t.TempDir(), []string{"rust"}, (&asked{}).run,
		&bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, errHardenLanguage) {
		t.Errorf("rust: %v", err)
	}
}
