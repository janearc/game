// Package lint runs whatever lint is described: the rules are lines
// in the dotfile or the repository's .game, and game has no opinions
// of its own.
//
// what a rule can be: every function commented on the line above it; no
// bare print outside package main, since a daemon logs and a library
// returns; banned words; no exclamation marks in docs.
//
// a width for prose, docs and comments, with wider code reported to look
// at, since a struct tag or a long string can be the good reason; a
// height for a readme; and shouty capitals in comments, reported rather
// than failed, because MUST and JSON are allowed to shout.
//
// go vet and gofmt are check's; staticcheck runs here if it is on the
// path.
package lint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Finding is one thing to fix, or one thing to look at.
type Finding struct {
	File string
	Line int
	Rule string
	Text string
	Look bool // reported, not failed
}

// String is the finding as one line: lint for a thing to fix, look for
// a thing to see.
func (f Finding) String() string {
	mark := "lint"
	if f.Look {
		mark = "look"
	}
	return fmt.Sprintf(
		"%s: %s:%d: %s: %s",
		mark,
		f.File,
		f.Line,
		f.Rule,
		f.Text,
	)
}

// Lint is the rules as described, ready to run.
type Lint struct {
	Width     int // 0 is no rule
	Rows      int
	Paragraph int // the most lines a paragraph of prose may run to
	Comments  bool
	Print     bool
	Exclaim   bool
	Shout     bool
	Tests     bool // lint the tests too; by default they are left alone
	words     *regexp.Regexp
	allow     map[string][]string // file to the rules it is let off
}

// Describe turns rule lines into a Lint; a bad argument is an error.
func Describe(rules []struct {
	Name string
	Args []string
}) (Lint, error) {
	var l Lint
	for _, r := range rules {
		switch r.Name {
		case "width", "rows", "paragraph":
			if len(r.Args) != 1 {
				return l, fmt.Errorf(
					"lint %s wants one number",
					r.Name,
				)
			}
			n := 0
			for _, c := range r.Args[0] {
				if c < '0' || c > '9' {
					return l, fmt.Errorf(
						"lint %s wants a "+
							"number, not %q",
						r.Name,
						r.Args[0],
					)
				}
				n = n*10 + int(c-'0')
			}
			switch r.Name {
			case "width":
				l.Width = n
			case "rows":
				l.Rows = n
			default:
				l.Paragraph = n
			}
		case "comments":
			l.Comments = true
		case "print":
			l.Print = true
		case "exclaim":
			l.Exclaim = true
		case "tests":
			l.Tests = true
		case "shout":
			l.Shout = true
		case "allow":
			if len(r.Args) < 2 {
				return l, fmt.Errorf(
					"lint allow wants a file and a lint",
				)
			}
			if l.allow == nil {
				l.allow = map[string][]string{}
			}
			for _, name := range r.Args[1:] {
				if to, ok := allowSaid[name]; ok {
					name = to
				}
				if !allowKnown[name] {
					return l, fmt.Errorf(
						"lint allow %s: no lint "+
							"called %q",
						r.Args[0], name,
					)
				}
				l.allow[r.Args[0]] = append(
					l.allow[r.Args[0]],
					name,
				)
			}
		case "words":
			if len(r.Args) == 0 {
				return l, fmt.Errorf(
					"lint words wants at least one word",
				)
			}
			quoted := make([]string, 0, len(r.Args))
			for _, w := range r.Args {
				quoted = append(quoted, regexp.QuoteMeta(w))
			}
			l.words = regexp.MustCompile(
				`(?i)\b(` + strings.Join(quoted, "|") + `)\b`,
			)
		default:
			return l, fmt.Errorf("unknown lint %q", r.Name)
		}
	}
	return l, nil
}

// testy is whether a path is a test rather than the thing being shipped.
//
// a test is ours to write as we like: it holds long table rows, fixtures
// and json on one line, and holding it to the width a reader of the
// shipped code needs buys nothing. a config that says "lint tests" gets
// them linted anyway.
func testy(path string) bool {
	base := filepath.Base(path)
	switch {
	case strings.HasPrefix(path, "testdata/"),
		strings.Contains(path, "/testdata/"),
		strings.HasSuffix(base, "_test.go"),
		strings.HasSuffix(base, "_test.py"),
		strings.HasSuffix(base, "_test.ts"),
		strings.HasSuffix(base, ".test.ts"),
		strings.HasSuffix(base, ".test.js"),
		strings.HasSuffix(base, "_test.exs"),
		strings.HasPrefix(base, "test_"):
		return true
	}
	return false
}

