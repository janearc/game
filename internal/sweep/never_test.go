package sweep

import (
	"strings"
	"testing"
)

// the never-words in these tests are made up, as the brief asks: no real
// one is written anywhere, and these are nobody's.
var madeUp = []string{"zorblax", "Quinta Veltrane", "ada mira lovelock"}

func bakedFrom(t *testing.T, salt string, list []string) Never {
	t.Helper()
	body, err := Bake(salt, list)
	if err != nil {
		t.Fatal(err)
	}
	return ParseNever(body)
}

// a word, a name of two and a name of three are each found, whatever
// their case and whatever stands between their words.
func TestAFoundNeverWord(t *testing.T) {
	never := bakedFrom(t, "a-salt", madeUp)
	for _, text := range []string{
		"hello, Zorblax!",
		"by quinta\nveltrane, in the margin",
		"written for ADA_MIRA_LOVELOCK",
	} {
		if !never.found(text) {
			t.Errorf("a never-word was missed in %q", text)
		}
	}
}

// a never-word is matched as whole words: one inside a longer word, half
// a name, or the words of a name apart, are not it.
func TestWhatIsNotANeverWord(t *testing.T) {
	never := bakedFrom(t, "a-salt", madeUp)
	for _, text := range []string{
		"zorblaxian lore",
		"quinta alone",
		"veltrane, then quinta",
		"ada mira, and later lovelock",
	} {
		if never.found(text) {
			t.Errorf("%q was taken for a never-word", text)
		}
	}
	if (Never{}).found("zorblax") {
		t.Error("a game with no never-words found one")
	}
}

// the baked file holds a salt and hashes and none of the words, and the
// same list and salt bake the same bytes in any order, so the same
// sources build the same binary; another salt bakes other hashes.
func TestBakingKeepsNoWord(t *testing.T) {
	body, err := Bake("a-salt", madeUp)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range madeUp {
		for _, word := range wordsOf(entry) {
			if strings.Contains(strings.ToLower(body), word) {
				t.Errorf("the baked file carries %q", word)
			}
		}
	}
	if !strings.HasPrefix(body, "salt a-salt\n") ||
		strings.Count(body, "\n") != len(madeUp)+1 {
		t.Errorf("not a salt and one hash a word:\n%s", body)
	}
	again, _ := Bake("a-salt", madeUp)
	turned, _ := Bake("a-salt", []string{madeUp[2], madeUp[0], madeUp[1]})
	other, _ := Bake("b-salt", madeUp)
	hashesOnly := func(baked string) string {
		_, hashes, _ := strings.Cut(baked, "\n")
		return hashes
	}
	if again != body || turned != body ||
		hashesOnly(other) == hashesOnly(body) {
		t.Error("baking is not the same bytes for the same list and " +
			"salt in any order, or is the same for another salt")
	}
}

// an entry the sweep could never find is refused, by its place and
// never by its words.
func TestBakingRefusesTooLongAName(t *testing.T) {
	_, err := Bake("a-salt", []string{"zorblax", "one two three four"})
	if err == nil {
		t.Fatal("a four-word never-word was baked")
	}
	if !strings.Contains(err.Error(), "never-word 2") ||
		strings.Contains(err.Error(), "three four") {
		t.Errorf("the refusal should name the place and no word: %v", err)
	}
	if _, err := Bake("a-salt", []string{"..."}); err == nil {
		t.Error("an entry with no words was baked")
	}
}

// a hit says a never-word was there and never which.
func TestAHitNamesNoWord(t *testing.T) {
	hits := Text("a subject", "thanks, zorblax", bakedFrom(t, "a-salt", madeUp))
	if len(hits) != 1 || hits[0].Kind != NeverWord ||
		strings.Contains(hits[0].Kind, "zorblax") {
		t.Errorf("not one hit that names no word: %v", hits)
	}
}
