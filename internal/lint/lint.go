// Package lint runs whatever lint is described: the rules are lines
// in the dotfile or the repository's .game, and game has no opinions
// of its own. what a rule can be: every function commented on the
// line above it; no bare print outside package main, since a daemon
// logs and a library returns; banned words; no exclamation marks in
// docs; a width for prose, docs and comments, with wider code reported
// to look at, since a struct tag or a long string can be the good
// reason; a height for a readme; and shouty capitals in comments,
// reported rather than failed, because MUST and JSON are allowed to
// shout. go vet and gofmt are check's; staticcheck runs here if it is
// on the path.
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
	return fmt.Sprintf("%s: %s:%d: %s: %s", mark, f.File, f.Line, f.Rule, f.Text)
}

// Lint is the rules as described, ready to run.
type Lint struct {
	Width    int // 0 is no rule
	Rows     int
	Comments bool
	Print    bool
	Exclaim  bool
	Shout    bool
	words    *regexp.Regexp
}

// Describe turns rule lines into a Lint; a bad argument is an error.
func Describe(rules []struct {
	Name string
	Args []string
}) (Lint, error) {
	var l Lint
	for _, r := range rules {
		switch r.Name {
		case "width", "rows":
			if len(r.Args) != 1 {
				return l, fmt.Errorf("lint %s wants one number", r.Name)
			}
			n := 0
			for _, c := range r.Args[0] {
				if c < '0' || c > '9' {
					return l, fmt.Errorf("lint %s wants a number, not %q", r.Name, r.Args[0])
				}
				n = n*10 + int(c-'0')
			}
			if r.Name == "width" {
				l.Width = n
			} else {
				l.Rows = n
			}
		case "comments":
			l.Comments = true
		case "print":
			l.Print = true
		case "exclaim":
			l.Exclaim = true
		case "shout":
			l.Shout = true
		case "words":
			if len(r.Args) == 0 {
				return l, fmt.Errorf("lint words wants at least one word")
			}
			quoted := make([]string, 0, len(r.Args))
			for _, w := range r.Args {
				quoted = append(quoted, regexp.QuoteMeta(w))
			}
			l.words = regexp.MustCompile(`(?i)\b(` + strings.Join(quoted, "|") + `)\b`)
		default:
			return l, fmt.Errorf("unknown lint %q", r.Name)
		}
	}
	return l, nil
}

// Empty is whether nothing was described.
func (l Lint) Empty() bool {
	return l.Width == 0 && l.Rows == 0 && !l.Comments && !l.Print && !l.Exclaim && !l.Shout && l.words == nil
}

// a run of capitals that is probably shouting: five or more letters,
// not one of the acronyms that are allowed to.
var shout = regexp.MustCompile(`\b[A-Z]{5,}\b`)

var allowed = map[string]bool{"ASCII": true, "ANSI": true, "HTTPS": true, "OKLAB": true, "OKLCH": true, "JSON": true, "MUST": true, "SHALL": true, "SHOULD": true, "NOTE": true, "TODO": true, "UTF8": true, "NOLINT": true, "NOCOLOR": true, "PANIC": true}

// Run lints a module rooted at dir by the rules described. What is
// linted is what git tracks, since that is the definition of ours: a
// toolchain fetched into an ignored directory is somebody else's prose.
// Outside a repository every file under dir counts, except testdata
// and bin.
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
	if sc, e := exec.LookPath("staticcheck"); e == nil {
		cmd := exec.Command(sc, "./...")
		cmd.Dir = dir
		b, _ := cmd.CombinedOutput()
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line != "" {
				out = append(out, Finding{File: "staticcheck", Rule: "staticcheck", Text: line})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out, nil
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
			out = append(out, Finding{rel, fset.Position(fn.Pos()).Line, "uncommented", fn.Name.Name + " has no comment on the line above it", false})
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
			if ok && pkg.Name == "fmt" && strings.HasPrefix(sel.Sel.Name, "Print") {
				out = append(out, Finding{rel, fset.Position(call.Pos()).Line, "bare print", "fmt." + sel.Sel.Name + " outside package main; a library returns and a daemon logs", false})
			}
			return true
		})
	}
	src, _ := os.ReadFile(path)
	for i, line := range strings.Split(string(src), "\n") {
		if l.words != nil {
			if m := l.words.FindString(line); m != "" {
				out = append(out, Finding{rel, i + 1, "word", m, false})
			}
		}
		if w := width(line); l.Width > 0 && w > l.Width {
			look := !strings.HasPrefix(strings.TrimSpace(line), "//")
			out = append(out, Finding{rel, i + 1, "width", fmt.Sprintf("%d columns", w), look})
		}
	}
	if !l.Shout {
		return out, nil
	}
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			for _, w := range shout.FindAllString(c.Text, -1) {
				if !allowed[w] {
					out = append(out, Finding{rel, fset.Position(c.Pos()).Line, "shouting", w, true})
				}
			}
		}
	}
	return out, nil
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
	if l.Rows > 0 && strings.EqualFold(filepath.Base(path), "README.md") && len(lines) > l.Rows {
		out = append(out, Finding{rel, len(lines), "height", fmt.Sprintf("%d lines; a readme fits %d", len(lines), l.Rows), false})
	}
	code := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			code = !code
			continue
		}
		if code {
			continue
		}
		if l.words != nil {
			if m := l.words.FindString(line); m != "" {
				out = append(out, Finding{rel, i + 1, "word", m, false})
			}
		}
		if l.Exclaim && strings.Contains(line, "!") && !strings.Contains(line, "![") && !strings.Contains(line, "!=") {
			out = append(out, Finding{rel, i + 1, "exclamation", strings.TrimSpace(line), false})
		}
		if w := width(line); l.Width > 0 && w > l.Width && !strings.HasPrefix(line, "|") && !strings.Contains(line, "](") {
			out = append(out, Finding{rel, i + 1, "width", fmt.Sprintf("%d columns", w), false})
		}
	}
	return out, nil
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

// tracked is every file git tracks under dir, relative to it, except
// testdata; an error means dir is not a repository.
func tracked(dir string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = dir
	b, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range strings.Split(string(b), "\x00") {
		if f == "" || strings.HasPrefix(f, "testdata/") || strings.Contains(f, "/testdata/") {
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
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != dir && (strings.HasPrefix(name, ".") || name == "bin" || name == "build" || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		out = append(out, rel)
		return nil
	})
	return out, err
}
