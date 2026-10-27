// Package lint runs the lint rules written in your config or the repository's
// .game. game has no opinions of its own.
//
// rules can ask for a comment above every function, no bare print outside
// package main, banned words kept out, and no exclamation marks in docs.
//
// prose, docs and comments have a width. wider code is reported to look at.
// a readme has a height. shouty capitals in comments are reported to look at.
//
// go vet and gofmt belong to check. staticcheck runs here, and a missing
// staticcheck is a finding.
package lint

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/toolchain"
)

// Finding is one thing to fix, or one thing to look at.
type Finding struct {
	file string
	line int
	rule string
	text string
	look bool // seen rather than named, and it still fails
}

// File is the file the finding is in.
func (found Finding) File() string { return found.file }

// Rule is the rule the finding breaks.
func (found Finding) Rule() string { return found.rule }

// Text is what the finding says.
func (found Finding) Text() string { return found.text }

// Look is whether the finding is to look at rather than to fix: seen
// rather than named, and it still fails.
func (found Finding) Look() bool { return found.look }

// looked is one finding to look at, at a line of a file.
func looked(file string, line int, rule, text string) Finding {
	return Finding{file: file, line: line, rule: rule, text: text,
		look: true}
}

// Report is one finding to fix, at a line of a file, or against the
// file alone when the line is nothing: a binary, a tool or a build
// setting has no line.
func Report(file string, line int, rule, text string) Finding {
	return Finding{file: file, line: line, rule: rule, text: text}
}

// Harden is whether the harden rule is on, which release and verify
// read to know whether to go beyond the source.
func (rules Lint) Harden() bool { return rules.harden }

// Verdict counts the fix and look findings, and errors if there are any.
func Verdict(found []Finding) (fix, look int, err error) {
	for _, finding := range found {
		if finding.look {
			look++
		} else {
			fix++
		}
	}
	switch {
	case count.Positive(fix) && count.Positive(look):
		err = fmt.Errorf("lint: %d to fix, %d to look at", fix, look)
	case count.Positive(fix):
		err = fmt.Errorf("lint: %d to fix", fix)
	case count.Positive(look):
		err = fmt.Errorf("lint: %d to look at", look)
	}
	return fix, look, err
}

// String is the finding as one line, marked lint or look.
func (found Finding) String() string {
	mark := "lint"
	if found.look {
		mark = "look"
	}
	return fmt.Sprintf(
		"%s: %s:%d: %s: %s",
		mark,
		found.file,
		found.line,
		found.rule,
		found.text,
	)
}

// Lint is the rules as described, ready to run.
type Lint struct {
	width     int // nothing is no rule
	rows      int
	paragraph int // the most lines a paragraph of prose may run to
	comments  bool
	print     bool
	exclaim   bool
	shout     bool
	tests     bool // lint the tests too; by default they are left alone
	harden    bool // report privilege and unsafe file handling
	// the four habits of a comment that is not plain. commas is how many
	// in one sentence are too many; nothing is no rule.
	chain   bool
	commas  int
	opener  bool
	history bool
	words   *regexp.Regexp
	allow   map[string][]string // file to the rules it is let off
}

// Describe turns rule lines into a Lint; a bad argument is an error.
func Describe(lines []config.Rule) (Lint, error) {
	var rules Lint
	for _, rule := range lines {
		if err := rules.describe(rule); err != nil {
			return rules, err
		}
	}
	return rules, nil
}

// turned is the rules that are on or off, by name: where each is kept.
// a name that is no such rule is nil.
func (rules *Lint) turned(name string) *bool {
	switches := map[string]*bool{
		"chain":    &rules.chain,
		"opener":   &rules.opener,
		"history":  &rules.history,
		"comments": &rules.comments,
		"print":    &rules.print,
		"exclaim":  &rules.exclaim,
		"tests":    &rules.tests,
		"harden":   &rules.harden,
		"shout":    &rules.shout,
	}
	return switches[name]
}

// describe reads one rule into the lint.
func (rules *Lint) describe(rule config.Rule) error {
	name, args := rule.Name(), rule.Args()
	if on := rules.turned(name); on != nil {
		*on = true
		return nil
	}
	switch name {
	case "width", "rows", "paragraph", "commas":
		return rules.numbered(name, args)
	case "allow":
		return rules.allowing(args)
	case "words":
		return rules.banning(args)
	}
	return fmt.Errorf("unknown lint %q", name)
}

