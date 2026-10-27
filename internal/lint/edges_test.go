package lint_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
)

// put writes text to a file of its own in a new directory and returns
// the path.
func put(t *testing.T, name, text string) string {
	t.Helper()
	dir := t.TempDir()
	writeIn(t, dir, name, text)
	return filepath.Join(dir, name)
}

// goFound is what linter finds in a go file holding text.
func goFound(t *testing.T, linter lint.Lint, text string) []lint.Finding {
	t.Helper()
	return ranOn(t, linter, map[string]string{"x.go": text})
}

// docFound is what linter finds in a markdown file of that name holding
// text.
func docFound(
	t *testing.T,
	linter lint.Lint,
	name, text string,
) []lint.Finding {
	t.Helper()
	return ranOn(t, linter, map[string]string{name: text})
}

func TestVerdictSaysWhatThereIs(t *testing.T) {
	t.Parallel()
	fix := lint.Report("", count.Nothing, "", "")
	look := looked(t)
	for _, each := range []struct {
		found []lint.Finding
		want  string
	}{
		{nil, ""},
		{[]lint.Finding{fix}, "lint: 1 to fix"},
		{[]lint.Finding{look}, "lint: 1 to look at"},
		{[]lint.Finding{fix, look}, "lint: 1 to fix, 1 to look at"},
	} {
		_, _, err := lint.Verdict(each.found)
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != each.want {
			t.Errorf("%d findings: %q, want %q", len(each.found), got,
				each.want)
		}
	}
}

func TestAFindingSaysWhichItIs(t *testing.T) {
	t.Parallel()
	line := count.One
	finding := lint.Report("a.go", line, "width", "90 columns")
	if got := finding.String(); got != "lint: a.go:1: width: 90 columns" {
		t.Errorf("to fix: %q", got)
	}
	if got := looked(t).String(); got != "look: x.go:3: width: 14 columns" {
		t.Errorf("to look at: %q", got)
	}
}

func TestAnyOneRuleIsNotEmpty(t *testing.T) {
	t.Parallel()
	paragraphs := int(held(t, "paragraph-rule"))
	sixParagraphs := strconv.Itoa(paragraphs)
	each := map[string]lint.Lint{
		"width": described(t, config.Ruled("width", "80")),
		"rows":  described(t, config.Ruled("rows", "40")),
		"paragraph": described(t, config.Ruled("paragraph",
			sixParagraphs)),
		"comments": described(t, config.Ruled("comments")),
		"print":    described(t, config.Ruled("print")),
		"exclaim":  described(t, config.Ruled("exclaim")),
		"shout":    described(t, config.Ruled("shout")),
		"words":    described(t, config.Ruled("words", "x")),
		"chain":    described(t, config.Ruled("chain")),
		"commas":   described(t, config.Ruled("commas", "2")),
		"opener":   described(t, config.Ruled("opener")),
		"history":  described(t, config.Ruled("history")),
	}
	for name, linter := range each {
		if linter.Empty() {
			t.Errorf("%s alone is empty", name)
		}
	}
}

// shoutedIn is the words the shout rule finds in each line, as a comment
// of a go file, joined by commas.
func shoutedIn(t *testing.T, lines ...string) []string {
	t.Helper()
	linter := described(t, config.Ruled("shout"))
	found := chunked(t, linter, lines...)
	said := make([]string, len(lines))
	for place, group := range found {
		said[place] = strings.Join(texts(group), ",")
	}
	return said
}

func TestShoutedCountsTheTicksBeforeEachRun(t *testing.T) {
	t.Parallel()
	cases := []struct{ line, want string }{
		{"// `HELLO` is code", ""},
		{"// `a` then `HELLO` quoted", ""},
		{"// `a` `b` then HELLO", "HELLO"},
		{"// see DESIGN.md", ""},
		{"// WORLD and HELLO", "WORLD,HELLO"},
	}
	var lines []string
	for _, each := range cases {
		lines = append(lines, each.line)
	}
	for place, got := range shoutedIn(t, lines...) {
		if got != cases[place].want {
			t.Errorf("shouted(%q) = %q, want %q", cases[place].line, got,
				cases[place].want)
		}
	}
}

// headerOf is how many lines at the top of a readme made of these lines
// are its title and art, as the rows rule leaves them out of the height.
func headerOf(t *testing.T, lines []string) int {
	t.Helper()
	prose := []string{"p one", "p two", "p three"}
	all := append(slices.Clone(lines), prose...)
	linter := described(t, config.Ruled("rows", "1"))
	found := docFound(t, linter, "README.md", strings.Join(all, "\n")+"\n")
	if len(found) != count.One {
		t.Fatalf("a readme of %q: %v", lines, found)
	}
	var tall int
	_, err := fmt.Sscanf(found[count.First].Text(), "%d lines of prose", &tall)
	if err != nil {
		t.Fatal(err)
	}
	return len(all) - tall
}

