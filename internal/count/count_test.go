package count_test

import (
	"testing"

	"github.com/janearc/game/internal/count"
)

// the helpers say what a count is without a bare number at the asking.
func TestCountsAreSaid(t *testing.T) {
	t.Parallel()
	if !count.None(count.Nothing) || count.Positive(count.Nothing) {
		t.Error("nothing is not none, or is positive")
	}
	if count.None(count.One) || !count.Positive(count.One) {
		t.Error("one is none, or is not positive")
	}
	three := count.Next(count.Next(count.One))
	if count.Last(three) != count.Next(count.One) {
		t.Errorf("the last of three is at %d", count.Last(three))
	}
	if count.First != count.Nothing {
		t.Error("a list does not begin at nothing")
	}
	if count.One != len("x") || count.Origin != len("x") {
		t.Error("one and the origin are not one")
	}
}

func TestAntiCountNegativeIsNotPositive(t *testing.T) {
	t.Parallel()
	if count.Positive(count.Nothing - count.One) {
		t.Error("a negative count is positive")
	}
}
