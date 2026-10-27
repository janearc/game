package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/mutants"
	"github.com/janearc/game/internal/repo"
)

// mutantArgs is what game mutants is told on its line.
type mutantArgs struct {
	only, again, report string
	workers             int
	timeout             time.Duration
	bench               bool
}

// mutantFlags reads game mutants' flags. a word that is not one is refused.
func mutantFlags(args []string) (mutantArgs, error) {
	var told mutantArgs
	fs := flag.NewFlagSet("mutants", flag.ExitOnError)
	told.whatToRun(fs)
	told.howToRun(fs)
	fs.Parse(args)
	if word, _, said := verbWord(fs.Args()); said {
		return told, fmt.Errorf(
			"mutants takes flags and nothing else; %q is not one",
			word,
		)
	}
	return told, nil
}

// whatToRun declares the flags that say which mutants run and where the
// report goes.
func (told *mutantArgs) whatToRun(fs *flag.FlagSet) {
	fs.StringVar(
		&told.only,
		"only",
		"",
		"mutate only where file:function matches this regular "+
			"expression",
	)
	fs.StringVar(
		&told.again,
		"again",
		"",
		"run only the mutants this earlier report says survived or "+
			"timed out; the lines a run printed as it went will do",
	)
	fs.StringVar(
		&told.report,
		"report",
		"",
		"write every mutant and what became of it to this file",
	)
}

// howToRun declares the flags that say how the mutants run.
func (told *mutantArgs) howToRun(fs *flag.FlagSet) {
	fs.IntVar(
		&told.workers,
		"workers",
		twoWorkers,
		"run this many mutants at once, each in a copy of the tree",
	)
	fs.DurationVar(
		&told.timeout,
		"timeout",
		suiteTimeout,
		"tests still running after this on a mutant have noticed it; "+
			"at least twice the unchanged suite's time, or the "+
			"run refuses to start and says what to give",
	)
	fs.BoolVar(
		&told.bench,
		"bench",
		false,
		"run each suite unchanged, say how long the whole run would "+
			"take, and stop",
	)
}

// the defaults a mutation run takes, as picked here: two mutants at
// once, each given twenty seconds, and the whole run tried again at
// four times the workers when the bench estimates it.
const (
	twoWorkers   = 2                // defined here: a laptop's share
	suiteTimeout = 20 * time.Second // defined here: a suite's slack
	fourTimes    = 4                // defined here: a second guess
)

// mutate is game mutants. it runs every suite unchanged first, then each
// mutant against its package's tests, and can be interrupted. it writes the
// report where asked and says the verdict.
func mutate(here repo.Repo, args []string) error {
	return mutateBy(here, args, diskMiner{}, os.Stdout)
}

// mutateBy is mutate, running the mutants with run and saying what
// became of them on out.
func mutateBy(here repo.Repo, args []string, run miner,
	out io.Writer) error {
	told, err := mutantFlags(args)
	if err != nil {
		return err
	}
	opts, err := mutantOptions(
		told.only, told.again, told.workers, told.timeout,
	)
	if err != nil {
		return err
	}
	if told.bench && told.report != "" {
		return errors.New(
			"--bench runs no mutants, so there is nothing for " +
				"--report to write",
		)
	}
	if told.bench {
		opts = opts.Benchmarking()
	}
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	// the first interrupt stops the run and lets it say what it found.
	// a second one ends the program.
	go stopOnce(ctx, stop)
	// the miner settles before it removes its copies, which takes long.
	// killed meanwhile the run has still left its report.
	// what is written then is not written again.
	tell := &settled{ctx: ctx, out: out, path: told.report}
	found, err := run.Run(ctx, here.Dir(), opts, tell.settle,
		decidedOn(out))
	stopped := errors.Is(err, context.Canceled)
	if err != nil && !stopped {
		return err
	}
	if told.bench {
		return tellBench(out, found, told.workers, stopped)
	}
	return tell.verdict(found, stopped)
}

// settled writes the report and tells the tally once, as the run settles.
// it holds the verdict for when the run returns.
type settled struct {
	ctx    context.Context
	out    io.Writer
	path   string
	done   bool
	result error
}

// settle writes and tells what the run found, as the run settles. the run
// is stopped when its context is done.
func (tell *settled) settle(found findings) {
	tell.done = true
	tell.result = writeAndTell(tell.out, found, tell.path,
		tell.ctx.Err() != nil)
}

// verdict is the error of the run. it is what settling gave. when the
// miner never settled it is what writing and telling what it returned
// gives.
func (tell *settled) verdict(found findings, stopped bool) error {
	if tell.done {
		return tell.result
	}
	return writeAndTell(tell.out, found, tell.path, stopped)
}

// writeAndTell writes the report where asked and then tells the tally.
// its error is the exit code, as tellMutants gives it.
func writeAndTell(out io.Writer, found findings, path string,
	stopped bool) error {
	if path != "" {
		text := reportText(found, stopped)
		err := os.WriteFile(path, []byte(text), ownerWrites)
		if err != nil {
			return err
		}
	}
	return tellMutants(out, found, stopped)
}