func TestHeaderAtTheEdges(t *testing.T) {
	t.Parallel()
	for _, each := range []struct {
		lines []string
		want  int
	}{
		{nil, count.Nothing},
		{[]string{"", ""}, count.Nothing},
		{[]string{"# title", ""}, count.Nothing},
		{[]string{"    art"}, count.One},
		{[]string{"```", "```"}, count.Next(count.One)},
		{[]string{"```", "art never closed"}, count.Nothing},
	} {
		if got := headerOf(t, each.lines); got != each.want {
			t.Errorf("header(%q) = %d, want %d", each.lines, got,
				each.want)
		}
	}
}

func TestNamedReadsTheNameAfterTheCapitals(t *testing.T) {
	t.Parallel()
	names := []string{
		"// DESIGN.md", "// DESIGN-x.md", "// DESIGN/x.md",
		"// DESIGN.x.md", "// DESIGN/azAZ09-_x.md", "// DESIGN.go,",
		"// DESIGN-.md",
	}
	for place, got := range shoutedIn(t, names...) {
		if got != "" {
			t.Errorf("%q is a name and shouted %q", names[place], got)
		}
	}
	shouts := []string{
		"// DESIGN", "// DESIGN.", "// DESIGN.Md", "// DESIGN.m1",
		"// DESIGN`x.md", "// DESIGN{x.md", "// DESIGN\x40x.md",
		"// DESIGN[x.md", "// DESIGN:x.md",
	}
	for place, got := range shoutedIn(t, shouts...) {
		if got != "DESIGN" {
			t.Errorf("%q is not a name and shouted %q", shouts[place], got)
		}
	}
}

func TestAllowedHereAtTheEdges(t *testing.T) {
	t.Parallel()
	src := "package x\n\nfunc f() {\n" +
		"\ta := 1 // game: allow width\n" +
		"\t// game: allow width\n" +
		"\tb := 2\n" +
		"\t// game: allow width\n" +
		"\n" +
		"\tc := 3\n" +
		"\td := 4 // game: allow width\n" +
		"\te := 5\n" +
		"\t// game: allow paragraph\n" +
		"\tg := 6\n" +
		"\t_, _, _, _, _, _ = a, b, c, d, e, g\n}\n"
	narrow := described(t, config.Ruled("width", "5"))
	found := goFound(t, narrow, src)
	wide := map[int]bool{}
	for _, one := range found {
		if one.Rule() == "width" {
			wide[lineOf(one)] = true
		}
	}
	for line, allowed := range map[int]bool{
		onLine(src, "a := 1"): true, onLine(src, "b := 2"): true,
		onLine(src, "c := 3"): true, onLine(src, "d := 4"): true,
		onLine(src, "e := 5"): false, onLine(src, "g := 6"): false,
	} {
		if wide[line] == allowed {
			t.Errorf("line %d: allowed %v, but found wide %v", line,
				allowed, wide[line])
		}
	}
}

func TestRunSaysADirectoryThatIsNotThere(t *testing.T) {
	t.Parallel()
	gone := filepath.Join(t.TempDir(), "gone")
	if _, err := (lint.Lint{}).Run(gone); err == nil {
		t.Error("a directory that is not there was linted")
	}
}

func TestRunLeavesTheTestsAlone(t *testing.T) {
	t.Parallel()
	dir := filepath.Dir(put(t, "x_test.go", "package x\n\nfunc f() {}\n"))
	linter := described(t, config.Ruled("comments"))
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range found {
		if finding.Rule() == "uncommented" {
			t.Errorf("a test was linted: %s", finding)
		}
	}
}

// onPath leaves PATH holding only dir, and GOBIN, GOPATH and the go env
// file empty, so a tool is found only where a test puts it.
func onPath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir)
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", t.TempDir())
	t.Setenv("GOENV", "off")
	t.Setenv("GOTOOLCHAIN", "local")
}

// executable writes a shell script that may be run.
func executable(t *testing.T, path, script string) {
	t.Helper()
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
		t.Fatal(err)
	}
	openScript := os.FileMode(0o755)
	body := []byte("#!/bin/sh\n" + script)
	if err := os.WriteFile(path, body, openScript); err != nil {
		t.Fatal(err)
	}
}

