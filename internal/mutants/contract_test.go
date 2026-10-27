package mutants_test

import (
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

func TestRepeatedSelectionIsReportedEvenWhenEmpty(t *testing.T) {
	t.Parallel()
	options := mutants.Ask(count.One, time.Second).OnlyAgain(map[string]bool{})
	if !strings.Contains(options.String(), ", 0 again") {
		t.Fatalf("empty repeat selection hidden: %s", options)
	}
	options = options.OnlyAgain(map[string]bool{"chosen": true})
	if !strings.Contains(options.String(), ", 1 again") {
		t.Fatalf("repeat selection count lost: %s", options)
	}
}