// Empty is whether nothing was described.
func (l Lint) Empty() bool {
	return l.Width == 0 && l.Rows == 0 && l.Paragraph == 0 && !l.Comments &&
		!l.Print && !l.Exclaim && !l.Shout && l.words == nil
}

// a run of capitals that is probably shouting: five or more letters,
// not one of the acronyms that are allowed to.
var shout = regexp.MustCompile(`\b[A-Z]{5,}\b`)

// shouted is the capitals in a line that are shouting: not allowed words,
// not a file's name (DESIGN.md, LICENSE.txt), and not inside backticks,
// where a name is quoted rather than said.
func shouted(line string) []string {
	var out []string
	ticks := 0
	last := 0
	for _, m := range shout.FindAllStringIndex(line, -1) {
		ticks += strings.Count(line[last:m[0]], "`")
		last = m[0]
		w := line[m[0]:m[1]]
		quoted := ticks%2 == 1
		if !allowed[w] && !quoted && !named(line, m[0], m[1]) {
			out = append(out, w)
		}
	}
	return out
}

// header is how many lines at the top of a document are its art.
//
// a readme in this house opens with a picture, the way a manpage opens
// with a name: a title, then one block of art, then the page itself. the
// art is not prose and does not count against the height, and a block
// anywhere further down is code and does.
func header(lines []string) int {
	i := 0
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
		return 0
	}
	fenced := strings.HasPrefix(strings.TrimSpace(lines[i]), "```")
	indented := strings.HasPrefix(lines[i], "    ") ||
		strings.HasPrefix(lines[i], "\t")
	if !fenced && !indented {
		return 0
	}
	if fenced {
		for j := i + 1; j < len(lines); j++ {
			fence := strings.TrimSpace(lines[j])
			if strings.HasPrefix(fence, "```") {
				return j + 1
			}
		}
		return 0
	}
	j := i
	for j < len(lines) {
		line := lines[j]
		if strings.TrimSpace(line) == "" ||
			strings.HasPrefix(line, "    ") ||
			strings.HasPrefix(line, "\t") {
			j++
			continue
		}
		break
	}
	return j
}

// named is whether the capitals at from:to are part of a file's name.
//
// the token around them is taken whole, since a name is letters, digits,
// dots, hyphens, underscores and slashes, and a name is anything in that
// token with a lowercase extension on the end: DESIGN.md, LICENSE.txt,
// Runbook-restore.md, doc/operation.md.
func named(line string, from, to int) bool {
	part := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			c >= '0' && c <= '9' ||
			c == '.' || c == '-' || c == '_' || c == '/'
	}
	start, end := from, to
	for start > 0 && part(line[start-1]) {
		start--
	}
	for end < len(line) && part(line[end]) {
		end++
	}
	token := line[start:end]
	dot := strings.LastIndexByte(token, '.')
	if dot < 0 || dot == len(token)-1 {
		return false
	}
	for i := dot + 1; i < len(token); i++ {
		if token[i] < 'a' || token[i] > 'z' {
			return false
		}
	}
	return true
}

// importLine is the shape of an import: a path, with an alias, a dot or an
// underscore in front of it at most.
var importLine = regexp.MustCompile(
	`^\s*([A-Za-z_]\w*|\.|_)?\s*"[^"]+"\s*(//.*)?$`)

// keywords that can stand in front of a string and make a statement, not
// an import.
var notAnAlias = map[string]bool{
	"return": true, "case": true, "panic": true, "go": true, "defer": true,
	"chan": true, "range": true, "break": true, "continue": true,
}

// imported is whether a line of go is an import: a path is one token and
// cannot be split, so its width is not a thing to fix. only the alias in
// front of it could be shorter, and that is a name, not a width.
func imported(line string) bool {
	m := importLine.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	return !notAnAlias[m[1]]
}

// allowed is a line the repository has said to leave alone: "game: allow
// RULE" in a comment on the line, or in a comment on the line above it.
//
// a rule with a counted debt is worth arguing with once. a line that is
// long because the thing it holds is long, an id, a url, a pattern, says
// so where it sits, and the count then means what it says.
func allowedHere(lines []string, i int, rule string) bool {
	says := func(text string) bool {
		at := strings.Index(text, "game: allow")
		if at < 0 {
			return false
		}
		rest := strings.TrimSpace(text[at+len("game: allow"):])
		return rest == "" || strings.HasPrefix(rest, rule)
	}
	if says(lines[i]) {
		return true
	}
	for j := i - 1; j >= 0; j-- {
		t := strings.TrimSpace(lines[j])
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "#") {
			return false
		}
		return says(t)
	}
	return false
}

