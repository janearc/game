package lint

import (
	"os"
	"os/exec"
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
	os.WriteFile(
		filepath.Join(dir, "main.go"),
		[]byte(
			"package main\n\n// main is fine.\nfunc "+
				"main() { helper() }\n\nfunc helper() {}\n",
		),
		0o644,
	)
	os.WriteFile(
		filepath.Join(dir, "lib", "lib.go"),
		[]byte(
			"package lib\n\nimport \"fmt\"\n\n// Say is a blazing"+"-fast HELLOWORLD, JSON allowed.\nfunc Say() { fmt.Println(\"x\") }\n",
		),
		0o644,
	)
	wide := strings.Repeat("x", 81)
	os.WriteFile(
		filepath.Join(dir, "README.md"),
		[]byte(
			"# x\n\nwow!\n\n```\nfine!\n```\n"+wide+"\n| "+wide+" |\n",
		),
		0o644,
	)
	os.WriteFile(
		filepath.Join(dir, "wide.go"),
		[]byte(
			"package main\n\n// "+wide+"\nfunc wide() { _ = \""+wide+"\" }\n",
		),
		0o644,
	)
	os.WriteFile(
		filepath.Join(dir, "tall", "README.md"),
		[]byte(strings.Repeat("x\n", 26)),
		0o644,
	)
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
	want := map[string]int{
		"uncommented": 1,
		"bare print":  1,
		"word":        1,
		"exclamation": 1,
		"shouting":    1,
		"width":       3,
		"height":      1,
	}
	for rule, n := range want {
		if got[rule] != n {
			t.Errorf(
				"%s: got %d want %d; all: %v",
				rule,
				got[rule],
				n,
				fs,
			)
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

// a file not yet added is linted, and an ignored one is not: journeyer
// found a tree called clean while four new lines were over the width.
func TestTrackedSeesNewFiles(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	write := func(rel, body string) {
		path := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(body), 0o644)
	}
	write(".gitignore", "build/\n")
	write("old.go", "package x\n")
	write("new.go", "package x\n")
	write("build/vendor.go", "package y\n")
	git("add", "old.go", ".gitignore")
	files, err := tracked(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f] = true
	}
	for _, want := range []string{"old.go", "new.go", ".gitignore"} {
		if !got[want] {
			t.Errorf("%s not linted; got %v", want, files)
		}
	}
	if got["build/vendor.go"] {
		t.Errorf("an ignored file was linted; got %v", files)
	}
}

// a paragraph of prose longer than the rule fails, in a doc and in a
// comment; a list item is its own paragraph; code in a fence, a table
// and an indented block in a comment are not prose.
func TestParagraph(t *testing.T) {
	dir := t.TempDir()
	five := "one\ntwo\nthree\nfour\nfive\n"
	doc := "# title\n\n" + five + "\n- a\n- b\n- c\n- d\n- e\n\n" +
		"```\n" + five + "```\n\n| a |\n| b |\n| c |\n| d |\n| e |\n"
	os.WriteFile(filepath.Join(dir, "doc.md"), []byte(doc), 0o644)
	src := "package x\n\n// f runs.\n// " + strings.ReplaceAll(
		strings.TrimSpace(five),
		"\n",
		"\n// ",
	) +
		"\n//\n//\tone\n//\ttwo\n//\tthree\n//\tfour\n//\tfive\nfunc " +
		"f() {}\n"
	os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644)
	l, err := Describe([]struct {
		Name string
		Args []string
	}{{"paragraph", []string{"4"}}})
	if err != nil {
		t.Fatal(err)
	}
	fs, err := l.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range fs {
		if f.Rule == "paragraph" {
			got[f.File]++
		}
	}
	if got["doc.md"] != 1 {
		t.Errorf(
			"doc.md: want only the five-line paragraph, got %d: %v",
			got["doc.md"],
			fs,
		)
	}
	if got["x.go"] != 1 {
		t.Errorf(
			"x.go: want the six-line comment paragraph, "+
				"not the code, got %d: %v",
			got["x.go"],
			fs,
		)
	}
	if !Failed(fs) {
		t.Error("a long paragraph did not fail")
	}
}

