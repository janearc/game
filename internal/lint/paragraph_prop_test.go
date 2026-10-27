package lint_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"pgregory.net/rapid"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
)

// a line of prose: starts with a letter, so it is not a heading, a
// table row, a fence, a list item or an indented line.
var prose = rapid.StringMatching(`[a-z][a-z ,]{0,40}`)

// paragraphsProperty is the property that the findings are exactly the
// paragraphs longer than the rule, each at its first line.
type paragraphsProperty struct {
	dir       string
	mostRule  int
	mostLines int
	mostSizes int
	mostGap   int
}

// check is the property as rapid runs it.
func (prop paragraphsProperty) check(t *rapid.T) {
	rule := rapid.IntRange(count.One, prop.mostRule).Draw(t, "paragraph")
	given := strconv.Itoa(rule)
	linter, err := lint.Describe([]config.Rule{
		config.Ruled("paragraph", given),
	})
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	var want []lint.Finding
	var doc strings.Builder
	sizes := rapid.SliceOfN(rapid.IntRange(count.One, prop.mostLines),
		count.Nothing, prop.mostSizes)
	for _, size := range sizes.Draw(t, "sizes") {
		gap := rapid.IntRange(count.One, prop.mostGap).Draw(t, "gap")
		for range gap {
			lines = append(lines, "")
			doc.WriteString("\n")
		}
		start := count.Next(len(lines))
		for range size {
			drawn := prose.Draw(t, "line")
			lines = append(lines, drawn)
			doc.WriteString(drawn + "\n")
		}
		if size > rule {
			text := fmt.Sprintf("%d lines; a paragraph holds %d", size, rule)
			want = append(want, lint.Report("doc.md", start, "paragraph", text))
		}
	}
	got := prop.ran(t, linter, doc.String())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v\nlines %q", got, want, lines)
	}
}

// ran lints the doc as doc.md in the property's directory and gives the
// findings less staticcheck's.
func (prop paragraphsProperty) ran(
	t *rapid.T, linter lint.Lint, doc string,
) []lint.Finding {
	path := filepath.Join(prop.dir, "doc.md")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(prop.dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []lint.Finding
	for _, one := range found {
		if one.Rule() != "staticcheck" {
			out = append(out, one)
		}
	}
	return out
}

// paragraphs of drawn sizes, separated by blank lines: the findings are
// exactly the paragraphs longer than the rule, each at its first line.
func TestDocParagraphsFindsExactlyTheLongOnes(t *testing.T) {
	onPath(t, quietStaticcheck(t))
	prop := paragraphsProperty{
		dir:       t.TempDir(),
		mostRule:  int(held(t, "drawn-most-paragraph")),
		mostLines: int(held(t, "drawn-most-lines")),
		mostSizes: int(held(t, "drawn-most-sizes")),
		mostGap:   int(held(t, "drawn-most-gap")),
	}
	rapid.Check(t, prop.check)
}

// tabWidth is the columns a tab advances to the next multiple of, as
// go's tabwriter has defined it: eight.
const tabWidth = 8

// widthProperty is the property that a line is too wide when its width
// is past the rule, where the width counts runes, not bytes, and a tab
// goes to the next column that is a multiple of eight, as go prints it.
type widthProperty struct {
	dir  string
	text *rapid.Generator[string]
	most int
}

// check is the property as rapid runs it.
func (prop widthProperty) check(t *rapid.T) {
	line := prop.text.Draw(t, "line")
	rule := rapid.IntRange(count.One, prop.most).Draw(t, "width")
	runes := utf8.RuneCountInString(line)
	tabbed := runes + tabWidth - runes%tabWidth
	path := filepath.Join(prop.dir, "doc.md")
	body := "x\n" + line + "\n" + line + "\t\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	widest := strconv.Itoa(rule)
	linter, err := lint.Describe([]config.Rule{config.Ruled("width", widest)})
	if err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(prop.dir)
	if err != nil {
		t.Fatal(err)
	}
	wide := map[int]bool{}
	for _, one := range found {
		if one.Rule() == "width" {
			wide[lineOf(one)] = true
		}
	}
	plain, withTab := count.Next(count.One), count.Next(count.Next(count.One))
	if wide[plain] != (runes > rule) || wide[withTab] != (tabbed > rule) {
		t.Fatalf("%q at %d: %d runes, %d with a tab, found %v", line, rule,
			runes, tabbed, wide)
	}
}

// width counts runes, not bytes, and a tab goes to the next column that
// is a multiple of eight, as go prints it.
func TestWidthCountsRunesAndTabsAsGoPrintsThem(t *testing.T) {
	onPath(t, quietStaticcheck(t))
	text := rapid.StringMatching(`[a-zA-Z0-9 .,;:éß→]{0,40}`)
	prop := widthProperty{dir: t.TempDir(), text: text, most: tabWidth * tabWidth}
	rapid.Check(t, prop.check)
}