// tagged is whether a line of go is a struct field with a tag: the tag
// is one literal in the grammar and cannot be split, so its width is
// never a thing to fix.
//
// the type may be written inline, as struct{ A, B string }, when it has
// no braces of its own inside.
var tagged = regexp.MustCompile(
	`^\s*[A-Za-z_]\w*(\s*,\s*[A-Za-z_]\w*)*\s+` +
		`([][*.\w]*struct\{[^{}` + "`" + `]*\}|[][*.\w]+(\{\})?)` +
		`\s+` + "`[^`]*`" + `\s*(//.*)?$`)

// allowed is what may be in capitals: acronyms, and the words RFC 2119
// gives capitals to. everything else is sentence case.
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

// Run lints a module rooted at dir by the rules described. What is
// linted is what git tracks or is about to: an ignored directory is
// somebody else's prose, but a new file not yet added is ours, and a
// lint that cannot see it calls a tree clean before its first commit.
//
// Outside a repository every file under dir counts, except testdata and
// bin.
func (l Lint) Run(dir string) ([]Finding, error) {
	files, err := tracked(dir)
	if err != nil {
		files, err = walked(dir)
		if err != nil {
			return nil, err
		}
	}
	var out []Finding
	for _, rel := range files {
		if testy(rel) && !l.Tests {
			continue
		}
		path := filepath.Join(dir, rel)
		switch {
		case strings.HasSuffix(rel, ".go"):
			f, e := l.goFile(path, rel)
			if e != nil {
				return nil, e
			}
			out = append(out, f...)
		case strings.HasSuffix(rel, ".md"):
			f, e := l.doc(path, rel)
			if e != nil {
				return nil, e
			}
			out = append(out, f...)
		}
	}
	// a class that did not run is not a class that passed. an absent
	// linter printed a note beside an exit of zero, which is a green
	// light bolted to a cut wire: every repository here reported clean
	// with staticcheck never once run against it.
	sc := tool("staticcheck")
	if sc == "" {
		out = append(out, Finding{
			File: "staticcheck",
			Rule: "staticcheck",
			Text: "staticcheck did not run and its findings are " +
				"unknown; go install honnef.co/go/tools/cmd/" +
				"staticcheck@latest",
		})
	}
	if sc != "" {
		cmd := exec.Command(sc, "./...")
		cmd.Dir = dir
		b, _ := cmd.CombinedOutput()
		body := strings.TrimSpace(string(b))
		for _, line := range strings.Split(body, "\n") {
			// only a finding is a finding: staticcheck also
			// writes warnings, and "matched no packages" is
			// what a tree with no go in it always gets.
			if finding.MatchString(line) {
				out = append(
					out,
					Finding{
						File: "staticcheck",
						Rule: "staticcheck",
						Text: line,
					},
				)
			}
		}
	}
	kept := make([]Finding, 0, len(out))
	for _, f := range out {
		if l.allowed(f.File, f.Rule) {
			continue
		}
		kept = append(kept, f)
	}
	out = kept
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
}

// allowSaid translates the word a config uses for a rule into the word a
// finding uses for it.
//
// they differ for four rules: the config names the rule as it is
// described, the finding as the line is measured. an allowance that does
// not translate matches nothing, and it used to do so in silence.
var allowSaid = map[string]string{
	"rows":     "height",
	"shout":    "shouting",
	"comments": "uncommented",
	"words":    "word",
	"exclaim":  "exclamation",
}

// allowKnown is every rule a finding can carry, so an allowance for a rule
// that does not exist is refused when the config is read rather than
// quietly allowing nothing.
var allowKnown = map[string]bool{
	"width": true, "height": true, "paragraph": true,
	"shouting": true, "uncommented": true, "word": true,
	"print": true, "exclamation": true, "tests": true,
}

// finding matches a staticcheck finding, which is file:line:col: text.
// anything else it writes is a warning about its own run.
var finding = regexp.MustCompile(`^[^ ]+:[0-9]+:[0-9]+: `)

// tool finds a linter: on the PATH first, then where go install puts
// things.
//
// `GOBIN` and `GOPATH/bin` are commonly not on a path, and a linter that is
// installed but unfound is reported as missing.
func tool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, v := range []string{"GOBIN", "GOPATH"} {
		out, err := exec.Command("go", "env", v).Output()
		if err != nil {
			continue
		}
		dir := strings.TrimSpace(string(out))
		if dir == "" {
			continue
		}
		if v == "GOPATH" {
			dir = filepath.Join(dir, "bin")
		}
		at := filepath.Join(dir, name)
		if fi, err := os.Stat(at); err == nil && !fi.IsDir() {
			return at
		}
	}
	return ""
}

