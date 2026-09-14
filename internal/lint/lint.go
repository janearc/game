// Package lint is the house rules that a machine can check: every
// function commented on the line above it; no bare print outside
// package main, since a daemon logs and a library returns; no
// buzzwords; no exclamation marks in docs; eighty columns for prose,
// which is docs and comments, with wider code reported to look at,
// since a struct tag or a long string can be the good reason; and
// shouty capitals in comments, reported rather than failed, because
// MUST and JSON are allowed to shout. go vet and gofmt are check's;
// staticcheck runs here if it is on the path.
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

// the words that are banned from code, docs and names, from the house
// rules, and one that was said too much. assembled from pieces so this
// file does not find itself.
var buzzwords = regexp.MustCompile(`(?i)\b(` + strings.Join([]string{"hyper" + "scaler", "enterprise" + "-grade", "blazing" + "-fast", "blazingly" + " fast", "load" + "-bearing"}, "|") + `)\b`)

// a run of capitals that is probably shouting: five or more letters,
// not one of the acronyms that are allowed to.
var shout = regexp.MustCompile(`\b[A-Z]{5,}\b`)

var allowed = map[string]bool{"ASCII": true, "ANSI": true, "HTTPS": true, "OKLAB": true, "OKLCH": true, "JSON": true, "MUST": true, "SHALL": true, "SHOULD": true, "NOTE": true, "TODO": true, "UTF8": true, "NOLINT": true, "NOCOLOR": true, "PANIC": true}

// Run lints a module rooted at dir.
func Run(dir string) ([]Finding, error) {
	var out []Finding
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name != "." && (strings.HasPrefix(name, ".") || name == "bin" || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		switch {
		case strings.HasSuffix(name, ".go"):
			f, e := goFile(path, rel)
			if e != nil {
				return e
			}
			out = append(out, f...)
		case strings.HasSuffix(name, ".md"):
			f, e := doc(path, rel)
			if e != nil {
				return e
			}
			out = append(out, f...)
		}
		return nil
	})
	if err != nil {
		return nil, err
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
func goFile(path, rel string) ([]Finding, error) {
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
		if !ok {
			continue
		}
		if fn.Doc == nil || strings.TrimSpace(fn.Doc.Text()) == "" {
			out = append(out, Finding{rel, fset.Position(fn.Pos()).Line, "uncommented", fn.Name.Name + " has no comment on the line above it", false})
		}
	}
	if !isMain {
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
		if m := buzzwords.FindString(line); m != "" {
			out = append(out, Finding{rel, i + 1, "buzzword", m, false})
		}
		if w := width(line); w > Columns {
			look := !strings.HasPrefix(strings.TrimSpace(line), "//")
			out = append(out, Finding{rel, i + 1, "width", fmt.Sprintf("%d columns", w), look})
		}
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

// doc is the rules for one markdown file: no buzzwords, no exclamation
// marks, since nothing is an emergency because nothing is an emergency.
func doc(path, rel string) ([]Finding, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Finding
	code := false
	for i, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			code = !code
			continue
		}
		if code {
			continue
		}
		if m := buzzwords.FindString(line); m != "" {
			out = append(out, Finding{rel, i + 1, "buzzword", m, false})
		}
		if strings.Contains(line, "!") && !strings.Contains(line, "![") && !strings.Contains(line, "!=") {
			out = append(out, Finding{rel, i + 1, "exclamation", strings.TrimSpace(line), false})
		}
		if w := width(line); w > Columns && !strings.HasPrefix(line, "|") && !strings.Contains(line, "](") {
			out = append(out, Finding{rel, i + 1, "width", fmt.Sprintf("%d columns", w), false})
		}
	}
	return out, nil
}

// Columns is the width prose keeps to: what a reader's screen shows,
// and what jane reads at. tables and lines with a link are let be in
// docs, since neither wraps.
const Columns = 80

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
