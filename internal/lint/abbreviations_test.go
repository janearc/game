package lint_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/lint"
)

// fitsSource is a go file whose comment names FITS keywords and an
// archive, and one word in capitals that is no abbreviation.
const fitsSource = "package x\n\n" +
	"// BITPIX, NAXIS and HEASARC are named, and NEVER shouts.\n" +
	"var X = 1\n"

func TestAProjectsAbbreviationsAreNotShouting(t *testing.T) {
	t.Parallel()
	linter := described(t, config.Ruled("shout"),
		config.Ruled("abbreviations", "BITPIX", "NAXIS"),
		config.Ruled("abbreviations", "HEASARC"))
	if got := texts(goFound(t, linter, fitsSource)); !slices.Equal(got,
		[]string{"NEVER"}) {
		t.Errorf("in a comment: %v", got)
	}
	found := docFound(t, linter, "a.md", "a\nBITPIX and NEVER\n")
	if got := texts(found); !slices.Equal(got, []string{"NEVER"}) {
		t.Errorf("in a doc: %v", got)
	}
}

// The anti-test: with no abbreviations line, every one of them shouts.
func TestWithoutTheLineAbbreviationsShout(t *testing.T) {
	t.Parallel()
	linter := described(t, config.Ruled("shout"))
	got := texts(goFound(t, linter, fitsSource))
	if want := []string{"BITPIX", "NAXIS", "HEASARC",
		"NEVER"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// The anti-test: a line with no words, or a word that is not capitals,
// is refused, and the refusal names the word.
func TestAbbreviationsNotInCapitalsAreRefused(t *testing.T) {
	t.Parallel()
	for _, words := range [][]string{nil, {"Bitpix"}, {"2MASS"},
		{"NAXIS", "naxis1"}} {
		_, err := lint.Describe([]config.Rule{
			config.Ruled("abbreviations", words...)})
		if err == nil {
			t.Errorf("%q was taken", words)
			continue
		}
		if len(words) > 0 && !strings.Contains(err.Error(),
			words[len(words)-1]) {
			t.Errorf("%q: %v does not name it", words, err)
		}
	}
}
