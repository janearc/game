package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a module with one of everything: an uncommented function, a bare
// print in a library, a buzzword, an exclamation mark in a doc, and a
// shout in a comment that is only reported.
func TestRun(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "lib"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tall"), 0o755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\n// main is fine.\nfunc main() { helper() }\n\nfunc helper() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "lib", "lib.go"), []byte("package lib\n\nimport \"fmt\"\n\n// Say is a blazing"+"-fast HELLOWORLD, JSON allowed.\nfunc Say() { fmt.Println(\"x\") }\n"), 0o644)
	wide := strings.Repeat("x", 81)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# x\n\nwow!\n\n```\nfine!\n```\n"+wide+"\n| "+wide+" |\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "wide.go"), []byte("package main\n\n// "+wide+"\nfunc wide() { _ = \""+wide+"\" }\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "tall", "README.md"), []byte(strings.Repeat("x\n", 26)), 0o644)
	l, err := Describe([]struct {
		Name string
		Args []string
	}{{"width", []string{"80"}}, {"rows", []string{"25"}}, {"comments", nil}, {"print", nil}, {"exclaim", nil}, {"shout", nil}, {"words", []string{"blazing" + "-fast"}}})
	if err != nil {
		t.Fatal(err)
	}
	fs, err := l.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range fs {
		got[f.Rule]++
	}
	// width: the doc's wide line fails, its table row does not; the
	// wide comment fails, the wide code line is only a look.
	want := map[string]int{"uncommented": 1, "bare print": 1, "word": 1, "exclamation": 1, "shouting": 1, "width": 3, "height": 1}
	for rule, n := range want {
		if got[rule] != n {
			t.Errorf("%s: got %d want %d; all: %v", rule, got[rule], n, fs)
		}
	}
	if !Failed(fs) {
		t.Error("nothing failed")
	}
	looks := 0
	for _, f := range fs {
		if f.Rule == "width" && f.Look {
			looks++
		}
	}
	if looks != 1 {
		t.Errorf("wide code should be one look, got %d", looks)
	}
	only := []Finding{{Rule: "shouting", Look: true}}
	if Failed(only) {
		t.Error("a look failed the lint")
	}
}

// nothing described is no lint at all, and a bad rule is an error.
func TestDescribe(t *testing.T) {
	l, err := Describe(nil)
	if err != nil || !l.Empty() {
		t.Fatalf("empty: %+v %v", l, err)
	}
	if _, err := Describe([]struct {
		Name string
		Args []string
	}{{"width", []string{"wide"}}}); err == nil {
		t.Error("a bad width described")
	}
}
