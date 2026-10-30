package main

// game harden runs the harden rule in layers, cheapest first. The
// source rules lint.Hardened knows come first. Then the module's
// dependencies. Missing tools are findings, never passes. No go.mod is
// not a failure.

import (
	"fmt"
	"io"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/lint"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/puzzlemouth/pzm"
)

// hardenVerb is game harden.
func hardenVerb(here repo.Repo, _ config.Config, call pzm.Call) error {
	return harden(here.Dir(), call.Args(), run, call.Out())
}

// harden is hardenVerb in dir, running tools with by.
func harden(dir string, args []string, by runner, out io.Writer) error {
	if word, _, said := verbWord(args); said {
		return fmt.Errorf("harden: it takes no word, not %q", word)
	}
	return hardenAll(dir, by, out)
}

// verbWord is the verb's word and the arguments after it. The word
// comes first. said is false when there were no arguments.
func verbWord(args []string) (word string, rest []string, said bool) {
	if len(args) == noArgs {
		return "", nil, false
	}
	first := 0         // defined by go: a list begins at nothing
	after := first + 1 // fencepost, defined: the word's own follow it
	return args[first], args[after:], true
}

// noArgs is the length of an argument list with nothing in it.
var noArgs int

// hardenAll is game harden: the source and the modules.
func hardenAll(dir string, by runner, out io.Writer) error {
	found, err := hardenFinds(dir, by)
	if err != nil {
		return err
	}
	return sayHarden(out, found)
}

// hardenFinds gathers every finding the bare pass produces: the source
// rules first, then the layers beyond them.
func hardenFinds(dir string, by runner) ([]lint.Finding, error) {
	src, err := lint.Hardened(dir)
	if err != nil {
		return nil, err
	}
	beyond, err := hardenBeyondSource(dir, by)
	if err != nil {
		return nil, err
	}
	return append(src, beyond...), nil
}

// hardenBeyondSource is every harden layer but the source rules: the Go
// analyzers over the module. release and verify add it after the lint has
// run; the lint already holds the source rules.
func hardenBeyondSource(dir string, by runner) ([]lint.Finding, error) {
	return hardenGo(dir, by)
}

// hardenOf is the findings of the harden rule alone, out of a lint's.
func hardenOf(found []lint.Finding) []lint.Finding {
	var out []lint.Finding
	for _, finding := range found {
		if finding.Rule() == rule {
			out = append(out, finding)
		}
	}
	return out
}

// none says there are no findings in a list.
func none(found []lint.Finding) bool {
	return len(found) == noArgs
}

// sayHarden prints every finding on out and fails with the count, or says
// the tree is clean.
func sayHarden(out io.Writer, found []lint.Finding) error {
	for _, finding := range found {
		fmt.Fprintln(out, finding)
	}
	if !none(found) {
		return fmt.Errorf("harden: %d to fix", len(found))
	}
	fmt.Fprintln(out, "harden: clean")
	return nil
}