// allowed is whether a config let one file off one rule.
//
// the exemption is written where the rest of the lint is written, so the
// page that carries it is named in the repository rather than marked up
// inside the prose a reader has to read.
func (l Lint) allowed(file, rule string) bool {
	for _, name := range l.allow[file] {
		if name == rule {
			return true
		}
	}
	return false
}

// Failed is whether any finding is one to fix.
func Failed(fs []Finding) bool {
	for _, f := range fs {
		if !f.Look {
			return true
		}
	}
	return false
}

// goFile is the rules for one go file.
func (l Lint) goFile(path, rel string) ([]Finding, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for _, cg := range file.Comments {
		if strings.HasPrefix(cg.Text(), "Code generated") {
			return nil, nil
		}
	}
	isMain := file.Name.Name == "main"
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !l.Comments {
			continue
		}
		if fn.Doc == nil || strings.TrimSpace(fn.Doc.Text()) == "" {
			out = append(
				out,
				Finding{
					rel,
					fset.Position(fn.Pos()).Line,
					"uncommented",
					fn.Name.Name +
						" has no comment above it",
					false,
				},
			)
		}
	}
	if l.Print && !isMain {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if ok && pkg.Name == "fmt" &&
				strings.HasPrefix(sel.Sel.Name, "Print") {
				why := "fmt." + sel.Sel.Name +
					" outside main: a library returns " +
					"and a daemon logs"
				at := fset.Position(call.Pos()).Line
				found := Finding{
					rel, at, "bare print", why, false,
				}
				out = append(out, found)
			}
			return true
		})
	}
	src, _ := os.ReadFile(path)
	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		if l.words != nil {
			if m := l.words.FindString(line); m != "" {
				out = append(
					out,
					Finding{rel, i + 1, "word", m, false},
				)
			}
		}
		if w := width(line); l.Width > 0 && w > l.Width &&
			!tagged.MatchString(line) &&
			!imported(line) &&
			!allowedHere(lines, i, "width") {
			look := !strings.HasPrefix(
				strings.TrimSpace(line),
				"//",
			)
			out = append(
				out,
				Finding{
					rel,
					i + 1,
					"width",
					fmt.Sprintf("%d columns", w),
					look,
				},
			)
		}
	}
	if l.Paragraph > 0 {
		for _, cg := range file.Comments {
			out = append(out, l.commentParagraphs(fset, cg, rel)...)
		}
	}
	if !l.Shout {
		return out, nil
	}
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			at := fset.Position(c.Pos()).Line
			for _, line := range strings.Split(c.Text, "\n") {
				if blocky(line) {
					continue
				}
				for _, w := range shouted(line) {
					out = append(
						out,
						Finding{
							rel,
							at,
							"shouting",
							w,
							true,
						},
					)
				}
			}
		}
	}
	return out, nil
}

// blocky is whether a line of a comment is a code block rather than
// prose.
//
// go renders an indented line in a doc comment as code, exactly as a
// fenced block in markdown is code, and what a usage line holds is
// grammar: the name of an argument, a setting, a branch.
//
// the shout rule reads prose, so it stops at the indent, the way it
// already stops at a fence.
func blocky(line string) bool {
	t := strings.TrimSpace(line)
	for _, mark := range []string{"//", "/*", "*"} {
		if strings.HasPrefix(t, mark) {
			t = strings.TrimPrefix(t, mark)
			break
		}
	}
	return strings.HasPrefix(t, "\t") || strings.HasPrefix(t, "    ")
}

// doc is the rules for one markdown file: banned words, no exclamation
// marks, since nothing is an emergency because nothing is an emergency,
// the width, and a readme's height.
func (l Lint) doc(path, rel string) ([]Finding, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Finding
	lines := strings.Split(strings.TrimRight(string(src), "\n"), "\n")
	if l.Rows > 0 && strings.EqualFold(filepath.Base(path), "README.md") {
		if n := len(lines) - header(lines); n > l.Rows {
			out = append(
				out,
				Finding{
					rel,
					len(lines),
					"height",
					fmt.Sprintf(
						"%d lines of prose; a readme "+
							"fits %d",
						n,
						l.Rows,
					),
					false,
				},
			)
		}
	}
	if l.Paragraph > 0 {
		out = append(out, l.docParagraphs(lines, rel)...)
	}
	code := false
	art := header(lines)
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			code = !code
			continue
		}
		if code {
			continue
		}
		// the header is a picture, and a picture is not wrapped: it is
		// already exempt from the height, and the width would ask a
		// bird to be narrower than it was drawn.
		if i < art {
			continue
		}
		if l.words != nil {
			if m := l.words.FindString(line); m != "" {
				out = append(
					out,
					Finding{rel, i + 1, "word", m, false},
				)
			}
		}
		if l.Exclaim && strings.Contains(line, "!") &&
			!strings.Contains(line, "![") &&
			!strings.Contains(line, "!=") {
			out = append(
				out,
				Finding{
					rel,
					i + 1,
					"exclamation",
					strings.TrimSpace(line),
					false,
				},
			)
		}
		if w := width(line); l.Width > 0 && w > l.Width &&
			!strings.HasPrefix(line, "|") &&
			!strings.Contains(line, "](") {
			out = append(
				out,
				Finding{
					rel,
					i + 1,
					"width",
					fmt.Sprintf("%d columns", w),
					false,
				},
			)
		}
		if l.Shout && !strings.HasPrefix(line, "    ") &&
			!strings.HasPrefix(line, "\t") {
			for _, w := range shouted(line) {
				out = append(
					out,
					Finding{
						rel,
						i + 1,
						"shouting",
						w,
						true,
					},
				)
			}
		}
	}
	return out, nil
}