// TestStaticcheckKeepsItsFindingLines sets PATH, so it runs alone.
func TestStaticcheckKeepsItsFindingLines(t *testing.T) {
	bin := t.TempDir()
	onPath(t, bin)
	found, err := (lint.Lint{}).Run(t.TempDir())
	if err != nil || len(found) != count.One ||
		!strings.Contains(found[count.First].Text(), "did not run") {
		t.Errorf("no staticcheck: %v, %v", found, err)
	}
	executable(t, filepath.Join(bin, "staticcheck"),
		"echo 'a.go:1:2: something (SA1)'\necho 'a warning'\n")
	found, err = (lint.Lint{}).Run(t.TempDir())
	if err != nil || len(found) != count.One ||
		found[count.First].Text() != "a.go:1:2: something (SA1)" {
		t.Errorf("one finding and a warning: %v, %v", found, err)
	}
}

// goDir is where the go command is, before a test moves PATH.
func goDir(t *testing.T) string {
	t.Helper()
	at, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go on PATH")
	}
	return filepath.Dir(at)
}

// TestToolLooksOnThePathThenGOBINThenGOPATH sets PATH and the go
// environment and moves to a directory of its own, so it runs alone.
func TestToolLooksOnThePathThenGOBINThenGOPATH(t *testing.T) {
	goes := goDir(t)
	onPath(t, goes)
	if got := lint.Tool("nothere"); got != "" {
		t.Errorf("a tool nowhere was found at %q", got)
	}
	gobin, gopath := t.TempDir(), t.TempDir()
	t.Setenv("GOPATH", gopath)
	executable(t, filepath.Join(gopath, "bin", "sc"), "")
	if got := lint.Tool("sc"); got != filepath.Join(gopath, "bin", "sc") {
		t.Errorf("in GOPATH/bin: %q", got)
	}
	t.Setenv("GOBIN", gobin)
	executable(t, filepath.Join(gobin, "sc"), "")
	if got := lint.Tool("sc"); got != filepath.Join(gobin, "sc") {
		t.Errorf("in GOBIN before GOPATH: %q", got)
	}
	onPath(t, gobin+string(os.PathListSeparator)+goes)
	if got := lint.Tool("sc"); got != filepath.Join(gobin, "sc") {
		t.Errorf("on the PATH: %q", got)
	}
	// a directory of the tool's name is not the tool
	onPath(t, goes)
	t.Setenv("GOBIN", gobin)
	t.Setenv("GOPATH", gopath)
	openDir := os.FileMode(0o755)
	if err := os.Mkdir(filepath.Join(gobin, "dir"), openDir); err != nil {
		t.Fatal(err)
	}
	executable(t, filepath.Join(gopath, "bin", "dir"), "")
	if got := lint.Tool("dir"); got != filepath.Join(gopath, "bin", "dir") {
		t.Errorf("past a directory in GOBIN: %q", got)
	}
}

// TestToolStopsAtThePath: with no GOPATH and no HOME, go has no GOBIN
// or GOPATH to give, and the empty answer is not the directory the run
// stands in; with no go to ask, only the PATH is looked on. it sets
// PATH and moves to a directory of its own, so it runs alone.
func TestToolStopsAtThePath(t *testing.T) {
	goes := goDir(t)
	here := t.TempDir()
	t.Chdir(here)
	executable(t, filepath.Join(here, "local"), "")
	onPath(t, goes)
	t.Setenv("GOPATH", "")
	t.Setenv("HOME", "")
	if got := lint.Tool("local"); got != "" {
		t.Errorf("with go giving no directory, found %q", got)
	}
	gobin := t.TempDir()
	executable(t, filepath.Join(gobin, "sc"), "")
	onPath(t, goes)
	t.Setenv("GOBIN", gobin)
	if got := lint.Tool("sc"); got != filepath.Join(gobin, "sc") {
		t.Errorf("in GOBIN with no GOPATH: %q", got)
	}
	onPath(t, t.TempDir())
	if got := lint.Tool("sc"); got != "" {
		t.Errorf("with no go, found %q", got)
	}
}

func TestKeptSortsByFileThenLineAndKeepsTies(t *testing.T) {
	t.Parallel()
	linter := described(t, config.Ruled("words", "synergy"), config.Ruled("exclaim"))
	found := ranOn(t, linter, map[string]string{
		"b.md": "synergy\n",
		"a.md": "synergy\nsynergy wow!\n",
	})
	var got []string
	for _, one := range found {
		said := fmt.Sprintf("%s:%d %s", one.File(), lineOf(one), one.Rule())
		got = append(got, said)
	}
	want := []string{"a.md:1 word", "a.md:2 word", "a.md:2 exclamation", "b.md:1 word"}
	if !slices.Equal(got, want) {
		t.Errorf("kept in the order %q, want %q", got, want)
	}
}

