package mutants_test

import (
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

func TestAntiMutantsAgainIgnoresKilled(t *testing.T) {
	t.Parallel()
	line := "killed x.go:1 x.go f: x < 0 => x > 0\tTestX\n"
	if got := mutants.ParseAgain(line); len(got) != count.Nothing {
		t.Fatalf("killed mutant selected again: %v", got)
	}
}
