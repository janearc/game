package main

import (
	"fmt"
	"os"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/lint"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/puzzlemouth/pzm"
)

// lintVerb is game lint. it lists every finding unless --count, tallying
// on the call's output, and saying on standard error that there are no
// rules. lint.Verdict decides the exit code.
func lintVerb(here repo.Repo, cfg config.Config, call pzm.Call) error {
	out, tallies := call.Out(), call.On("count")
	rules, err := lint.Describe(cfg.Lint())
	if err != nil {
		return err
	}
	if rules.Empty() {
		fmt.Fprintln(os.Stderr,
			"lint: hey buddy, i live to lint. where is "+
				"that config? add lint lines to "+
				"~/.config/game/config or .game",
		)
		return nil
	}
	found, err := rules.Run(here.Dir())
	if err != nil {
		return err
	}
	looks := map[string]int{}
	for _, finding := range found {
		if finding.Look() {
			looks[finding.Rule()]++
		}
		if !tallies {
			fmt.Fprintln(out, finding)
		}
	}
	for rule, many := range looks {
		fmt.Fprintf(out, "look: %d %s\n", many, rule)
	}
	// lint.Verdict decides the exit code. it is its own function so a
	// test can call it.
	if _, _, err := lint.Verdict(found); err != nil {
		return err
	}
	fmt.Fprintln(out, "lint: clean")
	return nil
}
