// Package mutants is the mutation run. it changes a repository's source one
// small thing at a time, an operator, a number or a guard, and runs each
// package's tests against each change. a change the tests do not notice
// survives.
//
// when a survivor makes the code wrong, a test is written that fails on it.
// when it makes no difference to anything the package promises, its name
// goes in .mutants at the repository root with the reason, and the run lets
// it by.
package mutants

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/count"
)

// Mutant is one change to one file. it holds where, what it was, what it
// became and the changed file's whole source.
type Mutant struct {
	file     string // the file's path from the repository root
	function string // the function the change is in, or "package"
	line     int
	before   string
	after    string
	source   []byte
}

// File is the file's path from the repository root.
func (mutant Mutant) File() string { return mutant.file }

// Line is the line the change is on.
func (mutant Mutant) Line() int { return mutant.line }

// Name is how a mutant is known in .mutants and in a report. it is the
// file, the function and the text changed, not the line, which moves
// whenever anything above it does.
func (mutant Mutant) Name() string {
	return fmt.Sprintf(
		"%s %s: %s => %s",
		mutant.file,
		mutant.function,
		mutant.before,
		mutant.after,
	)
}

// where is the file and function, as --only matches them.
func (mutant Mutant) where() string {
	return mutant.file + ":" + mutant.function
}

// change is one mutation that can be applied to the parse and undone.
type change interface {
	apply()
	undo()
}

// swap is a change that puts one value where another was.
type swap[value any] struct {
	at       *value
	to, from value
}

// apply puts the new value in.
func (made swap[value]) apply() { *made.at = made.to }

// undo puts the old value back.
func (made swap[value]) undo() { *made.at = made.from }

// the operators swapped. arithmetic ones have one partner and comparisons
// have two.
var (
	arithmeticSwap = map[token.Token]token.Token{
		token.ADD:        token.SUB,
		token.SUB:        token.ADD,
		token.MUL:        token.QUO,
		token.QUO:        token.MUL,
		token.LAND:       token.LOR,
		token.LOR:        token.LAND,
		token.ADD_ASSIGN: token.SUB_ASSIGN,
		token.SUB_ASSIGN: token.ADD_ASSIGN,
		token.MUL_ASSIGN: token.QUO_ASSIGN,
		token.QUO_ASSIGN: token.MUL_ASSIGN,
		token.INC:        token.DEC,
		token.DEC:        token.INC,
	}
	comparisonSwap = map[token.Token][]token.Token{
		token.LSS: {token.LEQ, token.GEQ},
		token.LEQ: {token.LSS, token.GTR},
		token.GTR: {token.GEQ, token.LEQ},
		token.GEQ: {token.GTR, token.LSS},
		token.EQL: {token.NEQ},
		token.NEQ: {token.EQL},
	}
	callSwap = map[string]string{"Min": "Max", "Max": "Min"}
)

// of is every mutant of one source file, named by its path from the root.
func of(root, rel string) ([]Mutant, error) {
	path := filepath.Join(root, rel)
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	walk := &walker{rel: filepath.ToSlash(rel), fileSet: fileSet,
		file: file}
	for _, declaration := range file.Decls {
		walk.function = "package"
		if decl, ok := declaration.(*ast.FuncDecl); ok {
			walk.function = decl.Name.Name
		}
		walk.parents = nil
		ast.Inspect(declaration, walk.visit)
	}
	return walk.mutants, nil
}

// walker holds one file's parse while its mutants are made.
type walker struct {
	rel      string
	fileSet  *token.FileSet
	file     *ast.File
	function string
	parents  []ast.Node
	mutants  []Mutant
}

// render prints a node as source. a node of a parsed file, printed to a
// buffer, has nothing to fail on.
func (walk *walker) render(node any) string {
	var out bytes.Buffer
	_ = printer.Fprint(&out, walk.fileSet, node)
	return out.String()
}

// oneLine is a node's source on one line, without the comment over a
// constant.
func (walk *walker) oneLine(node ast.Node) string {
	if spec, ok := node.(*ast.ValueSpec); ok {
		undocumented := swap[*ast.CommentGroup]{at: &spec.Doc,
			from: spec.Doc}
		undocumented.apply()
		defer undocumented.undo()
	}
	words := strings.Fields(walk.render(node))
	return strings.Join(words, " ")
}

// around is what the report shows for the current node. It is the nearest
// statement or declaration.
//
// A node in the head of an if, for or range is named by the head. So is a
// node in a switch's tag, and one in an expression of a case. A node in a
// body is not. A select's case is a statement and is named as one.
//
// The top of the walk is a function or a declaration, so every node has a
// name.
func (walk *walker) around() ast.Node {
	for i := count.Last(len(walk.parents)); ; i-- {
		var from ast.Node
		if count.Next(i) < len(walk.parents) {
			from = walk.parents[count.Next(i)]
		}
		switch parent := walk.parents[i].(type) {
		case *ast.AssignStmt, *ast.ReturnStmt, *ast.ExprStmt,
			*ast.IncDecStmt, *ast.ValueSpec, *ast.DeclStmt,
			*ast.SendStmt, *ast.GoStmt, *ast.DeferStmt,
			*ast.FuncDecl, *ast.GenDecl:
			return parent
		case *ast.IfStmt:
			if from == parent.Cond {
				return parent.Cond
			}
		case *ast.ForStmt:
			if from == parent.Cond {
				return parent.Cond
			}
		case *ast.RangeStmt:
			if from == parent.X {
				return parent.X
			}
		case *ast.SwitchStmt:
			if from == parent.Tag {
				return parent.Tag
			}
		case *ast.CaseClause:
			listed, ok := from.(ast.Expr)
			if ok && slices.Contains(parent.List, listed) {
				return listed
			}
		}
	}
}