// numbered reads a rule that takes one number.
func (rules *Lint) numbered(name string, args []string) error {
	if len(args) != count.One {
		return fmt.Errorf("lint %s wants one number", name)
	}
	said := args[count.First]
	number, err := strconv.Atoi(said)
	if err != nil || !count.Positive(number) ||
		strings.TrimLeft(said, "0123456789") != "" {
		return fmt.Errorf("lint %s wants a number, not %q", name, said)
	}
	switch name {
	case "width":
		rules.width = number
	case "rows":
		rules.rows = number
	case "commas":
		rules.commas = number
	default:
		rules.paragraph = number
	}
	return nil
}

// fileAndLint is the fewest arguments an allow takes: the file and one
// lint, as the line is defined.
const fileAndLint = 2

// allowing reads an allow: the file, then the lints it is let off.
func (rules *Lint) allowing(args []string) error {
	if len(args) < fileAndLint {
		return fmt.Errorf("lint allow wants a file and a lint")
	}
	if rules.allow == nil {
		rules.allow = map[string][]string{}
	}
	file, lints := args[count.First], args[count.Next(count.First):]
	for _, name := range lints {
		if to, ok := allowSaid[name]; ok {
			name = to
		}
		if !allowKnown[name] {
			return fmt.Errorf("lint allow %s: no lint called %q",
				file, name)
		}
		rules.allow[file] = append(rules.allow[file], name)
	}
	return nil
}

// banning reads the words banned from code and docs.
func (rules *Lint) banning(args []string) error {
	if count.None(len(args)) {
		return fmt.Errorf("lint words wants at least one word")
	}
	quoted := make([]string, count.Nothing, len(args))
	for _, word := range args {
		quoted = append(quoted, regexp.QuoteMeta(word))
	}
	rules.words = regexp.MustCompile(
		`(?i)\b(` + strings.Join(quoted, "|") + `)\b`,
	)
	return nil
}

// testy returns true if the path is a test file: go's, or a C program's
// named test_. Run skips test files unless Tests is set. a file of any
// other language is no kind lint reads, so its tests are not named here.
func testy(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") ||
		strings.HasPrefix(base, "test_")
}

// Empty is whether nothing was described. One comment rule alone is a
// config. That is chain or commas or opener or history.
func (rules Lint) Empty() bool {
	return count.None(rules.width) && count.None(rules.rows) &&
		count.None(rules.paragraph) &&
		!rules.comments && !rules.print && !rules.exclaim &&
		!rules.shout && !rules.asksHabit() &&
		!rules.harden && rules.words == nil
}

// shout matches a run of five or more capitals.
var shout = regexp.MustCompile(`\b[A-Z]{5,}\b`)

// a match's span as regexp gives it: where it starts and where it ends.
const (
	spanStart = 0 // defined by regexp: the first of the pair
	spanEnd   = 1 // defined by regexp: the second of the pair
)

// shouted returns the runs of capitals in a line that are shouting. it skips
// allowed words, file names and anything inside backticks.
func shouted(line string) []string {
	var out []string
	var ticks, last int
	for _, span := range shout.FindAllStringIndex(line, every) {
		start, end := span[spanStart], span[spanEnd]
		ticks += strings.Count(line[last:start], "`")
		last = start
		word := line[start:end]
		if !allowed[word] && !betweenTicks(ticks) && !named(line, end) {
			out = append(out, word)
		}
	}
	return out
}

// betweenTicks is whether a place after that many backticks is inside a
// pair of them: an odd count opened one that has not closed.
func betweenTicks(ticks int) bool {
	const pair = 2 // defined: backticks come in pairs
	return ticks%pair == count.One
}

// header is how many lines at the top of a document are its title and art.
// the art is one fenced or indented block at the top.
func header(lines []string) int {
	i := count.First
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i < len(lines) && strings.HasPrefix(lines[i], "# ") {
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) {
		return count.Nothing
	}
	fenced := strings.HasPrefix(strings.TrimSpace(lines[i]), "```")
	indented := strings.HasPrefix(lines[i], "    ") ||
		strings.HasPrefix(lines[i], "\t")
	if !fenced && !indented {
		return count.Nothing
	}
	if fenced {
		for after := count.Next(i); after < len(lines); after++ {
			fence := strings.TrimSpace(lines[after])
			if strings.HasPrefix(fence, "```") {
				return count.Next(after)
			}
		}
		return count.Nothing
	}
	after := i
	for after < len(lines) {
		line := lines[after]
		if strings.TrimSpace(line) == "" ||
			strings.HasPrefix(line, "    ") ||
			strings.HasPrefix(line, "\t") {
			after++
			continue
		}
		break
	}
	return after
}