// a file's name and a quoted name are not shouting, nor are the signal
// names; a struct field's tag is never too wide to fix.
// a usage line in a doc comment is code, so its argument names are not
// shouting, while the prose around it still is.
func TestBlocky(t *testing.T) {
	for _, line := range []string{
		"//\tgame bounce [RANGE]",
		"//    set     CONTACT ada@example.com",
		" *\tgame run NAME",
	} {
		if !blocky(line) {
			t.Errorf("not read as code: %q", line)
		}
	}
	for _, line := range []string{
		"// the RANGE is a commit or a range of them",
		"//",
		"// set CONTACT to who answers",
	} {
		if blocky(line) {
			t.Errorf("read as code: %q", line)
		}
	}
}

// what shouts and what does not: a word in capitals is shouting, a file
// name and a backticked name are not, and a struct field's tag is never
// too wide, since the tag is one literal and cannot be split.
func TestShoutedAndTagged(t *testing.T) {
	if got := shouted("see DESIGN.md and `README`, on SIGTERM, NEVER"); len(
		got,
	) != 1 ||
		got[0] != "NEVER" {
		t.Errorf("shouted: %v", got)
	}
	long := strings.Repeat("x", 90)
	if !tagged.MatchString("\tName string `json:\"" + long + "\"`") {
		t.Error("a tagged field was not recognised")
	}
	if !tagged.MatchString("\tA, B int `json:\"" + long + "\"` // both") {
		t.Error("a tagged pair with a comment was not recognised")
	}
	inline := "\t\tMeta struct{ Name, Namespace string } `json:\"" +
		long + "\"`"
	if !tagged.MatchString(inline) {
		t.Error(
			"a tagged field of an inline struct type " +
				"was not recognised",
		)
	}
	if !tagged.MatchString(
		"\tItems []struct{ A int } `json:\"" + long + "\"`",
	) {
		t.Error("a tagged slice of an inline struct was not recognised")
	}
	if tagged.MatchString("\tf := func() { `" + long + "` }") {
		t.Error(
			"a raw string in a function literal was " +
				"taken for a tag",
		)
	}
	if tagged.MatchString("\tx := `" + long + "`") {
		t.Error("a raw string assignment was taken for a tag")
	}
}

// a test file is not linted unless the config asks for it, and a name
// with capitals in it is a name wherever the capitals sit.
func TestTestsAndNames(t *testing.T) {
	for _, path := range []string{
		"store_test.go", "clients/go/flipr_test.go",
		"testdata/x.json", "a/testdata/x.json", "test_ingest.py",
		"client.test.ts",
	} {
		if !testy(path) {
			t.Errorf("%q is a test and was not taken for one", path)
		}
	}
	for _, path := range []string{"store.go", "tester.go", "protest.md"} {
		if testy(path) {
			t.Errorf("%q is not a test and was taken for one", path)
		}
	}
	for _, line := range []string{
		"// see RUNBOOK-restore.md for the page",
		"// DESIGN.md says why",
		"// doc/OPERATION.md, at 03:20",
	} {
		if got := shouted(line); len(got) != 0 {
			t.Errorf("a file name shouted in %q: %v", line, got)
		}
	}
	if got := shouted("// THE CLIENT ANNOUNCES ITSELF"); len(got) != 3 {
		t.Errorf("real shouting was missed: %v", got)
	}
}

// a readme opens with a picture, and the picture is not prose: the height
// is what is left after the title and one block of art at the top.
func TestHeaderIsArt(t *testing.T) {
	art := []string{"# flipr", "", "    ~~~ dolphin ~~~", "    ~~ ~~", "",
		"## name", "flipr -- the flag store"}
	if got := header(art); got != 5 {
		t.Errorf("header is %d lines, want 5", got)
	}
	fenced := []string{"", "# x", "", "```", "art", "```", "text"}
	if got := header(fenced); got != 6 {
		t.Errorf("fenced header is %d lines, want 6", got)
	}
	prose := []string{"# x", "", "words about x", "", "    code later"}
	if got := header(prose); got != 0 {
		t.Errorf("a document with no art has a header of %d", got)
	}
}