func TestUncommentedIsARule(t *testing.T) {
	t.Parallel()
	source := "package x\n\nfunc a() {}\n\n//\nfunc b() {}\n"
	if found := goFound(t, lint.Lint{}, source); count.Positive(
		len(found)) {
		t.Errorf("with the rule off: %v", found)
	}
	linter := described(t, config.Ruled("comments"))
	found := goFound(t, linter, source)
	second := count.Next(count.First)
	if len(found) != count.Next(count.One) ||
		found[second].Text() != "b has no comment above it" {
		t.Errorf("an empty comment is no comment: %v", found)
	}
}

func TestPrintsAreFmtsOnly(t *testing.T) {
	t.Parallel()
	linter := described(t, config.Ruled("print"))
	found := goFound(t, linter, "package x\n\n"+
		"import (\n\t\"fmt\"\n\t\"log\"\n)\n\n"+
		"func f() {\n\tg()\n\tlog.Println()\n\ts.w.Printf()\n"+
		"\tfmt.Print()\n}\n")
	if len(found) != count.One ||
		!strings.HasPrefix(found[count.First].Text(), "fmt.Print ") {
		t.Errorf("found %v, want only fmt.Print", found)
	}
}

func TestTheWidthIsPastTheRule(t *testing.T) {
	t.Parallel()
	lineWidth := len("// x")
	for _, each := range []struct {
		width int
		want  bool
	}{
		{count.One, true},
		{count.Last(lineWidth), true},
		{lineWidth, false},
	} {
		rule := strconv.Itoa(each.width)
		linter := described(t, config.Ruled("width", rule))
		wide := false
		for _, finding := range goFound(t, linter, "package x\n\n// x\n") {
			third := count.Next(count.Next(count.One))
			wide = wide || lineOf(finding) == third &&
				finding.Rule() == "width"
		}
		if wide != each.want {
			t.Errorf("a comment of 4 at %d: %v", each.width, wide)
		}
		doc := docFound(t, linter, "a.md", "abcd\n")
		if got := rulesOf(doc) == "width"; got != each.want {
			t.Errorf("a line of 4 at %d: %v", each.width, got)
		}
	}
}

func TestAnIndentedCommentDoesNotShout(t *testing.T) {
	t.Parallel()
	linter := described(t, config.Ruled("shout"))
	found := goFound(t, linter, "package x\n\n// see:\n//\tHELLOWORLD\n")
	if count.Positive(len(found)) {
		t.Errorf("an indented comment shouted: %v", found)
	}
}

func TestADocThatIsNotThere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	writeIn(t, dir, "gone.md", "text\n")
	gitIn(t, dir, "add", "gone.md")
	if err := os.Remove(filepath.Join(dir, "gone.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := (lint.Lint{}).Run(dir); err == nil {
		t.Error("a doc that is not there was read")
	}
}

func TestADocsFirstLineAfterItsArtIsRead(t *testing.T) {
	t.Parallel()
	linter := described(t, config.Ruled("shout"))
	found := docFound(t, linter, "a.md", "    art\nHELLOWORLD here\n")
	second := count.Next(count.One)
	if len(found) != count.One || lineOf(found[count.First]) != second {
		t.Errorf("found %v, want the shout on line 2", found)
	}
	paragraph := described(t, config.Ruled("paragraph", "1"))
	found = docFound(t, paragraph, "b.md", "one\ntwo\n")
	if rulesOf(found) != "paragraph" {
		t.Errorf("a paragraph rule of one: %v", found)
	}
}

func TestAReadmesHeightLeavesOutItsArt(t *testing.T) {
	t.Parallel()
	art := "    one\n    two\n    three\n\n"
	height := int(held(t, "height-rule"))
	rows := strconv.Itoa(height)
	for prose, want := range map[string]string{
		"a\nb\n":          "",
		"a\nb\nc\n":       "",
		"a\nb\nc\nd\ne\n": "height",
	} {
		linter := described(t, config.Ruled("rows", rows))
		found := docFound(t, linter, "README.md", art+prose)
		if got := rulesOf(found); got != want {
			t.Errorf("%q under the art: %q, want %q", prose, got, want)
		}
	}
}

