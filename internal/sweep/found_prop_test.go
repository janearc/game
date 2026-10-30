package sweep_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"pgregory.net/rapid"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/sweep"
)

// the never-words drawn here are made up and short; the filler words
// are longer than any of them, so no filler is ever one by accident.
// longest is the most words a never-word may run to: three, as defined:
// a word or a name of two or three.
const longest = 3

var (
	neverWord = rapid.StringMatching(`[a-z]{3,8}`)
	filler    = rapid.StringMatching(`[a-z]{9,12}`)
	between   = []string{" ", "_", "-", "\n", ", ", ". ", "  ", "/"}
)

// cased is a word with each letter's case drawn.
func cased(t *rapid.T, word string) string {
	var out strings.Builder
	for _, letter := range word {
		if rapid.Bool().Draw(t, "upper") {
			letter = unicode.ToUpper(letter)
		}
		out.WriteRune(letter)
	}
	return out.String()
}

// fillers is a run of filler words joined by spaces, possibly none.
func fillers(t *rapid.T, most int, label string) string {
	drawn := rapid.SliceOfN(filler, count.Nothing, most).Draw(t, label)
	return strings.Join(drawn, " ")
}

// bakedProperty is the property that any baked entry is found, drawn
// from entries of up to the most words a name may have.
type bakedProperty struct {
	entry       *rapid.Generator[[]string]
	mostEntries int
	mostFillers int
	seen        scratch
}

// check is the property as rapid runs it.
func (prop bakedProperty) check(t *rapid.T) {
	entries := rapid.SliceOfN(prop.entry, count.One,
		prop.mostEntries).Draw(t, "entries")
	list := make([]string, len(entries))
	for place, words := range entries {
		list[place] = strings.Join(words, " ")
	}
	salt := rapid.StringMatching(`[a-z0-9]{4,12}`).Draw(t, "salt")
	never := bakedFrom(t, salt, list)
	last := count.Last(len(entries))
	pick := entries[rapid.IntRange(count.Nothing, last).Draw(t, "pick")]
	var text strings.Builder
	text.WriteString(fillers(t, prop.mostFillers, "before") + " ")
	for place, word := range pick {
		if count.Positive(place) {
			text.WriteString(rapid.SampledFrom(between).Draw(t, "sep"))
		}
		text.WriteString(cased(t, word))
	}
	text.WriteString(" " + fillers(t, prop.mostFillers, "after"))
	other := fillers(t, prop.mostFillers, "other")
	found := prop.seen.hits(t, never, text.String(), other)
	if !found[count.First] {
		t.Fatalf("%q was not found in %q", pick, text.String())
	}
	if found[count.Next(count.First)] {
		t.Fatalf("found a never-word in %q, which has none", other)
	}
}

// any baked entry is found in a text that carries its words in order,
// whatever their case and whatever stands between them; a text of other
// words is not found.
func TestAnyBakedEntryIsFoundAndOnlyIt(t *testing.T) {
	entry := rapid.SliceOfN(neverWord, count.One, longest)
	prop := bakedProperty{
		seen:        newScratch(t),
		entry:       entry,
		mostEntries: int(held(t, "drawn-most-entries")),
		mostFillers: int(held(t, "drawn-most-fillers")),
	}
	rapid.Check(t, prop.check)
}

// overlongProperty is the property that an entry of more words than a
// name has is refused, drawn with well-formed entries beside it.
type overlongProperty struct {
	entry     *rapid.Generator[[]string]
	mostGood  int
	pastLimit int
}

// check is the property as rapid runs it.
func (prop overlongProperty) check(t *rapid.T) {
	var list []string
	good := rapid.SliceOfN(prop.entry, count.Nothing,
		prop.mostGood).Draw(t, "good")
	for _, words := range good {
		list = append(list, strings.Join(words, " "))
	}
	least := count.Next(longest)
	long := rapid.SliceOfN(neverWord, least, prop.pastLimit).Draw(t,
		"long")
	list = append(list, strings.Join(long, " "))
	_, err := sweep.Bake("a-salt", list)
	want := fmt.Sprintf("never-word %d is %d words", len(list), len(long))
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

// an entry of more words than a name has is refused, naming its place
// in the list and its length.
func TestBakeRefusesAnEntryLongerThanAName(t *testing.T) {
	entry := rapid.SliceOfN(neverWord, count.One, longest)
	prop := overlongProperty{
		entry:     entry,
		mostGood:  int(held(t, "drawn-most-good-entries")),
		pastLimit: longest + int(held(t, "drawn-overlong-span")),
	}
	rapid.Check(t, prop.check)
}
