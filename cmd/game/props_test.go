package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"

	"pgregory.net/rapid"
)

// Logs hold up to longestLog lines. Tail keeps up to mostKept of them.
// Up to mostWords words follow release.
const (
	longestLog = 20 // defined here: longer than a short log is kept whole
	mostKept   = 25 // defined here: more than the longest log, and fewer
	mostWords  = 6  // defined here: enough for forms and then a tag
)

// aLog is the draw of a log's lines and how many of them are kept.
func aLog(draw *rapid.T) ([]string, int) {
	line := rapid.StringMatching(`[a-z0-9 ]{1,12}`)
	lines := rapid.SliceOfN(line, count.One, longestLog).Draw(draw, "lines")
	keep := rapid.IntRange(count.One, mostKept).Draw(draw, "keep")
	return lines, keep
}

// indented is the lines, each indented as tail indents them.
func indented(lines []string) []string {
	var said []string
	for _, kept := range lines {
		said = append(said, "  "+kept)
	}
	return said
}

// tailOf is what tail says of a log with these lines, keeping that many.
func tailOf(draw *rapid.T, path string, lines []string, keep int) []string {
	body := strings.Join(lines, "\n") + "\n"
	readable := os.FileMode(0o644)
	if err := os.WriteFile(path, []byte(body), readable); err != nil {
		draw.Fatal(err)
	}
	var out bytes.Buffer
	tail(&out, path, keep)
	text := strings.TrimRight(out.String(), "\n")
	return strings.Split(text, "\n")
}

// loggedAt is a log file that tail is checked against.
type loggedAt struct{ path string }

// checkTail is one draw of a log: tail says what is kept of it.
func (log loggedAt) checkTail(draw *rapid.T) {
	lines, keep := aLog(draw)
	got := tailOf(draw, log.path, lines, keep)
	first := max(len(lines)-keep, count.Nothing)
	want := indented(lines[first:])
	if !slices.Equal(got, want) {
		draw.Fatalf("kept %d of %q: got %q, want %q", keep, lines, got, want)
	}
}

// Tail says the last lines of any log. It indents each. It says a short
// log whole.
func TestTailSaysTheLastLinesOfAnyLog(t *testing.T) {
	t.Parallel()
	log := loggedAt{path: filepath.Join(t.TempDir(), "log")}
	rapid.Check(t, log.checkTail)
}

// theFirstNoForm is the first word that is no form of release, or TAG
// when every word is one.
func theFirstNoForm(words []string) string {
	for _, each := range words {
		if !releaseForms[each] {
			return each
		}
	}
	return "TAG"
}

// checkFirstTag is one draw of words: firstTag finds the tag in them.
func checkFirstTag(draw *rapid.T) {
	word := rapid.SampledFrom([]string{
		"diff", "push", "stack", "v1", "v2.0", "rc", "x",
	})
	drawn := rapid.SliceOfN(word, count.Nothing, mostWords)
	words := drawn.Draw(draw, "words")
	if got, want := firstTag(words), theFirstNoForm(words); got != want {
		draw.Fatalf("%q: got %q, want %q", words, got, want)
	}
}

// The tag is the first word that is no form of release. When every word
// is a form it is TAG.
func TestFirstTagIsTheFirstWordThatIsNoForm(t *testing.T) {
	t.Parallel()
	rapid.Check(t, checkFirstTag)
}
