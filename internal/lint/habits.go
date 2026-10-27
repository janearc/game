package lint

import (
	"github.com/janearc/game/internal/count"
	"go/ast"
	"go/token"
	"regexp"
	"strings"
	"unicode"
)

// the four habits a plain comment does without, each a rule a .game may
// ask for over the comments of every go file: chain, commas, opener and
// history. okapi measures the same four.

// joined matches a comma and a joining word: one more clause hung on
// the sentence.
var joined = regexp.MustCompile(
	`,\s+(?i:because|so|since|which|and|but|while|then|or|as|where|` +
		`when|unless|though)\b`,
)

// opener matches a sentence that starts with a or an.
var opener = regexp.MustCompile(`^(?i:a|an)\s`)

// history matches a comment that tells the code's past.
var history = regexp.MustCompile(
	`20\d\d-\d\d-\d\d|first draft|proposal|was tried|used to`,
)

// directive matches a comment line that instructs a tool rather than a
// reader.
var directive = regexp.MustCompile(
	`^\s*//(go:|\s*game: allow|line |nolint|ninja:)`,
)

// asksHabit is whether any of the four comment rules is asked.
func (rules Lint) asksHabit() bool {
	return rules.chain || count.Positive(rules.commas) ||
		rules.opener || rules.history
}

// habits is the four rules over one file's comments, each finding at
// the first line of its comment. a comment with noqa on any line is
// left alone whole.
func (rules Lint) habits(
	fset *token.FileSet, file *ast.File, rel string,
) []Finding {
	if !rules.asksHabit() {
		return nil
	}
	var out []Finding
	for _, group := range file.Comments {
		lines, ok := proseOf(group)
		if !ok {
			continue
		}
		at := fset.Position(group.Pos()).Line
		out = append(out, rules.habitsIn(rel, at, lines)...)
	}
	return out
}

// proseOf is the lines of a comment that are prose: the slashes and one
// space gone, directives and indented samples left out. false when the
// comment asks to be left alone, or is a block comment.
func proseOf(group *ast.CommentGroup) ([]string, bool) {
	var out []string
	for _, each := range group.List {
		if !strings.HasPrefix(each.Text, "//") {
			return nil, false
		}
		if noqa.MatchString(each.Text) {
			return nil, false
		}
		if directive.MatchString(each.Text) || blocky(each.Text) {
			continue
		}
		body := strings.TrimPrefix(each.Text, "//")
		out = append(out, strings.TrimPrefix(body, " "))
	}
	return out, count.Positive(len(out))
}

// habitsIn is the findings for one comment's prose: each habit a
// sentence has, and the history the comment tells.
func (rules Lint) habitsIn(rel string, at int, lines []string) []Finding {
	var out []Finding
	text := strings.Join(lines, " ")
	for _, sentence := range sentences(text) {
		if rules.chain && chained(sentence) {
			out = append(out, habit(rel, at, "chain", sentence))
		}
		if count.Positive(rules.commas) &&
			strings.Count(sentence, ",") >= rules.commas {
			out = append(out, habit(rel, at, "commas", sentence))
		}
		if rules.opener && opener.MatchString(sentence) {
			out = append(out, habit(rel, at, "opener", sentence))
		}
	}
	if rules.history {
		if told := history.FindString(text); told != "" {
			out = append(out, habit(rel, at, "history", told))
		}
	}
	return out
}

// twoClauses is how many joining words make a sentence a chain of
// clauses, defined here as two.
const twoClauses = 2

// chained is whether a sentence chains clauses: two or more joins.
func chained(sentence string) bool {
	joins := joined.FindAllStringIndex(sentence, every)
	return len(joins) >= twoClauses
}

// a sentence is cut to fit a line of a finding: sixty characters,
// with three dots to say it was cut.
const (
	fitsALine = 60    // defined here: what fits a line of a finding
	dots      = "..." // defined here: how a cut is shown
)

// habit is one finding, with the sentence cut to fit a line.
func habit(rel string, at int, rule, sentence string) Finding {
	if len(sentence) > fitsALine {
		sentence = sentence[:fitsALine-len(dots)] + dots
	}
	return Report(rel, at, rule, sentence)
}

// sentences cuts text into sentences at a full stop or a question mark
// that has a space after it. blank pieces are dropped.
func sentences(text string) []string {
	var out []string
	start := count.First
	for i := count.First; count.Next(i) < len(text); i++ {
		ends := text[i] == '.' || text[i] == '?'
		next := count.Next(i)
		if ends && unicode.IsSpace(rune(text[next])) {
			out = keep(out, text[start:next])
			start = next
		}
	}
	return keep(out, text[start:])
}

// keep appends piece to out, trimmed, unless it is blank.
func keep(out []string, piece string) []string {
	if piece = strings.TrimSpace(piece); piece != "" {
		out = append(out, piece)
	}
	return out
}