// named is whether the capitals ending at to sit in a file's name, as
// DESIGN.md does: the letters, digits, dots, hyphens, underscores and
// slashes after them end in a dot and a lowercase extension. what comes
// before the capitals cannot change that, so it is not read.
func named(line string, to int) bool {
	end := to
	for end < len(line) && partOfName(line[end]) {
		end++
	}
	rest := line[to:end]
	dot := strings.LastIndexByte(rest, '.')
	if dot == notFound {
		return false
	}
	extension := rest[count.Next(dot):]
	return extension != "" &&
		strings.Trim(extension, "abcdefghijklmnopqrstuvwxyz") == ""
}

// notFound is what strings gives for a place it did not find, as it
// has defined it.
const notFound = -1

// partOfName is whether a byte may be part of a file's name: a letter,
// a digit, a dot, a hyphen, an underscore or a slash.
func partOfName(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' ||
		char == '.' || char == '-' || char == '_' || char == '/'
}

// importLine matches an import line. optional alias, dot, or underscore
// before the path.
var importLine = regexp.MustCompile(
	`^\s*(?P<alias>[A-Za-z_]\w*|\.|_)?\s*"[^"]+"\s*(//.*)?$`)

// keywords that can start a statement, not an import.
var notAnAlias = map[string]bool{
	"return": true, "case": true, "panic": true, "go": true, "defer": true,
	"chan": true, "range": true, "break": true, "continue": true,
}

// imported checks if a line is an import. it is not when a keyword such as
// return stands where the alias would.
func imported(line string) bool {
	match := importLine.FindStringSubmatch(line)
	if match == nil {
		return false
	}
	return !notAnAlias[match[importLine.SubexpIndex("alias")]]
}

// allowedHere is whether line i, or the comment line above it, says "game:
// allow RULE". a bare "game: allow" allows every rule.
func allowedHere(lines []string, i int, rule string) bool {
	if saysAllow(lines[i], rule) {
		return true
	}
	for above := count.Last(i); above >= count.First; above-- {
		text := strings.TrimSpace(lines[above])
		if text == "" {
			continue
		}
		comment := strings.HasPrefix(text, "//") ||
			strings.HasPrefix(text, "#")
		if !comment {
			return false
		}
		return saysAllow(text, rule)
	}
	return false
}

// saysAllow is whether a line says "game: allow", bare or naming the
// rule.
func saysAllow(text, rule string) bool {
	_, after, said := strings.Cut(text, "game: allow")
	if !said {
		return false
	}
	rest := strings.TrimSpace(after)
	return rest == "" || strings.HasPrefix(rest, rule)
}

// tagged matches a struct field with a tag. the tag is one backtick literal.
// the type is a plain name or a short struct on the same line.
var tagged = regexp.MustCompile(
	`^\s*[A-Za-z_]\w*(\s*,\s*[A-Za-z_]\w*)*\s+` +
		`([][*.\w]*struct\{[^{}` + "`" + `]*\}|[][*.\w]+(\{\})?)` +
		`\s+` + "`[^`]*`" + `\s*(//.*)?$`)

// allowed lists acronyms and RFC 2119 terms that may be in capitals.
var allowed = map[string]bool{
	"ASCII":   true,
	"ANSI":    true,
	"HTTPS":   true,
	"OKLAB":   true,
	"OKLCH":   true,
	"JSON":    true,
	"UTF8":    true,
	"NOLINT":  true,
	"NOCOLOR": true,
	"PANIC":   true,
	"NOTE":    true,
	"TODO":    true,
	"JSONL":   true,
	"SIGINT":  true,
	"SIGTERM": true,
	"SIGKILL": true, "SIGHUP": true, "SIGQUIT": true,
	"MUST": true, "SHALL": true, "SHOULD": true, "REQUIRED": true,
	"RECOMMENDED": true, "OPTIONAL": true,
}

