package main

import (
	"context"
	"time"

	"github.com/janearc/game/internal/mutants"
)

// finding is one mutant as game mutants tells it. state is what became of
// it. name is its name in .mutants. at is file, line and name. failing is
// the tests that failed on it, or why they could not run.
type finding struct {
	state, name, at, failing string
}

// suiteCost is one package's unchanged suite. alone is the time its
// mutants would take one after another.
type suiteCost struct {
	dir         string
	took, alone time.Duration
	mutants     int
}

// findings is what a run found, as far as the verb needs to say it.
type findings interface {
	// Lines is the report as written to a file, one mutant a line.
	Lines() []string
	// Untested is the packages with source and no tests.
	Untested() []string
	// Broken is the packages whose tests could not be run unchanged.
	Broken() []string
	// Stale is the lines of .mutants the source has moved on from.
	Stale() []string
	// Outcomes is what became of each mutant, in the run's order.
	Outcomes() []finding
	// Suites is each sound package's unchanged suite.
	Suites() []suiteCost
	// Estimate is the longest the run should take at that many workers.
	Estimate(workers int) time.Duration
}

// miner runs mutants over the tree at dir.
type miner interface {
	// Run runs the mutants. As soon as every mutant has an outcome it
	// hands what it found to settled. The copies of the tree are not yet
	// removed then.
	//
	// Removing four gigabytes takes long enough to be killed in. What
	// was found must be written down first. The findings it returns are
	// the same. They are returned even when the run was stopped part way.
	//
	// decided gets the report line of each survivor and timeout.
	Run(ctx context.Context, dir string, opts mutants.Options,
		settled func(findings), decided func(string)) (findings, error)
}

// diskMiner is the miner for production: mutants.Run on the real tree.
type diskMiner struct{}

// Run runs the mutants and hands back what they found. It hands the
// report to settled when it is settled.
func (diskMiner) Run(ctx context.Context, dir string,
	opts mutants.Options, settled func(findings),
	decided func(string)) (findings, error) {
	opts = opts.Settling(settling{settled: settled}.settle).
		Deciding(decided)
	found, err := mutants.Run(ctx, dir, opts)
	return seen{report: found}, err
}

// settling hands a settled report on as findings.
type settling struct {
	settled func(findings)
}

// settle hands the report to settled.
func (hand settling) settle(report mutants.Report) {
	hand.settled(seen{report: report})
}

// seen is a mutants.Report as findings.
type seen struct {
	report mutants.Report
}

// Lines is the report's lines.
func (one seen) Lines() []string { return one.report.Lines() }

// Untested is the report's untested packages.
func (one seen) Untested() []string { return one.report.Untested() }

// Broken is the report's broken packages.
func (one seen) Broken() []string { return one.report.Broken() }

// Stale is the report's stale lines.
func (one seen) Stale() []string { return one.report.Stale() }

// Estimate is the report's estimate at that many workers.
func (one seen) Estimate(workers int) time.Duration {
	return one.report.Estimate(workers)
}

// Outcomes is each outcome of the report as the verb tells it.
func (one seen) Outcomes() []finding {
	var out []finding
	for _, made := range one.report.Outcomes() {
		mutant := made.Mutant()
		out = append(out, finding{
			state:   made.State(),
			name:    mutant.Name(),
			at:      where(mutant),
			failing: made.Failing(),
		})
	}
	return out
}

// Suites is each suite of the report as the verb tells it.
func (one seen) Suites() []suiteCost {
	var out []suiteCost
	for _, suite := range one.report.Suites() {
		out = append(out, suiteCost{
			dir:     suite.Dir(),
			took:    suite.Took(),
			alone:   suite.Alone(),
			mutants: suite.Mutants(),
		})
	}
	return out
}
