package lint_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
)

// lintWidth is the width rule the tests hold the lint to, as text.
func lintWidth(t *testing.T) string {
	t.Helper()
	width := int(held(t, "lint-width"))
	return strconv.Itoa(width)
}

// lintRows is the rows rule the tests hold the lint to, as text.
func lintRows(t *testing.T) string {
	t.Helper()
	rows := int(held(t, "lint-rows"))
	return strconv.Itoa(rows)
}

// described is the lint of the rules given, or the test stops.
func described(t *testing.T, rules ...config.Rule) lint.Lint {
	t.Helper()
	linter, err := lint.Describe(rules)
	if err != nil {
		t.Fatal(err)
	}
	return linter
}

// rulesOf is the rules of each finding, in order.
func rulesOf(found []lint.Finding) string {
	var out []string
	for _, finding := range found {
		out = append(out, finding.Rule())
	}
	return strings.Join(out, ",")
}

// everything is a module with one of everything: an uncommented
// function, a bare print in a library, a buzzword, an exclamation mark
// in a doc, a shout in a comment, a wide comment, a wide code line, a
// tall readme and art at the top of one.
func everything(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	width := int(held(t, "lint-width"))
	rows := int(held(t, "lint-rows"))
	wide := strings.Repeat("x", count.Next(width))
	tall := strings.Repeat("x\n", count.Next(rows))
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	files := map[string]string{
		"main.go": "package main\n\n// main is fine.\nfunc " +
			"main() { helper() }\n\nfunc helper() {}\n",
		"lib/lib.go": "package lib\n\nimport \"fmt\"\n\n" +
			"// Say is a blazing" + "-fast HELLOWORLD, JSON allowed.\n" +
			"func Say() { fmt.Println(\"x\") }\n",
		"README.md": "# x\n\nwow!\n\n```\nfine!\n```\n" + wide +
			"\n| " + wide + " |\n",
		"wide.go": "package main\n\n// " + wide +
			"\nfunc wide() { _ = \"" + wide + "\" }\n",
		"tall/README.md": tall,
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), openFile); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// a module with one of everything lints to one finding of each kind.
func TestRun(t *testing.T) {
	dir := everything(t)
	width := lintWidth(t)
	rows := lintRows(t)
	linter, err := lint.Describe([]config.Rule{
		config.Ruled("width", width),
		config.Ruled("rows", rows),
		config.Ruled("comments"),
		config.Ruled("print"),
		config.Ruled("exclaim"),
		config.Ruled("shout"),
		config.Ruled("words", "blazing"+"-fast"),
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	checkRun(t, found)
	only := []lint.Finding{looked(t)}
	if fix, _, _ := lint.Verdict(only); fix != count.Nothing {
		t.Error("a look was a finding to fix")
	}
}

// checkRun is the everything module's tally: one finding of each kind,
// and the wide code line only a look.
func checkRun(t *testing.T, found []lint.Finding) {
	t.Helper()
	got := map[string]int{}
	for _, finding := range found {
		got[finding.Rule()]++
	}
	want := map[string]int{
		"uncommented": count.One,
		"bare print":  count.One,
		"word":        count.One,
		"exclamation": count.One,
		"shouting":    count.One,
		"width":       int(held(t, "width-findings")),
		"height":      count.One,
	}
	for rule, wanted := range want {
		if got[rule] != wanted {
			t.Errorf("%s: got %d want %d; all: %v", rule, got[rule],
				wanted, found)
		}
	}
	if _, _, err := lint.Verdict(found); err == nil {
		t.Error("nothing failed")
	}
	looks := count.Nothing
	for _, finding := range found {
		if finding.Rule() == "width" && finding.Look() {
			looks++
		}
	}
	if looks != count.One {
		t.Errorf("wide code should be one look, got %d", looks)
	}
}

// nothing described is no lint at all, and a bad rule is an error.
func TestDescribe(t *testing.T) {
	linter, err := lint.Describe(nil)
	if err != nil || !linter.Empty() {
		t.Fatalf("empty: %+v %v", linter, err)
	}
	if _, err := lint.Describe([]config.Rule{
		config.Ruled("width", "wide"),
	}); err == nil {
		t.Error("a bad width described")
	}
}

// a file not yet added is linted, and an ignored one is not: journeyer
// found a tree called clean while four new lines were over the width.
func TestTrackedSeesNewFiles(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	writeIn(t, dir, ".gitignore", "build/\n")
	writeIn(t, dir, "old.md", "synergy\n")
	writeIn(t, dir, "new.md", "synergy\n")
	writeIn(t, dir, "build/vendor.md", "synergy\n")
	gitIn(t, dir, "add", "old.md", ".gitignore")
	files := filesFound(t, dir)
	got := map[string]bool{}
	for _, file := range files {
		got[file] = true
	}
	for _, want := range []string{"old.md", "new.md"} {
		if !got[want] {
			t.Errorf("%s not linted; got %v", want, files)
		}
	}
	if got["build/vendor.md"] {
		t.Errorf("an ignored file was linted; got %v", files)
	}
}

// writeIn writes body to name under dir, making the directories.
func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
		t.Fatal(err)
	}
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(path, []byte(body), openFile); err != nil {
		t.Fatal(err)
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
	openFile := os.FileMode(0o644)
	page := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(page, []byte(doc), openFile); err != nil {
		t.Fatal(err)
	}
	src := "package x\n\n// f runs.\n// " + strings.ReplaceAll(
		strings.TrimSpace(five),
		"\n",
		"\n// ",
	) +
		"\n//\n//\tone\n//\ttwo\n//\tthree\n//\tfour\n//\tfive\nfunc " +
		"f() {}\n"
	source := filepath.Join(dir, "x.go")
	if err := os.WriteFile(source, []byte(src), openFile); err != nil {
		t.Fatal(err)
	}
	linter, err := lint.Describe([]config.Rule{config.Ruled("paragraph", "4")})
	if err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, finding := range found {
		if finding.Rule() == "paragraph" {
			got[finding.File()]++
		}
	}
	if got["doc.md"] != count.One {
		t.Errorf("doc.md: want only the five-line paragraph, got %d: %v",
			got["doc.md"], found)
	}
	if got["x.go"] != count.One {
		t.Errorf("x.go: want the six-line comment paragraph, "+
			"not the code, got %d: %v", got["x.go"], found)
	}
	if _, _, err := lint.Verdict(found); err == nil {
		t.Error("a long paragraph did not fail")
	}
}

// a file's name and a quoted name are not shouting, nor are the signal
// names; a struct field's tag is never too wide to fix.
// a usage line in a doc comment is code, so its argument names are not
// shouting, while the prose around it still is.
func TestBlocky(t *testing.T) {
	t.Parallel()
	indented := []string{
		"//\tgame bounce [RANGE]",
		"//    set     CONTACT ada@example.com",
		"/*\n *\tgame run NAME\n */",
	}
	for place, got := range shoutedIn(t, indented...) {
		if got != "" {
			t.Errorf("not read as code: %q shouted %q", indented[place], got)
		}
	}
	prose := []string{
		"// the RANGE is a commit or a range of them",
		"//",
		"// set CONTACT to who answers",
	}
	want := []string{"RANGE", "", "CONTACT"}
	for place, got := range shoutedIn(t, prose...) {
		if got != want[place] {
			t.Errorf("read as code: %q shouted %q, want %q", prose[place],
				got, want[place])
		}
	}
}

// what shouts and what does not: a word in capitals is shouting, a file
// name and a backticked name are not, and a struct field's tag is never
// too wide, since the tag is one literal and cannot be split.
func TestShoutedAndTagged(t *testing.T) {
	t.Parallel()
	got := shoutedIn(t, "// see DESIGN.md and `README`, on SIGTERM, NEVER")
	if got[count.First] != "NEVER" {
		t.Errorf("shouted: %v", got)
	}
	width := int(held(t, "lint-width"))
	long := strings.Repeat("x", count.Next(width))
	tag := "`json:\"" + long + "\"`"
	src := "package x\n\ntype T struct {\n" +
		"\tName string " + tag + "\n" +
		"\tA, B int " + tag + " // both\n" +
		"\tMeta struct{ Name, Namespace string } " + tag + "\n" +
		"\tItems []struct{ A int } " + tag + "\n" +
		"}\n\n" +
		"var F = func() { _ = `" + long + "` }\n\n" +
		"var X = `" + long + "`\n"
	span := lintWidth(t)
	linter := described(t, config.Ruled("width", span))
	var wide []int
	for _, one := range goFound(t, linter, src) {
		wide = append(wide, lineOf(one))
	}
	funcLiteral, rawString := onLine(src, "var F = func"), onLine(src, "var X = `")
	if want := []int{funcLiteral, rawString}; !slices.Equal(wide, want) {
		t.Errorf("wide lines %v: a tagged field is never too wide, a raw "+
			"string in code is; want %v", wide, want)
	}
}

// a test file is not linted unless the config asks for it, and a name
// with capitals in it is a name wherever the capitals sit.
func TestTestsAndNames(t *testing.T) {
	t.Parallel()
	linter := described(t, config.Ruled("words", "synergy"), config.Ruled("harden"))
	files := map[string]string{
		"store.go":                 "package s // synergy\n",
		"tester.go":                "package s // synergy\n",
		"protest.md":               "synergy\n",
		"store_test.go":            "package s // synergy\n",
		"clients/go/flipr_test.go": "package s // synergy\n",
		"test_ingest.c":            "void f(void) { setuid(0); }\n",
		"ingest.c":                 "void f(void) { setuid(0); }\n",
	}
	got := map[string]bool{}
	for _, one := range ranOn(t, linter, files) {
		got[one.File()] = true
	}
	for file := range files {
		skipped := strings.HasSuffix(file, "_test.go") ||
			strings.HasPrefix(filepath.Base(file), "test_")
		if got[file] == skipped {
			t.Errorf("%s: skipped %v, but found in it %v", file, skipped,
				got[file])
		}
	}
	names := shoutedIn(t,
		"// see RUNBOOK-restore.md for the page",
		"// DESIGN.md says why",
		"// doc/OPERATION.md, at 03:20",
		"// THE CLIENT ANNOUNCES ITSELF",
	)
	for place, got := range names[:count.Last(len(names))] {
		if got != "" {
			t.Errorf("a file name shouted in line %d: %v", place, got)
		}
	}
	if got := names[count.Last(len(names))]; got != "CLIENT,ANNOUNCES,ITSELF" {
		t.Errorf("real shouting was missed: %v", got)
	}
}

// a readme opens with a picture, and the picture is not prose: the height
// is what is left after the title and one block of art at the top.
func TestHeaderIsArt(t *testing.T) {
	t.Parallel()
	art := []string{"# flipr", "", "    ~~~ dolphin ~~~", "    ~~ ~~", "",
		"## name", "flipr -- the flag store"}
	// the title, a blank, the art and the blank after it: the prose
	// starts at the section
	want := slices.Index(art, "## name")
	if got := headerOf(t, art); got != want {
		t.Errorf("header is %d lines, want %d", got, want)
	}
	fenced := []string{"", "# x", "", "```", "art", "```", "text"}
	closing := slices.Index(fenced, "text")
	if got := headerOf(t, fenced); got != closing {
		t.Errorf("fenced header is %d lines, want %d", got, closing)
	}
	prose := []string{"# x", "", "words about x", "", "    code later"}
	if got := headerOf(t, prose); got != count.Nothing {
		t.Errorf("a document with no art has a header of %d", got)
	}
}

// an import path is one token: it cannot be split, so its width is not a
// finding, while a long string in code still is.
func TestImportedIsNotWide(t *testing.T) {
	t.Parallel()
	width := int(held(t, "lint-width"))
	long := strings.Repeat("x", count.Next(width))
	src := "package x\n\nimport (\n" +
		"\tobservabilityproto \"github.com/" + long + "\"\n" +
		"\t\"github.com/" + long + "\"\n" +
		"\t_ \"github.com/" + long + "\"\n" +
		")\n\nfunc f() string {\n" +
		"\ts := \"" + long + "\"\n" +
		"\t_ = s\n" +
		"\treturn \"" + long + "\"\n}\n"
	span := lintWidth(t)
	linter := described(t, config.Ruled("width", span))
	var wide []int
	for _, one := range goFound(t, linter, src) {
		wide = append(wide, lineOf(one))
	}
	assigned, returned := onLine(src, "s := \""), onLine(src, "return \"")
	if want := []int{assigned, returned}; !slices.Equal(wide, want) {
		t.Errorf("wide lines %v: an import path is one token, a long "+
			"string in code is not; want %v", wide, want)
	}
}

// a line the repository says to leave alone is left alone, and the marker
// may sit on the line or on the line above it.
func TestAllowedHere(t *testing.T) {
	t.Parallel()
	src := "package x\n\nfunc f() {\n" +
		"\tx := 1 // game: allow width\n" +
		"\t// game: allow width\n" +
		"\ty := 2\n" +
		"\tz := 3\n" +
		"\t// game: allow paragraph\n" +
		"\tw := 4\n" +
		"\t_, _, _, _ = x, y, z, w\n}\n"
	linter := described(t, config.Ruled("width", "5"))
	wide := map[int]bool{}
	for _, one := range goFound(t, linter, src) {
		wide[lineOf(one)] = true
	}
	onIt, under := onLine(src, "x := 1"), onLine(src, "y := 2")
	plain, other := onLine(src, "z := 3"), onLine(src, "w := 4")
	if wide[onIt] {
		t.Error("a marker on the line was not honoured")
	}
	if wide[under] {
		t.Error("a marker above the line was not honoured")
	}
	if !wide[plain] {
		t.Error("a line with no marker was allowed")
	}
	if !wide[other] {
		t.Error("a marker for another rule allowed this one")
	}
}

// a readme's header art is exempt from the width as well as the height: a
// picture is not wrapped.
func TestHeaderArtIsNotMeasured(t *testing.T) {
	t.Parallel()
	width := int(held(t, "lint-width"))
	wide := "    " + strings.Repeat("~", count.Next(width))
	src := "# x\n\n" + wide + "\n\n## name\n\nshort prose\n"
	rows := lintRows(t)
	rule := lintWidth(t)
	linter := described(t, config.Ruled("width", rule),
		config.Ruled("rows", rows))
	for _, finding := range docFound(t, linter, "README.md", src) {
		t.Errorf("the header was measured: %v", finding)
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
	openFile := os.FileMode(0o644)
	for _, name := range []string{"OPERATION.md", "README.md"} {
		page := filepath.Join(dir, name)
		if err := os.WriteFile(page, []byte("# a page\n\n"+long),
			openFile); err != nil {
			t.Fatal(err)
		}
	}
	linter, err := lint.Describe([]config.Rule{
		config.Ruled("paragraph", "4"),
		config.Ruled("allow", "OPERATION.md", "paragraph"),
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range found {
		if finding.File() == "OPERATION.md" {
			t.Errorf("the exempt page was still held: %s", finding)
		}
	}
	heldStill := false
	for _, finding := range found {
		heldStill = heldStill || (finding.File() == "README.md" &&
			finding.Rule() == "paragraph")
	}
	if !heldStill {
		t.Error("the page that was not exempt went unreported")
	}
}

// TestAllowRows is the card waived by the name the config uses, rows,
// against the finding the page gets, height.
func TestAllowRows(t *testing.T) {
	dir := t.TempDir()
	rows := int(held(t, "lint-rows"))
	tall := "# a page\n\n" +
		strings.Repeat("a line of prose.\n", count.Next(rows))
	openFile := os.FileMode(0o644)
	page := filepath.Join(dir, "README.md")
	if err := os.WriteFile(page, []byte(tall), openFile); err != nil {
		t.Fatal(err)
	}
	linter, err := lint.Describe([]config.Rule{
		config.Ruled("rows", "25"),
		config.Ruled("allow", "README.md", "rows"),
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range found {
		if finding.Rule() == "height" {
			t.Errorf("the waived page was still measured: %s", finding)
		}
	}
}

// TestVerdictCountsLooks is the regression test for the gate that was not a
// gate. before 2026-09-16 a set of findings that were all looks produced no
// error, so `game lint` printed its tallies and exited nought, and every
// chain, hook and script that used the verb as a gate carried on past real
// findings. two sessions hit it independently on the same evening.
func TestVerdictCountsLooks(t *testing.T) {
	line := count.One
	look := looked(t)
	for _, each := range []struct {
		name  string
		found []lint.Finding
		fix   int
		look  int
		fails bool
	}{
		{"nothing at all", nil, count.Nothing, count.Nothing, false},
		{
			"only a look, and it must still fail",
			[]lint.Finding{look},
			count.Nothing, count.One, true,
		},
		{
			"only a fix",
			[]lint.Finding{lint.Report("a.go", line, "paragraph", "")},
			count.One, count.Nothing, true,
		},
		{
			"both, and the message says both",
			[]lint.Finding{
				lint.Report("a.go", line, "paragraph", ""),
				look,
			},
			count.One, count.One, true,
		},
	} {
		fix, look, err := lint.Verdict(each.found)
		if fix != each.fix || look != each.look {
			t.Errorf("%s: fix %d look %d, want %d and %d",
				each.name, fix, look, each.fix, each.look)
		}
		if (err != nil) != each.fails {
			t.Errorf("%s: err %v, want failure=%v", each.name, err,
				each.fails)
		}
	}
}

// the counts belong in the verdict's message, because a person reads
// it.
func TestVerdictSaysBothCounts(t *testing.T) {
	t.Parallel()
	found := []lint.Finding{
		lint.Report("", count.Nothing, "width", ""),
		looked(t),
	}
	_, _, err := lint.Verdict(found)
	if err == nil ||
		!strings.Contains(err.Error(), "1 to fix, 1 to look at") {
		t.Errorf("message %v, want both counts", err)
	}
}

// noqaSource is a Go file where every kind of finding appears twice,
// once on a line that says noqa and once on a line that does not, one
// column past the width given.
func noqaSource(width int) string {
	return "package lib\n\nimport \"fmt\"\n\n" +
		"// " + strings.Repeat("x", count.Next(width)) + " noqa\n" +
		"// " + strings.Repeat("x", count.Next(width)) + "\n" +
		"// HELLOWORLD said twice. noqa\n" +
		"// HELLOWORLD said twice.\n" +
		"func Say() {\n" +
		"\tfmt.Println(\"x\") // noqa\n" +
		"\tfmt.Println(\"x\")\n" +
		"\t_ = \"blazing" + "-fast\" // noqa\n" +
		"\t_ = \"blazing" + "-fast\"\n" +
		"}\n"
}

// noqaTree is the noqa fixture: the source with every kind of finding
// twice and the page with the mark, in a directory of its own.
func noqaTree(t *testing.T, width int) string {
	t.Helper()
	dir := t.TempDir()
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Join(dir, "lib"), openDir); err != nil {
		t.Fatal(err)
	}
	openFile := os.FileMode(0o644)
	source := filepath.Join(dir, "lib", "lib.go")
	body := noqaSource(width)
	if err := os.WriteFile(source, []byte(body), openFile); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(dir, "README.md")
	if err := os.WriteFile(page, []byte("# x\n\nwow! noqa\n\nwow!\n"),
		openFile); err != nil {
		t.Fatal(err)
	}
	return dir
}

// a line that says noqa, code or comment, is let off every rule; the
// same lines without it are not.
func TestNoqaLetsALineOff(t *testing.T) {
	width := int(held(t, "lint-width"))
	dir := noqaTree(t, width)
	rule := lintWidth(t)
	linter, err := lint.Describe([]config.Rule{
		config.Ruled("width", rule),
		config.Ruled("print"),
		config.Ruled("exclaim"),
		config.Ruled("shout"),
		config.Ruled("words", "blazing"+"-fast"),
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, finding := range found {
		if finding.Rule() != "staticcheck" {
			file, line, rule := finding.File(), lineOf(finding),
				finding.Rule()
			got = append(got, fmt.Sprintf("%s:%d %s", file, line, rule))
		}
	}
	want := []string{
		"README.md:5 exclamation",
		"lib/lib.go:6 width",
		"lib/lib.go:8 shouting",
		"lib/lib.go:11 bare print",
		"lib/lib.go:13 word",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"),
			strings.Join(want, "\n"))
	}
}

// a go file that does not parse is an error from the run, not a file
// with no findings; and noqa on the first line lets that line off.
func TestRunRefusesAFileThatDoesNotParse(t *testing.T) {
	dir := t.TempDir()
	openFile := os.FileMode(0o644)
	bad := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(bad, []byte("package\n"), openFile); err != nil {
		t.Fatal(err)
	}
	linter, err := lint.Describe([]config.Rule{config.Ruled("width", "80")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := linter.Run(dir); err == nil {
		t.Error("a file that does not parse was linted")
	}
	width := int(held(t, "lint-width"))
	wide := strings.Repeat("x", count.Next(width))
	source := []byte("package x // " + wide + " noqa\n")
	if err := os.WriteFile(bad, source, openFile); err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range found {
		if finding.Rule() == "width" {
			t.Errorf("the first line said noqa and was not let off: %v",
				finding)
		}
	}
}

// habitsSource is a Go file with one comment for each habit, one that
// says noqa, one behind a directive with a sample, and one plain.
const habitsSource = `package p

// one hangs on, because it can, so it does.
func one() {}

// two, with, three commas, here.
func two() {}

// A law is stated here.
func three() {}

// this was tried in 2026 and used to work.
func four() {}

// it chains, because it can, so it does.
// plain says plain. noqa
func five() {}

/* a block, with, commas, here. */
func fiveb() {}

// it ran, so it stopped.
func fivec() {}

//go:generate thing
// it chains, because it can, so it does.
//	a, sample, with, commas
func six() {}

// plain says plain.
func seven() {}
`

// each habit is a finding at its comment's first line, with the
// sentence; noqa leaves the whole comment alone; directives and
// samples are not prose; a plain comment has none.
func TestCommentHabits(t *testing.T) {
	dir := t.TempDir()
	openFile := os.FileMode(0o644)
	source := filepath.Join(dir, "p.go")
	if err := os.WriteFile(source, []byte(habitsSource), openFile); err != nil {
		t.Fatal(err)
	}
	linter, err := lint.Describe([]config.Rule{
		config.Ruled("chain"),
		config.Ruled("commas", "3"),
		config.Ruled("opener"),
		config.Ruled("history"),
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, finding := range found {
		rule := finding.Rule()
		if rule != "staticcheck" {
			got = append(got, finding.String())
		}
	}
	want := []string{
		"lint: p.go:3: chain: one hangs on, because it can, so it does.",
		"lint: p.go:6: commas: two, with, three commas, here.",
		"lint: p.go:9: opener: A law is stated here.",
		"lint: p.go:12: history: was tried",
		"lint: p.go:25: chain: it chains, because it can, so it does.",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"),
			strings.Join(want, "\n"))
	}
}

// commas wants a number, and at two finds the sentences with two or
// more.
func TestCommentHabitsCommasAtTwo(t *testing.T) {
	dir := t.TempDir()
	openFile := os.FileMode(0o644)
	source := filepath.Join(dir, "p.go")
	if err := os.WriteFile(source, []byte(habitsSource), openFile); err != nil {
		t.Fatal(err)
	}
	two, err := lint.Describe([]config.Rule{config.Ruled("commas", "2")})
	if err != nil {
		t.Fatal(err)
	}
	found, _ := two.Run(dir)
	if len(found) != int(held(t, "commas-two-findings")) {
		t.Errorf("commas 2 finds %d, want the three sentences with "+
			"two or more", len(found))
	}
	if _, err := lint.Describe([]config.Rule{config.Ruled("commas")}); err == nil {
		t.Error("commas without a number was taken")
	}
}

// a rule left off finds nothing, whatever the others find; commas 1
// finds every sentence with a comma.
func TestCommentHabitsOffAndOn(t *testing.T) {
	dir := t.TempDir()
	openFile := os.FileMode(0o644)
	source := filepath.Join(dir, "p.go")
	if err := os.WriteFile(source, []byte(habitsSource), openFile); err != nil {
		t.Fatal(err)
	}
	if got := only(t, dir, "chain"); !slices.Equal(got,
		[]string{"chain", "chain"}) {
		t.Errorf("chain alone: %v", got)
	}
	if got := only(t, dir, "history"); !slices.Equal(got,
		[]string{"history"}) {
		t.Errorf("history alone: %v", got)
	}
	want := []string{"commas", "commas", "commas", "commas"}
	if got := only(t, dir, "commas", "1"); !slices.Equal(got, want) {
		t.Errorf("commas 1: %v", got)
	}
}

// only is what the one rule finds in the directory, by rule.
func only(t *testing.T, dir, name string, args ...string) []string {
	t.Helper()
	linter, err := lint.Describe([]config.Rule{config.Ruled(name, args...)})
	if err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	var rules []string
	for _, finding := range found {
		if finding.Rule() != "staticcheck" {
			rules = append(rules, finding.Rule())
		}
	}
	return rules
}

// a long sentence is cut to fit a line, at sixty characters.
func TestHabitCutsALongSentence(t *testing.T) {
	dir := t.TempDir()
	fits := int(held(t, "habit-line-fits"))
	dots := "..."
	cut := fits - len(dots)
	frame := len("A " + ".")
	long := "A " + strings.Repeat("x", fits) + "."
	exact := "A " + strings.Repeat("y", fits-frame) + "."
	past := "A " + strings.Repeat("z", count.Next(fits-frame)) + "."
	src := "package p\n\n// " + long + "\nfunc a() {}\n\n// " + exact +
		"\nfunc b() {}\n\n// " + past + "\nfunc c() {}\n"
	openFile := os.FileMode(0o644)
	source := filepath.Join(dir, "p.go")
	if err := os.WriteFile(source, []byte(src), openFile); err != nil {
		t.Fatal(err)
	}
	linter, err := lint.Describe([]config.Rule{config.Ruled("opener")})
	if err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(dir)
	if err != nil || len(found) != count.Next(count.Next(count.One)) {
		t.Fatalf("%v, %v", found, err)
	}
	first, second, third := count.First, count.Next(count.First),
		count.Next(count.Next(count.First))
	if found[first].Text() != long[:cut]+dots ||
		len(found[first].Text()) != fits {
		t.Errorf("long: %q", found[first].Text())
	}
	if found[second].Text() != exact {
		t.Errorf("sixty: %q", found[second].Text())
	}
	if found[third].Text() != past[:cut]+dots {
		t.Errorf("sixty-one: %q", found[third].Text())
	}
}

// a sentence ends at a stop or a question mark followed by space.
func TestSentences(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text string
		want []string
	}{
		{"no, end", []string{"no, end"}},
		{"a, x. b, y? c, z", []string{"a, x.", "b, y?", "c, z"}},
		{". then, x", []string{"then, x"}},
		{"? huh, x", []string{"huh, x"}},
		{"e.g. this, that", []string{"this, that"}},
		{"x, y.  z, w", []string{"x, y.", "z, w"}},
		{"1.5, 2.5 is a number.", []string{"1.5, 2.5 is a number."}},
		{"done, ok. ", []string{"done, ok."}},
		{"one, a.\ttwo, b.", []string{"one, a.", "two, b."}},
	}
	var comments []string
	for _, each := range cases {
		comments = append(comments, "// "+each.text)
	}
	linter := described(t, config.Ruled("commas", "1"))
	for place, found := range chunked(t, linter, comments...) {
		if got := texts(found); !slices.Equal(got, cases[place].want) {
			t.Errorf("sentences(%q) = %q, want %q", cases[place].text, got,
				cases[place].want)
		}
	}
}

// Describe refuses an allow with no lint, an allow for a lint it does
// not know, and words with none.
func TestDescribeRefusesBadLines(t *testing.T) {
	bad := [][]config.Rule{
		{config.Ruled("allow", "README.md")},
		{config.Ruled("allow", "README.md", "nosuch")},
		{config.Ruled("words")},
	}
	for _, rules := range bad {
		if _, err := lint.Describe(rules); err == nil {
			t.Errorf("Describe(%v) took it", rules)
		}
	}
	unknown := bad[count.Next(count.First)]
	_, err := lint.Describe(unknown)
	if err == nil ||
		err.Error() != `lint allow README.md: no lint called "nosuch"` {
		t.Errorf("the unknown lint is not named: %v", err)
	}
	spare := int(held(t, "spare-width"))
	given := strconv.Itoa(spare)
	linter, err := lint.Describe([]config.Rule{config.Ruled("width",
		given)})
	if err != nil {
		t.Fatal(err)
	}
	atSpare := "// " + strings.Repeat("x", spare-len("// "))
	past := atSpare + "x"
	src := "package x\n\n" + atSpare + "\n\n" + past + "\n"
	found := goFound(t, linter, src)
	if len(found) != count.One || lineOf(found[count.First]) != onLine(src, past) {
		t.Errorf("a width of %d: a line of %d is fine and one more is "+
			"not: %v", spare, spare, found)
	}
	allowing, err := lint.Describe([]config.Rule{
		config.Ruled("width", "5"),
		config.Ruled("allow", "a.go", "width", "chain"),
	})
	if err != nil {
		t.Fatal(err)
	}
	where := ranOn(t, allowing, map[string]string{
		"a.go": "package a // wide\n", "b.go": "package b // wide\n"})
	if len(where) != count.One || where[count.First].File() != "b.go" {
		t.Errorf("allow a.go width: %v", where)
	}
}

// a generated file is left alone whole; a print in package main is
// fine; a paragraph rule of one flags a two-line paragraph.
func TestGoFileEdges(t *testing.T) {
	dir := t.TempDir()
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Join(dir, "lib"), openDir); err != nil {
		t.Fatal(err)
	}
	openFile := os.FileMode(0o644)
	made := filepath.Join(dir, "gen.go")
	body := []byte("// Code generated by x. DO NOT EDIT.\n\n" +
		"package main\n\nfunc a() {}\n")
	if err := os.WriteFile(made, body, openFile); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "main.go")
	main := []byte("package main\n\nimport \"fmt\"\n\n// main says.\n" +
		"// and says.\nfunc main() { fmt.Println(1) }\n")
	if err := os.WriteFile(plain, main, openFile); err != nil {
		t.Fatal(err)
	}
	linter, err := lint.Describe([]config.Rule{
		config.Ruled("comments"),
		config.Ruled("print"),
		config.Ruled("paragraph", "1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, finding := range found {
		if finding.Rule() != "staticcheck" {
			got = append(got, finding.String())
		}
	}
	want := []string{"lint: main.go:5: paragraph: 2 lines; a paragraph holds 1"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// a method with type parameters is go 1.27's, and lint reads it like
// any other function: this one has no comment and is found.
func TestGoFileReadsGenericMethods(t *testing.T) {
	t.Parallel()
	body := "package generic\n\n" +
		"type box struct{ n int }\n\n" +
		"func (b box) plus[T ~int](v T) int { return b.n + int(v) }\n"
	linter := described(t, config.Ruled("comments"))
	found := goFound(t, linter, body)
	line := int(held(t, "generic-method-line"))
	if len(found) != count.One ||
		found[count.First].Rule() != "uncommented" ||
		lineOf(found[count.First]) != line {
		t.Errorf("a generic method: %+v", found)
	}
}

// a file lint cannot parse, in a module asking for a newer go than game
// was built with, says so.
func TestRunSaysTheModuleWantsANewerGo(t *testing.T) {
	dir := t.TempDir()
	openFile := os.FileMode(0o644)
	module := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(module, []byte("module x\n\ngo 1.99\n"),
		openFile); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.go")
	if err := os.WriteFile(bad, []byte("package x\n\nfunc (\n"),
		openFile); err != nil {
		t.Fatal(err)
	}
	linter := described(t, config.Ruled("comments"))
	_, err := linter.Run(dir)
	if err == nil || !strings.Contains(err.Error(), "asks for go1.99") {
		t.Errorf("a newer module's parse error: %v", err)
	}
}

// TestAllowWaivesTheCommentsRule: files allowed the comments rule are not
// held to it, and files that are not allowed are. The finding is the same
// for both, so the allowance alone stops it.
func TestAllowWaivesTheCommentsRule(t *testing.T) {
	t.Parallel()
	linter := described(t, config.Ruled("comments"),
		config.Ruled("allow", "x.go", "comments"))
	where := ranOn(t, linter, map[string]string{
		"x.go": "package x\n\nfunc F() {}\n",
		"y.go": "package x\n\nfunc G() {}\n"})
	if len(where) != count.One || where[count.First].File() != "y.go" {
		t.Errorf("allow x.go comments: %v", where)
	}
}