// Run lints the go and markdown files git tracks, or would track, under
// dir, or every one when dir is no repository. it adds staticcheck, drops
// what the allowances let off and sorts by file and line.
func (rules Lint) Run(dir string) ([]Finding, error) {
	files, err := tracked(dir)
	if err != nil {
		files, err = walked(dir)
		if err != nil {
			return nil, err
		}
	}
	var out []Finding
	for _, rel := range files {
		if testy(rel) && !rules.tests {
			continue
		}
		found, err := rules.file(filepath.Join(dir, rel), rel)
		if err != nil {
			built := runtime.Version()
			return nil, toolchain.Explain(dir, built, err)
		}
		out = append(out, found...)
	}
	out = append(out, staticcheck(dir)...)
	return rules.kept(out), nil
}

// file lints one file by its kind, less the findings on lines that say
// noqa; a file of no kind has no findings. C, Objective-C and Swift are
// of a kind to the harden rule alone.
func (rules Lint) file(path, rel string) ([]Finding, error) {
	switch {
	case strings.HasSuffix(rel, ".go"), strings.HasSuffix(rel, ".md"):
	case rules.harden && nativeSource(rel):
		return hardenSource(path, rel)
	default:
		return nil, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(rel, ".go") {
		return exempt(src, rules.doc(path, rel, src)), nil
	}
	found, err := rules.goFile(path, rel, src)
	if err != nil {
		return nil, err
	}
	return exempt(src, found), nil
}

// noqa matches a line that asks to be left alone, by that word.
var noqa = regexp.MustCompile(`\bnoqa\b`)

// exempt is the findings less those on a line of the source that says
// noqa, code or comment, whatever the rule. every finding is on a line
// of the source it was made from.
func exempt(src []byte, found []Finding) []Finding {
	lines := strings.Split(string(src), "\n")
	var out []Finding
	for _, one := range found {
		at := one.line - count.Origin
		if noqa.MatchString(lines[at]) {
			continue
		}
		out = append(out, one)
	}
	return out
}

// staticcheck runs staticcheck over the tree and keeps the finding lines.
// a missing staticcheck is itself a finding.
func staticcheck(dir string) []Finding {
	sc := Tool("staticcheck")
	if sc == "" {
		return []Finding{{
			file: "staticcheck",
			rule: "staticcheck",
			text: "staticcheck did not run and its findings are " +
				"unknown; go install honnef.co/go/tools/cmd/" +
				"staticcheck@latest",
		}}
	}
	cmd := exec.Command(sc, "./...")
	cmd.Dir = dir
	said, _ := cmd.CombinedOutput()
	var out []Finding
	body := strings.TrimSpace(string(said))
	for _, line := range strings.Split(body, "\n") {
		if finding.MatchString(line) {
			out = append(out, Finding{
				file: "staticcheck",
				rule: "staticcheck",
				text: line,
			})
		}
	}
	return out
}

// kept is the findings after removing allowed ones, sorted by file and line.
func (rules Lint) kept(found []Finding) []Finding {
	out := make([]Finding, count.Nothing, len(found))
	for _, one := range found {
		if rules.allowed(one.file, one.rule) {
			continue
		}
		out = append(out, one)
	}
	slices.SortStableFunc(out, byPlace)
	return out
}

// byPlace orders findings by file, then line, as slices.SortStableFunc
// asks.
func byPlace(one, other Finding) int {
	const same = 0 // defined by strings.Compare: equal strings
	if by := strings.Compare(one.file, other.file); by != same {
		return by
	}
	return cmp.Compare(one.line, other.line)
}

// allowSaid maps the word a config uses for a rule to the word a finding
// uses. an allowance with no entry here matches nothing.
var allowSaid = map[string]string{
	"rows":     "height",
	"shout":    "shouting",
	"comments": "uncommented",
	"words":    "word",
	"exclaim":  "exclamation",
}

// allowKnown is every rule a finding can carry. any other is refused when
// the config is read.
var allowKnown = map[string]bool{
	"width": true, "height": true, "paragraph": true,
	"shouting": true, "uncommented": true, "word": true,
	"print": true, "exclamation": true, "tests": true,
	"chain": true, "commas": true, "opener": true, "history": true,
	"harden": true,
}

// finding matches staticcheck's format. anything else is a warning.
var finding = regexp.MustCompile(`^[^ ]+:[0-9]+:[0-9]+: `)

// Tool returns the path of a tool, or an empty string if none is
// found. it looks on the path, then where go env says binaries go and
// in the bin under go's own path, so a go install has the same say as
// a brew install. game's harden pass finds its analyzers the same way.
func Tool(name string) string {
	if found, err := exec.LookPath(name); err == nil {
		return found
	}
	for _, setting := range []string{"GOBIN", "GOPATH"} {
		out, err := exec.Command("go", "env", setting).Output()
		if err != nil {
			continue
		}
		dir := strings.TrimSpace(string(out))
		if dir == "" {
			continue
		}
		if setting == "GOPATH" {
			dir = filepath.Join(dir, "bin")
		}
		at := filepath.Join(dir, name)
		if fi, err := os.Stat(at); err == nil && !fi.IsDir() {
			return at
		}
	}
	return ""
}

// allowed checks if a file is exempt from a rule.
func (rules Lint) allowed(file, rule string) bool {
	for _, name := range rules.allow[file] {
		if name == rule {
			return true
		}
	}
	return false
}

// goFile lints one go file for an uncommented function, a print outside
// main, the banned words, the width, long comment paragraphs and
// shouting. a generated file is left alone.
func (rules Lint) goFile(
	path, rel string, src []byte,
) ([]Finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	for _, group := range file.Comments {
		if strings.HasPrefix(group.Text(), "Code generated") {
			return nil, nil
		}
	}
	out := rules.uncommented(fset, file, rel)
	if rules.print && file.Name.Name != "main" {
		out = append(out, rules.prints(fset, file, rel)...)
	}
	lines := strings.Split(string(src), "\n")
	out = append(out, rules.goLines(lines, rel)...)
	if count.Positive(rules.paragraph) {
		for _, group := range file.Comments {
			more := rules.commentParagraphs(fset, group, rel)
			out = append(out, more...)
		}
	}
	if rules.shout {
		out = append(out, rules.shouts(fset, file, rel)...)
	}
	if rules.harden {
		out = append(out, harden(fset, file, rel)...)
	}
	out = append(out, rules.habits(fset, file, rel)...)
	return out, nil
}

// uncommented finds functions without a comment above them.
func (rules Lint) uncommented(
	fset *token.FileSet,
	file *ast.File,
	rel string,
) []Finding {
	if !rules.comments {
		return nil
	}
	var out []Finding
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Doc == nil || strings.TrimSpace(fn.Doc.Text()) == "" {
			at := fset.Position(fn.Pos()).Line
			out = append(out, Report(rel, at, "uncommented",
				fn.Name.Name+" has no comment above it"))
		}
	}
	return out
}

