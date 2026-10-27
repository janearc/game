package mutants_test

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
)

// againProperty is the property that a report read back names exactly
// the mutants that survived or timed out, drawn with the generators
// given. the report is written the way a report file is: one mutant a
// line, its state, file:line and name, a tab, and the tests that failed.
type againProperty struct {
	file     *rapid.Generator[string]
	function *rapid.Generator[string]
	changed  *rapid.Generator[string]
	failing  *rapid.Generator[string]
	states   []string
	most     int
	highest  int
}

// check is the property as rapid runs it.
func (prop againProperty) check(t *rapid.T) {
	var lines []string
	want := map[string]bool{}
	many := rapid.IntRange(count.Nothing, prop.most).Draw(t, "mutants")
	for range many {
		line := rapid.IntRange(count.One, prop.highest).Draw(t, "line")
		file := prop.file.Draw(t, "file")
		name := fmt.Sprintf("%s %s: %s => %s", file,
			prop.function.Draw(t, "function"),
			prop.changed.Draw(t, "before"), prop.changed.Draw(t, "after"))
		state := rapid.SampledFrom(prop.states).Draw(t, "state")
		failing := prop.failing.Draw(t, "failing")
		lines = append(lines, fmt.Sprintf("%-10s %s:%d %s\t%s", state,
			file, line, name, failing))
		if state == "survived" || state == "timed out" {
			want[name] = true
		}
	}
	text := strings.Join(lines, "\n")
	got := mutants.ParseAgain(text)
	if !maps.Equal(got, want) {
		t.Fatalf("got %v\nwant %v\nfrom\n%s", got, want, text)
	}
}

// a report written out and read back names exactly the mutants that
// survived or timed out, whatever else is in it.
func TestParseAgainReadsBackTheSurvivors(t *testing.T) {
	t.Parallel()
	states := []string{
		"killed", "survived", "timed out", "equivalent", "unbuildable",
		"broken", "stopped",
	}
	// source text as a mutant shows it: no tab and no newline, since
	// the report is one mutant a line with a tab before the tests
	prop := againProperty{
		file:     rapid.StringMatching(`[a-z]+(/[a-z]+){0,3}\.go`),
		function: rapid.StringMatching(`[a-zA-Z][a-zA-Z0-9_.]{0,12}|package`),
		changed:  rapid.StringMatching(`[a-zA-Z0-9_+*/<>=!&|.()", -]{1,16}`),
		failing:  rapid.StringMatching(`([A-Za-z]{1,10}(,[A-Za-z]{1,10}){0,3})?`),
		states:   states,
		most:     int(held(t, "drawn-most-mutants")),
		highest:  int(held(t, "drawn-highest-line")),
	}
	rapid.Check(t, prop.check)
}

// the lines a run writes read back as the property reads them: the
// survivors of the fixture, by name, and nothing a test killed.
func TestARunsOwnLinesReadBackAsItsSurvivors(t *testing.T) {
	t.Parallel()
	report, err := runQuick(t, fixture(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	written := strings.Join(report.Lines(), "\n")
	got := mutants.ParseAgain(written)
	want := map[string]bool{}
	for _, outcome := range report.Outcomes() {
		if outcome.State() == "survived" {
			want[outcome.Mutant().Name()] = true
		}
	}
	if !maps.Equal(got, want) || len(want) == count.Nothing {
		t.Errorf("read back %v, want %v", got, want)
	}
}
