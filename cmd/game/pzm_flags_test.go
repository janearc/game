package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// stackParallel is how many members the test builds at once: one more
// than the default.
var stackParallel = count.Next(count.One)

// anti-tests: a value flag whose value is not a number or a duration, and a
// flag that belongs to another form of the verb, are refused with what was
// wrong, and nothing runs.
func TestVerbsRefuseAFlagValueTheyCannotRead(t *testing.T) {
	t.Parallel()
	work := repoAt(t.TempDir())
	for _, each := range []struct {
		verb  string
		do    verb
		args  []string
		wants string
	}{
		{"mutants", mutantsVerb, []string{"--workers", "many"},
			`--workers wants a number, not "many"`},
		{"mutants", mutantsVerb, []string{"--timeout", "later"},
			`--timeout wants a duration, not "later"`},
		{"mutants", mutantsVerb, []string{"stray"},
			`mutants takes flags and nothing else; "stray" is not one`},
		{"build", buildVerb, []string{"stack", "--parallel", "all"},
			`--parallel wants a number, not "all"`},
		{"build", buildVerb, []string{"--parallel", "2"},
			"belong to build stack"},
		{"build", buildVerb, []string{"docker", "--here"},
			"belong to build stack"},
		{"build", buildVerb, []string{"x", "--from", "road"},
			"belong to build stack"},
		{"release", releaseVerb, []string{"v1", "--publish"},
			"release: --publish is a flag it does not take"},
	} {
		err := viaPzm(t, each.verb, each.do, work, blankConfig(), each.args)
		if err == nil || !strings.Contains(err.Error(), each.wants) {
			t.Errorf("%s %q: %v, want %q", each.verb, each.args, err,
				each.wants)
		}
	}
}

// build stack takes its flags, and the defaults are the documented ones:
// members from the dist, one at a time, not already here.
func TestBuildStackFlagsAndTheirDefaults(t *testing.T) {
	t.Parallel()
	got, err := stackFor([]string{"stack"})
	if err != nil || *got != (stackAsk{from: "dist", parallel: count.One}) {
		t.Errorf("defaults: %+v %v", got, err)
	}
	got, err = stackFor([]string{"stack", "--from", "road", "--parallel", strconv.Itoa(stackParallel),
		"--here"})
	if err != nil || *got != (stackAsk{from: "road", parallel: stackParallel, here: true}) {
		t.Errorf("given: %+v %v", got, err)
	}
}

// anti-tests: pzm refuses a flag no form of the verb declares and a
// missing value, for every verb that takes flags, whichever order they
// come in.
func TestEveryVerbWithFlagsRefusesAnUnknownFlagAndAMissingValue(t *testing.T) {
	t.Parallel()
	for _, described := range pzmVerbs() {
		if len(described.flags) == count.Nothing {
			continue
		}
		if _, err := parsedCall(described.name, []string{"--nonesuch"}); err == nil ||
			!strings.Contains(err.Error(), "unknown flag: --nonesuch") {
			t.Errorf("%s --nonesuch: %v", described.name, err)
		}
		for _, flag := range described.flags {
			if flag.kind == pzmSwitch {
				continue
			}
			_, err := parsedCall(described.name, []string{"--" + flag.name})
			if err == nil || !strings.Contains(err.Error(),
				"flag needs an argument: --"+flag.name) {
				t.Errorf("%s --%s: %v", described.name, flag.name, err)
			}
		}
	}
}