// prints is every fmt.Print outside package main. a library returns and
// a daemon logs.
func (rules Lint) prints(
	fset *token.FileSet,
	file *ast.File,
	rel string,
) []Finding {
	printing := &printing{fset: fset, rel: rel}
	ast.Inspect(file, printing.visit)
	return printing.out
}

// printing is a walk of one file for fmt.Print calls, and what it
// found.
type printing struct {
	fset *token.FileSet
	rel  string
	out  []Finding
}

// visit reads one node: a call of fmt.Print or its kin is a finding.
func (walk *printing) visit(node ast.Node) bool {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return true
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return true
	}
	pkg, ok := sel.X.(*ast.Ident)
	if ok && pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Print") {
		why := "fmt." + sel.Sel.Name +
			" outside main: a library returns " +
			"and a daemon logs"
		at := walk.fset.Position(call.Pos()).Line
		found := Report(walk.rel, at, "bare print", why)
		walk.out = append(walk.out, found)
	}
	return true
}

// goLines is the banned words and the width, line by line. wide code is a
// look and a wide comment is to fix. struct tags, import paths and lines an
// allow comment covers are let by.
func (rules Lint) goLines(lines []string, rel string) []Finding {
	var out []Finding
	for i, line := range lines {
		if rules.words != nil {
			if word := rules.words.FindString(line); word != "" {
				at := count.Next(i)
				out = append(out, Report(rel, at, "word", word))
			}
		}
		wide := width(line)
		if rules.tooWide(wide) &&
			!tagged.MatchString(line) &&
			!imported(line) &&
			!allowedHere(lines, i, "width") {
			trimmed := strings.TrimSpace(line)
			columns := fmt.Sprintf("%d columns", wide)
			at := count.Next(i)
			found := looked(rel, at, "width", columns)
			if strings.HasPrefix(trimmed, "//") {
				found = Report(rel, at, "width", columns)
			}
			out = append(out, found)
		}
	}
	return out
}