func TestADocsWordsAndShouts(t *testing.T) {
	t.Parallel()
	words, err := lint.Describe([]config.Rule{config.Ruled("words",
		"synergy")})
	if err != nil {
		t.Fatal(err)
	}
	if got := rulesOf(docFound(t, words, "a.md", "pure synergy\n")); got != "word" {
		t.Errorf("a banned word: %q", got)
	}
	shout := described(t, config.Ruled("shout"))
	for text, want := range map[string]string{
		"a\nHELLOWORLD\n":     "shouting",
		"a\n    HELLOWORLD\n": "",
		"a\n\tHELLOWORLD\n":   "",
	} {
		if got := rulesOf(docFound(t, shout, "a.md", text)); got != want {
			t.Errorf("shout on %q: %q, want %q", text, got, want)
		}
	}
	found := docFound(t, lint.Lint{}, "a.md", "a\nHELLOWORLD\n")
	if got := rulesOf(found); got != "" {
		t.Errorf("with the rule off: %q", got)
	}
}

func TestDocParagraphsAtTheEdges(t *testing.T) {
	t.Parallel()
	for _, each := range []struct {
		text string
		rule int
		want int
	}{
		{"one line\n", count.One, count.Nothing},
		{"a\n    b\nc\n", count.Next(count.One), count.One},
		{"    code\n    code\n    code\n", count.Next(count.One),
			count.Nothing},
	} {
		rule := strconv.Itoa(each.rule)
		linter := described(t, config.Ruled("paragraph", rule))
		found := docFound(t, linter, "a.md", each.text)
		if len(found) != each.want {
			t.Errorf("%q at %d: %v", each.text, each.rule, found)
		}
	}
}

// paragraphsOf is what a rule of two lines finds in the comments of a go
// file holding text.
func paragraphsOf(t *testing.T, text string) []lint.Finding {
	t.Helper()
	linter := described(t, config.Ruled("paragraph", "2"))
	return goFound(t, linter, text)
}

func TestCommentParagraphsAtTheEdges(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]int{
		"// a\n// b\n":                     count.Nothing,
		"// a\n// b\n//\n// c\n// d\n":     count.Nothing,
		"// a\n// b\n// c\n":               count.One,
		"// a\n/* b */\n// c\n":            count.Nothing,
		"// a\n// b\n// - c\n// - d\n":     count.Nothing,
		"// a\n// b\n//\n// c\n// d\n// e": count.One,
	} {
		found := paragraphsOf(t, "package x\n\n"+text+"\nvar X int\n")
		if len(found) != want {
			t.Errorf("%q: %v, want %d", text, found, want)
		}
	}
}

// gitIn runs git in dir, failing the test if it fails.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestTrackedLeavesOutTestdata(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	for _, rel := range []string{"a.md", "testdata/x.md",
		"sub/testdata/y.md"} {
		writeIn(t, dir, rel, "synergy\n")
	}
	got := filesFound(t, dir)
	if strings.Join(got, ",") != "a.md" {
		t.Errorf("tracked %q; want a.md", got)
	}
}

func TestWalkedLeavesOutWhatARepoWould(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, rel := range []string{".hidden/a.md", "bin/a.md", "build/a.md",
		"testdata/a.md", "vendor/a.md", "sub/bin/a.md", "keep.md"} {
		writeIn(t, dir, rel, "synergy\n")
	}
	got := filesFound(t, dir)
	if strings.Join(got, ",") != "keep.md" {
		t.Errorf("walked %q; want keep.md", got)
	}
}

// filesFound is the files of dir the lint reads, those a banned word in
// each of them is found in, by name.
func filesFound(t *testing.T, dir string) []string {
	t.Helper()
	linter := described(t, config.Ruled("words", "synergy"))
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, one := range found {
		if one.Rule() == "word" {
			files = append(files, one.File())
		}
	}
	return files
}

// an allow comment on the very first line of a file allows the line
// under it, and a first line that allows nothing allows nothing.
func TestAllowedHereReadsTheFirstLineOfAFile(t *testing.T) {
	t.Parallel()
	narrow := described(t, config.Ruled("width", "5"))
	allowed := "// game: allow width\npackage x\n"
	for _, one := range goFound(t, narrow, allowed) {
		if one.Rule() == "width" {
			t.Errorf("the first line allows width, found %v", one)
		}
	}
	silent := "// game: allow paragraph\npackage x\n"
	var wide bool
	for _, one := range goFound(t, narrow, silent) {
		wide = wide || one.Rule() == "width"
	}
	if !wide {
		t.Error("a first line allowing another rule allowed width")
	}
}