// reportText is the report as the file holds it, one mutant a line.
// the report of a stopped run says so on a line of its own before the
// mutants. it says how many were not reached and of how many.
func reportText(found findings, stopped bool) string {
	text := strings.Join(found.Lines(), "\n") + "\n"
	if !stopped {
		return text
	}
	outcomes := found.Outcomes()
	var unreached int
	for _, made := range outcomes {
		if made.state == "stopped" {
			unreached++
		}
	}
	if count.None(unreached) {
		return text
	}
	return fmt.Sprintf("partial run: %d of %d mutants not reached\n",
		unreached, len(outcomes)) + text
}

// stopOnce lets the signals go once the run is done or interrupted, so
// a second interrupt ends the program.
func stopOnce(ctx context.Context, stop context.CancelFunc) {
	<-ctx.Done()
	stop()
}

// ownerWrites is the mode a report is written with: the owner writes
// and everyone reads, defined as 0644 in sys/stat.h's bits.
const ownerWrites = 0o644

// quietFor is how long a run may go without a word, defined by the docs
// as thirty seconds.
const quietFor = 30 * time.Second

// mutantOptions reads flags into run options and writes progress to stderr.
func mutantOptions(
	only, again string,
	workers int,
	timeout time.Duration,
) (mutants.Options, error) {
	opts := mutants.Ask(workers, timeout).Nowed(time.Now).
		QuietAfter(quietFor).Writing(mutants.Disk())
	if !count.Positive(workers) {
		return opts, errors.New("--workers wants one or more")
	}
	if timeout <= noTime {
		return opts, errors.New("--timeout wants more than nothing")
	}
	if only != "" {
		re, err := regexp.Compile(only)
		if err != nil {
			return opts, fmt.Errorf("--only: %v", err)
		}
		opts = opts.OnlyWhere(re)
	}
	if again != "" {
		text, err := os.ReadFile(again)
		if err != nil {
			return opts, err
		}
		again := mutants.ParseAgain(string(text))
		opts = opts.OnlyAgain(again)
	}
	place, err := chosenPlace()
	if err != nil {
		return opts, err
	}
	return opts.Placing(place).Telling(toStderr), nil
}

// unmeasured is the folders Xcode does not instrument sources under.
// they are the per-user temp folders the system gives every login.
// the system names them /var/folders and /private/var/folders.
//
// tests of Xcode projects measure nothing in a copy under them and fail.
// this was found by running cmd/game's suite in copies there and
// elsewhere.
var unmeasured = []string{"/var/folders", "/private/var/folders"}

// measured says whether Xcode's coverage works on sources in the folder.
// it does in any folder but the unmeasured ones and an empty name.
// the name /var/foldersnot begins like one of them but is another folder.
func measured(dir string) bool {
	if dir == "" {
		return false
	}
	for _, folder := range unmeasured {
		if dir == folder || strings.HasPrefix(dir, folder+"/") {
			return false
		}
	}
	return true
}

// chosenPlace is the folder game mutants makes its copies of the tree in.
// it is the user's own temp folder setting when that names a folder where
// Xcode's coverage works. whoever set it chose.
//
// otherwise it is game/mutants in the user's cache folder
// (~/Library/Caches on macOS). the system's temp folder is one Xcode does
// not measure. the suite of a project with Xcode tests fails in a copy
// there.
//
// the run makes the folder. it makes a folder of its own in it for the
// copies. it removes that when it ends, stopped or not.
func chosenPlace() (string, error) {
	if tmp := os.Getenv("TMPDIR"); measured(tmp) {
		return tmp, nil
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("where to make the copies of the tree: "+
			"%v; set TMPDIR to a folder under your home", err)
	}
	return filepath.Join(cache, "game", "mutants"), nil
}

// tellBench prints what the unchanged suites cost and the most the run
// could take. it gives each package, then all of them. it fails when a
// suite could not be run. it fails for a stale line. it fails when the
// run was interrupted, and the figures are then partial.
func tellBench(
	out io.Writer, found findings, workers int, stopped bool,
) error {
	suites := append([]suiteCost{}, found.Suites()...)
	slices.SortFunc(suites, dearestFirst)
	fmt.Fprintln(
		out,
		"bench: each suite unchanged, and the most its mutants cost:",
	)
	for _, suite := range suites {
		fmt.Fprintf(
			out,
			"  %-28s %5d mutants  suite %7s  at most %s alone\n",
			suite.dir,
			suite.mutants,
			mutants.About(suite.took),
			mutants.About(suite.alone),
		)
	}
	broken := found.Broken()
	for _, one := range broken {
		fmt.Fprintf(out, "  broken: %s\n", one)
	}
	stale := found.Stale()
	tellStale(out, stale)
	if stopped {
		fmt.Fprintln(out,
			"bench: stopped before every suite ran; partial")
	}
	more := fourTimes * workers
	atAsked := mutants.About(found.Estimate(workers))
	atMore := mutants.About(found.Estimate(more))
	fmt.Fprintf(out, "bench: at most about %s at %d workers; %s at %d\n",
		atAsked, workers, atMore, more)
	return mutantsVerdict(len(broken), len(stale), nil, stopped)
}