// tooWide is whether a line's width breaks the rule, when there is one.
func (rules Lint) tooWide(wide int) bool {
	return count.Positive(rules.width) && wide > rules.width
}

// shouts is every run of capitals in a comment, as a look. indented lines
// are left alone.
func (rules Lint) shouts(
	fset *token.FileSet,
	file *ast.File,
	rel string,
) []Finding {
	var out []Finding
	for _, group := range file.Comments {
		for _, comment := range group.List {
			at := fset.Position(comment.Pos()).Line
			for _, line := range strings.Split(comment.Text, "\n") {
				if blocky(line) {
					continue
				}
				for _, word := range shouted(line) {
					out = append(out, Finding{
						rel, at, "shouting", word, true,
					})
				}
			}
		}
	}
	return out
}

// blocky checks if a comment line is indented, which go treats as code,
// like a markdown fence.
func blocky(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, mark := range []string{"//", "/*", "*"} {
		if strings.HasPrefix(trimmed, mark) {
			trimmed = strings.TrimPrefix(trimmed, mark)
			break
		}
	}
	return strings.HasPrefix(trimmed, "\t") ||
		strings.HasPrefix(trimmed, "    ")
}

// doc lints one markdown file for height, paragraphs, banned words,
// exclamations, width and shouting. code blocks and the header's art are
// left alone.
func (rules Lint) doc(path, rel string, src []byte) []Finding {
	body := strings.TrimRight(string(src), "\n")
	lines := strings.Split(body, "\n")
	out := rules.height(path, rel, lines)
	if count.Positive(rules.paragraph) {
		out = append(out, rules.docParagraphs(lines, rel)...)
	}
	code := false
	art := header(lines)
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			code = !code
			continue
		}
		// the header is a picture, not wrapped, so it is skipped.
		if code || i < art {
			continue
		}
		at := count.Next(i)
		out = append(out, rules.docLine(rel, at, line)...)
	}
	return out
}

// height is a readme taller than the rule, its header not counted.
func (rules Lint) height(path, rel string, lines []string) []Finding {
	readme := strings.EqualFold(filepath.Base(path), "README.md")
	if count.None(rules.rows) || !readme {
		return nil
	}
	tall := len(lines) - header(lines)
	if tall <= rules.rows {
		return nil
	}
	return []Finding{Report(rel, len(lines), "height", fmt.Sprintf(
		"%d lines of prose; a readme fits %d", tall, rules.rows))}
}

// docLine lints one line of prose. it finds a banned word, an
// exclamation that is not an image or a not-equals, a width past the
// rule that is not a table row or a link, and shouting outside an
// indented block.
func (rules Lint) docLine(rel string, at int, line string) []Finding {
	var out []Finding
	if rules.words != nil {
		if word := rules.words.FindString(line); word != "" {
			out = append(out, Report(rel, at, "word", word))
		}
	}
	if rules.exclaim && strings.Contains(line, "!") &&
		!strings.Contains(line, "![") &&
		!strings.Contains(line, "!=") {
		said := strings.TrimSpace(line)
		out = append(out, Report(rel, at, "exclamation", said))
	}
	wide := width(line)
	if rules.tooWide(wide) &&
		!strings.HasPrefix(line, "|") &&
		!strings.Contains(line, "](") {
		columns := fmt.Sprintf("%d columns", wide)
		out = append(out, Report(rel, at, "width", columns))
	}
	if rules.shout && !strings.HasPrefix(line, "    ") &&
		!strings.HasPrefix(line, "\t") {
		for _, word := range shouted(line) {
			out = append(out, Finding{
				rel, at, "shouting", word, true,
			})
		}
	}
	return out
}

// long is the finding for a paragraph that runs past the rule.
func (rules Lint) long(rel string, start, ran int) Finding {
	return Finding{
		rel,
		start,
		"paragraph",
		fmt.Sprintf("%d lines; a paragraph holds %d", ran,
			rules.paragraph),
		false,
	}
}