// record makes one mutant: the change is applied, the file and the
// statement around the change are printed, and the change is undone.
func (walk *walker) record(shown, at ast.Node, made change) {
	before := walk.oneLine(shown)
	made.apply()
	line := walk.fileSet.Position(at.Pos()).Line
	source := walk.render(walk.file)
	walk.mutants = append(walk.mutants, Mutant{
		file:     walk.rel,
		function: walk.function,
		line:     line,
		before:   before,
		after:    walk.oneLine(shown),
		source:   []byte(source),
	})
	made.undo()
}

// pop leaves the node last entered.
func (walk *walker) pop() {
	walk.parents = walk.parents[:count.Last(len(walk.parents))]
}

// visit tracks node parents and creates mutants.
func (walk *walker) visit(node ast.Node) bool {
	if node == nil {
		walk.pop()
		return true
	}
	walk.parents = append(walk.parents, node)
	switch node := node.(type) {
	case *ast.ArrayType:
		// an array's length is not arithmetic. inspect sends no nil for
		// a node it did not enter, so pop it here.
		walk.pop()
		return false
	case *ast.BinaryExpr:
		walk.binary(node)
	case *ast.UnaryExpr:
		if node.Op == token.SUB {
			walk.record(walk.around(), node, swap[token.Token]{
				at: &node.Op, to: token.ADD, from: token.SUB})
		}
	case *ast.AssignStmt:
		walk.assign(node)
	case *ast.IncDecStmt:
		walk.record(node, node, swap[token.Token]{at: &node.Tok,
			to: arithmeticSwap[node.Tok], from: node.Tok})
	case *ast.BasicLit:
		walk.literal(node)
	case *ast.CallExpr:
		walk.call(node)
	case *ast.IfStmt:
		walk.guard(node)
	}
	return true
}

// binary swaps an operator for its partner, or for each of a
// comparison's two.
func (walk *walker) binary(node *ast.BinaryExpr) {
	original := node.Op
	swaps := comparisonSwap[original]
	if swap, ok := arithmeticSwap[original]; ok {
		swaps = []token.Token{swap}
	}
	for _, swapped := range swaps {
		walk.record(walk.around(), node, swap[token.Token]{
			at: &node.Op, to: swapped, from: original})
	}
}

// assign swaps an arithmetic assignment, += for -= and the like.
func (walk *walker) assign(node *ast.AssignStmt) {
	swapped, ok := arithmeticSwap[node.Tok]
	if !ok {
		return
	}
	walk.record(node, node, swap[token.Token]{at: &node.Tok,
		to: swapped, from: node.Tok})
}

// literal adds one to an integer, and doubles a float and adds one.
func (walk *walker) literal(node *ast.BasicLit) {
	original := node.Value
	var changed string
	switch node.Kind {
	case token.INT:
		value, err := strconv.ParseInt(original, prefixed, wordBits)
		if err != nil {
			return
		}
		changed = strconv.FormatInt(value+count.One, decimal)
	case token.FLOAT:
		value, err := strconv.ParseFloat(original, wordBits)
		if err != nil {
			return
		}
		changed = strconv.FormatFloat(doubled*value+count.One, 'g',
			shortest, wordBits)
	default:
		return
	}
	walk.record(walk.around(), node, swap[string]{at: &node.Value,
		to: changed, from: original})
}

// how a literal is read and written: its base from its prefix, into
// 64 bits; written back in decimal, a float as short as reads the same.
// a float is doubled and one is added, so a change of sign or scale
// shows.
const (
	prefixed = 0  // defined by strconv: the base from the prefix
	wordBits = 64 // defined by go: int64 and float64
	decimal  = 10 // defined by strconv: base ten
	shortest = -1 // defined by strconv: the fewest digits that read back
	doubled  = 2  // defined here: a float is doubled
)

// call swaps a Min for a Max and a Max for a Min.
func (walk *walker) call(node *ast.CallExpr) {
	selector, ok := node.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	swapped, ok := callSwap[selector.Sel.Name]
	if !ok {
		return
	}
	walk.record(walk.around(), node, swap[string]{at: &selector.Sel.Name,
		to: swapped, from: selector.Sel.Name})
}

// guard negates an if's condition, then empties its body.
func (walk *walker) guard(node *ast.IfStmt) {
	condition := node.Cond
	shown := &ast.ParenExpr{X: condition}
	negated := &ast.UnaryExpr{
		Op: token.NOT,
		X:  &ast.ParenExpr{X: condition},
	}
	walk.record(shown, node, negating{head: node, shown: shown,
		from: condition, to: negated})
	walk.record(node, node, swap[[]ast.Stmt]{at: &node.Body.List,
		from: node.Body.List})
}

// negating is the change that negates an if's condition, and keeps the
// shown parenthesised condition saying the same.
type negating struct {
	head     *ast.IfStmt
	shown    *ast.ParenExpr
	from, to ast.Expr
}

// apply puts the negation in.
func (made negating) apply() {
	made.head.Cond, made.shown.X = made.to, made.to
}

// undo puts the condition back.
func (made negating) undo() {
	made.head.Cond, made.shown.X = made.from, made.from
}
