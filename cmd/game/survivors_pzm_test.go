package main

import (
	"slices"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
)

// TestNormalizePzmArgsOfNoWordsIsNoWords: nothing to rename is nothing,
// and it does not reach for a first word that is not there.
func TestNormalizePzmArgsOfNoWordsIsNoWords(t *testing.T) {
	t.Parallel()
	if got := normalizePzmArgs(nil); count.Positive(len(got)) {
		t.Errorf("no words became %q", got)
	}
	if got := normalizePzmArgs([]string{}); count.Positive(len(got)) {
		t.Errorf("an empty list became %q", got)
	}
}

// TestNormalizePzmArgsRenamesOnlyTheFirstWord: who is renamed first and
// nowhere else.
func TestNormalizePzmArgsRenamesOnlyTheFirstWord(t *testing.T) {
	t.Parallel()
	args := []string{"stack", "who", "history"}
	if got := normalizePzmArgs(args); !slices.Equal(got, args) {
		t.Errorf("later words became %q", got)
	}
}

// TestNormalizePzmArgsLeavesAWordAndALongFlagAlone: a word that names a
// legacy flag is a word, and a long flag already has its dashes.
func TestNormalizePzmArgsLeavesAWordAndALongFlagAlone(t *testing.T) {
	t.Parallel()
	args := []string{"history", "only", "scheme=S", "--only", "--workers=3"}
	if got := normalizePzmArgs(args); !slices.Equal(got, args) {
		t.Errorf("normalized %q to %q", args, got)
	}
	if got := normalizePzmArgs([]string{"-only", "-workers=3"}); !slices.Equal(got, []string{"--only", "--workers=3"}) {
		t.Errorf("legacy flags became %q", got)
	}
}

// TestTheBenchSpeaksOfFourTimesTheWorkersAndAQuietRunOfThirtySeconds: the
// two numbers the docs give.
func TestTheBenchSpeaksOfFourTimesTheWorkersAndAQuietRunOfThirtySeconds(t *testing.T) {
	t.Parallel()
	if fourTimes != fourWorkers {
		t.Errorf("the second guess is %d times the workers, want %d", fourTimes, fourWorkers)
	}
	if quietFor != quietLimit {
		t.Errorf("a run may be quiet for %v, want 30s", quietFor)
	}
}

// the two numbers the docs give for the bench and the progress line.
const (
	fourWorkers = 4                // defined here: the docs say four times the workers
	quietLimit  = 30 * time.Second // defined here: the docs say thirty seconds
)