// listItem matches a line starting a list item.
var listItem = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s`)

// paragraphs is a run of prose being measured against the rule: the
// line it started at, how many lines it has run to, and the findings
// for the ones that ran past.
type paragraphs struct {
	rules Lint
	rel   string
	start int
	ran   int
	out   []Finding
}

// ends is the end of a paragraph: one that ran past the rule is a
// finding, and the count starts again.
func (prose *paragraphs) ends() {
	if prose.ran > prose.rules.paragraph {
		prose.out = append(prose.out,
			prose.rules.long(prose.rel, prose.start, prose.ran))
	}
	prose.ran = count.Nothing
}

// line is one more line of the paragraph, started at this line when it
// is the first.
func (prose *paragraphs) line(at int) {
	if count.None(prose.ran) {
		prose.start = at
	}
	prose.ran++
}

// docParagraphs finds prose paragraphs in markdown that break the rule.
// blank lines or lists end them. code, tables and headings are skipped.
func (rules Lint) docParagraphs(lines []string, rel string) []Finding {
	prose := &paragraphs{rules: rules, rel: rel}
	var fence bool
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			prose.ends()
			fence = !fence
		case fence,
			trimmed == "",
			strings.HasPrefix(trimmed, "|"),
			strings.HasPrefix(trimmed, "#"),
			count.None(prose.ran) &&
				(strings.HasPrefix(line, "    ") ||
					strings.HasPrefix(line, "\t")):
			prose.ends()
		default:
			if listItem.MatchString(line) {
				prose.ends()
			}
			prose.line(count.Next(i))
		}
	}
	prose.ends()
	return prose.out
}

// commentParagraphs returns the paragraphs in one comment group that run past
// the rule. a blank line or a list item ends one. indented lines, go: lines
// and nolint lines are skipped.
func (rules Lint) commentParagraphs(
	fset *token.FileSet,
	cg *ast.CommentGroup,
	rel string,
) []Finding {
	prose := &paragraphs{rules: rules, rel: rel}
	for _, comment := range cg.List {
		if !strings.HasPrefix(comment.Text, "//") {
			prose.ends()
			continue
		}
		body := strings.TrimPrefix(comment.Text, "//")
		trimmed := strings.TrimSpace(body)
		switch {
		case trimmed == "",
			strings.HasPrefix(body, "\t"),
			strings.HasPrefix(body, "  "),
			strings.HasPrefix(
				trimmed,
				"go:",
			),
			strings.HasPrefix(trimmed, "nolint"):
			prose.ends()
		default:
			if listItem.MatchString(body) {
				prose.ends()
			}
			at := fset.Position(comment.Pos()).Line
			prose.line(at)
		}
	}
	prose.ends()
	return prose.out
}

// tabStop is how far a tab reaches, defined as go prints one: eight
// columns.
const tabStop = 8

// width is a line's width in columns, tabs counted as go prints them.
func width(line string) int {
	var columns int
	for _, char := range line {
		if char == '\t' {
			columns += tabStop - columns%tabStop
		} else {
			columns++
		}
	}
	return columns
}

// tracked lists the files git tracks or would track under dir, except
// testdata. it errors when dir is no repository.
func tracked(dir string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others",
		"--exclude-standard")
	cmd.Dir = dir
	listed, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, file := range strings.Split(string(listed), "\x00") {
		if file == "" || strings.HasPrefix(file, "testdata/") ||
			strings.Contains(file, "/testdata/") {
			continue
		}
		out = append(out, file)
	}
	return out, nil
}

// walked lists every file under dir, skipping what a repo would ignore.
func walked(dir string) ([]string, error) {
	walking := &walking{dir: dir}
	err := filepath.WalkDir(dir, walking.visit)
	return walking.out, err
}

// walking is a walk of a tree with no git, and the files found so far.
type walking struct {
	dir string
	out []string
}

// visit reads one entry: a directory a repo would ignore is skipped,
// and a file is listed from the tree's root.
func (walk *walking) visit(path string, entry os.DirEntry, err error) error {
	if err != nil {
		return err
	}
	name := entry.Name()
	if entry.IsDir() {
		if path != walk.dir && ignored(name) {
			return filepath.SkipDir
		}
		return nil
	}
	rel, _ := filepath.Rel(walk.dir, path)
	walk.out = append(walk.out, rel)
	return nil
}

// ignored is whether a directory's name is one a repo would ignore:
// hidden, or what is built, vendored or test data.
func ignored(name string) bool {
	return strings.HasPrefix(name, ".") || name == "bin" ||
		name == "build" || name == "testdata" || name == "vendor"
}