// long is the finding for a paragraph that runs past the rule, named
// by the line it starts on.
func (l Lint) long(rel string, start, n int) Finding {
	return Finding{
		rel,
		start,
		"paragraph",
		fmt.Sprintf("%d lines; a paragraph holds %d", n, l.Paragraph),
		false,
	}
}

// listItem is whether a line of prose starts a list item, which is a
// paragraph of its own.
var listItem = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s`)

// docParagraphs is every paragraph of prose in a markdown file that runs
// past the rule. a paragraph is a run of lines between blank ones; a
// list item starts a new one; code, tables and headings are not prose.
func (l Lint) docParagraphs(lines []string, rel string) []Finding {
	var out []Finding
	start, n, fence := 0, 0, false
	flush := func() {
		if n > l.Paragraph {
			out = append(out, l.long(rel, start, n))
		}
		n = 0
	}
	for i, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "```"):
			flush()
			fence = !fence
		case fence,
			t == "",
			strings.HasPrefix(t, "|"),
			strings.HasPrefix(t, "#"),
			n == 0 &&
				(strings.HasPrefix(line, "    ") ||
					strings.HasPrefix(line, "\t")):
			flush()
		default:
			if listItem.MatchString(line) {
				flush()
			}
			if n == 0 {
				start = i + 1
			}
			n++
		}
	}
	flush()
	return out
}

// commentParagraphs is every paragraph in one comment group that runs
// past the rule: runs of comment lines between empty ones, a list item
// starting a new one, and indented lines, which are code or a table in
// a comment, left out.
func (l Lint) commentParagraphs(
	fset *token.FileSet,
	cg *ast.CommentGroup,
	rel string,
) []Finding {
	var out []Finding
	start, n := 0, 0
	flush := func() {
		if n > l.Paragraph {
			out = append(out, l.long(rel, start, n))
		}
		n = 0
	}
	for _, c := range cg.List {
		if !strings.HasPrefix(c.Text, "//") {
			flush()
			continue
		}
		body := strings.TrimPrefix(c.Text, "//")
		t := strings.TrimSpace(body)
		switch {
		case t == "",
			strings.HasPrefix(body, "\t"),
			strings.HasPrefix(body, "  "),
			strings.HasPrefix(
				t,
				"go:",
			),
			strings.HasPrefix(t, "nolint"):
			flush()
		default:
			if listItem.MatchString(body) {
				flush()
			}
			if n == 0 {
				start = fset.Position(c.Pos()).Line
			}
			n++
		}
	}
	flush()
	return out
}

// width is a line's width in columns, tabs counted as go prints them.
func width(line string) int {
	n := 0
	for _, r := range line {
		if r == '\t' {
			n += 8 - n%8
		} else {
			n++
		}
	}
	return n
}

// tracked is every file git tracks or would track under dir, relative
// to it, except testdata; an error means dir is not a repository.
func tracked(dir string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others",
		"--exclude-standard")
	cmd.Dir = dir
	b, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range strings.Split(string(b), "\x00") {
		if f == "" || strings.HasPrefix(f, "testdata/") ||
			strings.Contains(f, "/testdata/") {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

// walked is every file under dir, for a directory that is not a
// repository, skipping what a repository would ignore anyway.
func walked(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(
		dir,
		func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if path != dir &&
					(strings.HasPrefix(name, ".") ||
						name == "bin" ||
						name == "build" ||
						name == "testdata" ||
						name == "vendor") {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			out = append(out, rel)
			return nil
		},
	)
	return out, err
}
