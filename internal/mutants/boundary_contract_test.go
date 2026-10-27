package mutants_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

func TestOptionsStringIncludesSelectionAndBenchmark(t *testing.T) {
	t.Parallel()
	opts := mutants.Ask(count.One, time.Second).
		OnlyWhere(regexp.MustCompile("sample")).Benchmarking()
	if !strings.Contains(opts.String(), ", only sample") ||
		!strings.Contains(opts.String(), ", a bench") {
		t.Fatalf("options are %q", opts.String())
	}
}
