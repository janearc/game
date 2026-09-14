package lint

import (
	"os"
	"path/filepath"
	"testing"
)

// a module with one of everything: an uncommented function, a bare
// print in a library, a buzzword, an exclamation mark in a doc, and a
// shout in a comment that is only reported.
func TestRun(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "lib"), 0o755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\n// main is fine.\nfunc main() { helper() }\n\nfunc helper() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "lib", "lib.go"), []byte("package lib\n\nimport \"fmt\"\n\n// Say is a blazing"+"-fast HELLOWORLD, JSON allowed.\nfunc Say() { fmt.Println(\"x\") }\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# x\n\nwow!\n\n```\nfine!\n```\n"), 0o644)
	fs, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range fs {
		got[f.Rule]++
	}
	want := map[string]int{"uncommented": 1, "bare print": 1, "buzzword": 1, "exclamation": 1, "shouting": 1}
	for rule, n := range want {
		if got[rule] != n {
			t.Errorf("%s: got %d want %d; all: %v", rule, got[rule], n, fs)
		}
	}
	if !Failed(fs) {
		t.Error("nothing failed")
	}
	only := []Finding{{Rule: "shouting", Look: true}}
	if Failed(only) {
		t.Error("a look failed the lint")
	}
}