// tellStale writes on out the lines of .mutants the source has moved on
// from.
func tellStale(out io.Writer, stale []string) {
	for _, name := range stale {
		fmt.Fprintf(
			out,
			"stale: .mutants names a change the source no longer "+
				"has: %s\n",
			name,
		)
	}
}

// tellMutants writes on out what could not be run, what survived and what
// timed out. then it writes the tally. its error is the exit code. any of
// those sets it, and so do a stale line and a stopped run.
func tellMutants(
	out io.Writer, found findings, stopped bool,
) error {
	for _, pkg := range found.Untested() {
		fmt.Fprintf(out, "untested: %s has source and no tests\n", pkg)
	}
	broken := found.Broken()
	for _, one := range broken {
		fmt.Fprintf(out, "broken: %s; nothing of it was run\n", one)
	}
	stale := found.Stale()
	tellStale(out, stale)
	outcomes := found.Outcomes()
	counts := map[string]int{}
	for _, made := range outcomes {
		counts[made.state]++
		if made.state == "broken" {
			fmt.Fprintf(out, "broken: %s: %s\n",
				made.name, made.failing)
		}
	}
	for _, made := range outcomes {
		if made.state == "survived" {
			fmt.Fprintf(out, "survived: %s\n", made.at)
		}
	}
	for _, made := range outcomes {
		if made.state == "timed out" {
			fmt.Fprintf(out, "timed out: %s\n", made.at)
		}
	}
	ran := len(outcomes) - counts["stopped"]
	fmt.Fprintf(
		out,
		"mutants: %d run; killed %d, survived %d, timed out %d, "+
			"equivalent %d, would not build %d\n",
		ran,
		counts["killed"],
		counts["survived"],
		counts["timed out"],
		counts["equivalent"],
		counts["unbuildable"],
	)
	return mutantsVerdict(len(broken), len(stale), counts, stopped)
}

// where is a mutant as the report places it: file, line and name.
func where(mutant mutants.Mutant) string {
	return fmt.Sprintf("%s:%d %s", mutant.File(), mutant.Line(),
		mutant.Name())
}

// mutantsVerdict returns the error with all reasons in one line. it is
// given the packages that could not be run and the stale lines of
// .mutants, as counts. it is given the outcomes tallied by state. stopped
// means the run was interrupted, which is a reason before anything ran.
func mutantsVerdict(broken, stale int, counts map[string]int,
	stopped bool) error {
	var reasons []string
	broken += counts["broken"]
	if count.Positive(broken) {
		reasons = append(reasons, fmt.Sprintf(
			"%d could not be run, so nothing is known there",
			broken,
		))
	}
	if survivors := counts["survived"]; count.Positive(survivors) {
		reasons = append(reasons, fmt.Sprintf(
			"%d survived: each wants a test that fails on it, "+
				"or a line in .mutants saying why the change "+
				"makes no difference",
			survivors,
		))
	}
	if late := counts["timed out"]; count.Positive(late) {
		reasons = append(reasons, fmt.Sprintf(
			"%d timed out, so nothing is known of them: run "+
				"--again on the report with fewer workers or "+
				"a longer --timeout",
			late,
		))
	}
	if count.Positive(stale) {
		reasons = append(reasons, fmt.Sprintf(
			"%d stale lines in .mutants", stale))
	}
	switch {
	case count.Positive(counts["stopped"]):
		reasons = append(reasons, fmt.Sprintf(
			"stopped with %d not run",
			counts["stopped"],
		))
	case stopped:
		reasons = append(reasons, "the run was interrupted")
	}
	if count.None(len(reasons)) {
		return nil
	}
	return errors.New(strings.Join(reasons, "; "))
}

// noTime is no duration at all.
var noTime time.Duration

// toStderr says one line on stderr, where the run's progress goes.
func toStderr(said string) {
	fmt.Fprintln(os.Stderr, said)
}

// dearestFirst orders suites by what their mutants would cost alone,
// the dearest first, as slices.SortFunc asks.
func dearestFirst(one, other suiteCost) int {
	return cmp.Compare(other.alone, one.alone)
}

// decidedOn says each decided survivor on out as it comes.
func decidedOn(out io.Writer) func(string) {
	return decidedLines{out: out}.say
}

// decidedLines is where decided lines are said.
type decidedLines struct {
	out io.Writer
}

// say says one line.
func (to decidedLines) say(line string) {
	fmt.Fprintln(to.out, line)
}