// an import path is one token: it cannot be split, so its width is not a
// finding, while a long string in code still is.
func TestImportedIsNotWide(t *testing.T) {
	long := strings.Repeat("x", 90)
	for _, line := range []string{
		"\tobservabilityproto \"github.com/" + long + "\"",
		"\t\"github.com/" + long + "\"",
		"\t_ \"github.com/" + long + "\"",
	} {
		if !imported(line) {
			t.Errorf("not read as an import: %q", line[:40])
		}
	}
	for _, line := range []string{
		"\ts := \"" + long + "\"",
		"\treturn \"" + long + "\"",
	} {
		if imported(line) {
			t.Errorf("read as an import: %q", line[:40])
		}
	}
}

// a line the repository says to leave alone is left alone, and the marker
// may sit on the line or on the line above it.
func TestAllowedHere(t *testing.T) {
	lines := []string{
		"\tx := 1 // game: allow width",
		"\t// game: allow width",
		"\ty := 2",
		"\tz := 3",
		"\t// game: allow paragraph",
		"\tw := 4",
	}
	if !allowedHere(lines, 0, "width") {
		t.Error("a marker on the line was not honoured")
	}
	if !allowedHere(lines, 2, "width") {
		t.Error("a marker above the line was not honoured")
	}
	if allowedHere(lines, 3, "width") {
		t.Error("a line with no marker was allowed")
	}
	if allowedHere(lines, 5, "width") {
		t.Error("a marker for another rule allowed this one")
	}
}

// a readme's header art is exempt from the width as well as the height: a
// picture is not wrapped.
func TestHeaderArtIsNotMeasured(t *testing.T) {
	wide := "    " + strings.Repeat("~", 90)
	src := "# x\n\n" + wide + "\n\n## name\n\nshort prose\n"
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "README.md"), []byte(src), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	l := Lint{Width: 80, Rows: 25}
	found, err := l.doc(filepath.Join(dir, "README.md"), "README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("the header was measured: %v", f)
	}
}

// TestAllow is the file-scoped exemption: the page named is let off the
// rules named, and every other page is still held to them.
func TestAllow(t *testing.T) {
	dir := t.TempDir()
	long := "a line of prose.\n" +
		"and another one.\n" +
		"and a third.\n" +
		"and a fourth.\n" +
		"and a fifth, which is over.\n"
	for _, name := range []string{"OPERATION.md", "README.md"} {
		if err := os.WriteFile(
			filepath.Join(dir, name),
			[]byte("# a page\n\n"+long),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
	rules := []struct {
		Name string
		Args []string
	}{
		{Name: "paragraph", Args: []string{"4"}},
		{Name: "allow", Args: []string{"OPERATION.md", "paragraph"}},
	}
	l, err := Describe(rules)
	if err != nil {
		t.Fatal(err)
	}
	found, err := l.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		if f.File == "OPERATION.md" {
			t.Errorf("the exempt page was still held: %s", f)
		}
	}
	held := false
	for _, f := range found {
		held = held || (f.File == "README.md" && f.Rule == "paragraph")
	}
	if !held {
		t.Error("the page that was not exempt went unreported")
	}
}

// TestAllowRows is the card waived by the name the config uses, rows,
// against the finding the page gets, height.
func TestAllowRows(t *testing.T) {
	dir := t.TempDir()
	tall := "# a page\n\n" + strings.Repeat("a line of prose.\n", 30)
	if err := os.WriteFile(
		filepath.Join(dir, "README.md"), []byte(tall), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	l, err := Describe([]struct {
		Name string
		Args []string
	}{
		{Name: "rows", Args: []string{"25"}},
		{Name: "allow", Args: []string{"README.md", "rows"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := l.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		if f.Rule == "height" {
			t.Errorf("the waived page was still measured: %s", f)
		}
	}
}
