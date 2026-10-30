package mutants

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/procgroup"
	"github.com/janearc/game/internal/toolchain"
)

// Options says how a run goes. Ask makes them with what every run has;
// the rest is said by a method each.
type Options struct {
	// only, when set, mutates only where file:function matches it.
	only *regexp.Regexp
	// again, when set, runs only the mutants named in it. ParseAgain reads
	// them from an earlier report.
	again map[string]bool
	// workers is how many mutants run at once, each in a copy of the
	// tree of its own.
	workers int
	// timeout is how long a mutant's tests may run before it is taken as
	// killed. each package's unchanged suite has five times as long before
	// it counts as broken.
	timeout time.Duration
	// bench runs each suite unchanged, times it, and stops. the report then
	// holds the suites and no outcomes, and Estimate reads them.
	bench bool
	// tell, when set, is told what the run is doing, in sentences for a
	// person.
	tell func(string)
	// quietFor is how long the run may go without a word.
	quietFor time.Duration
	// now is the clock the run reads: the caller owns it.
	now func() time.Time
	// files writes a mutant into a tree and puts the original back: the
	// caller owns the disk.
	files Files
	// settled is handed the report if it is set. This is once every mutant
	// has an outcome and before the copies of the tree are removed.
	settled func(Report)
	// decided gets each survivor's report line if it is set.
	decided func(string)
	// place is the folder the copies of the tree are made in if it is set.
	// The caller chooses it. Where the copies are can decide whether the
	// tests pass in them.
	place string
}

// Ask is a run of that many workers, each mutant's tests given that
// long.
func Ask(workers int, timeout time.Duration) Options {
	return Options{workers: workers, timeout: timeout}
}

// OnlyWhere is the options mutating only where file:function matches.
func (asked Options) OnlyWhere(only *regexp.Regexp) Options {
	asked.only = only
	return asked
}

// OnlyAgain is the options running only the mutants named, as
// ParseAgain read them from an earlier report.
func (asked Options) OnlyAgain(again map[string]bool) Options {
	asked.again = again
	return asked
}

// Benchmarking is the options running each suite unchanged, timing
// it, and stopping there.
func (asked Options) Benchmarking() Options {
	asked.bench = true
	return asked
}

// QuietAfter is the options saying something after that long quiet.
func (asked Options) QuietAfter(quietFor time.Duration) Options {
	asked.quietFor = quietFor
	return asked
}

// Nowed is the options reading the clock given.
func (asked Options) Nowed(now func() time.Time) Options {
	asked.now = now
	return asked
}

// Writing is the options changing files with the files given.
func (asked Options) Writing(files Files) Options {
	asked.files = files
	return asked
}

// Settling is the options handing the finished report to settled. It is
// handed over once every mutant has an outcome. This is before the copies
// of the tree are removed. Whoever must write the report down does so
// there. Then a slow removal cannot cost the report.
func (asked Options) Settling(settled func(Report)) Options {
	asked.settled = settled
	return asked
}

// Placing is the options making the copies of the tree in that folder,
// which the run makes if it is not there. Each run's copies are in a folder
// of their own in it, and the run removes that when it ends.
func (asked Options) Placing(place string) Options {
	asked.place = place
	return asked
}

// Deciding is the options giving decided the line Lines has for each
// survivor and timeout. ParseAgain reads it back. Killed mutants are
// not given.
func (asked Options) Deciding(decided func(string)) Options {
	asked.decided = decided
	return asked
}

// settle hands the report to whoever asked to see it settled, if anyone
// did.
func (asked Options) settle(found Report) {
	if asked.settled != nil {
		asked.settled(found)
	}
}

// Telling is the options telling someone what the run is doing, in
// sentences for a person.
func (asked Options) Telling(tell func(string)) Options {
	asked.tell = tell
	return asked
}

// String is the options as the run says them when it starts: the
// workers, the time a mutant has, and what narrows or shortens it.
func (asked Options) String() string {
	said := fmt.Sprintf("%d workers, %s a mutant", asked.workers,
		asked.timeout)
	if asked.only != nil {
		said += ", only " + asked.only.String()
	}
	if asked.again != nil {
		said += fmt.Sprintf(", %d again", len(asked.again))
	}
	if asked.place != "" {
		said += ", copies in " + asked.place
	}
	if asked.bench {
		said += ", a bench"
	}
	return said
}

// say tells the person, when anyone is listening.
func (asked Options) say(format string, args ...any) {
	if asked.tell != nil {
		asked.tell(fmt.Sprintf(format, args...))
	}
}

// Outcome is what became of one mutant.
type Outcome struct {
	mutant Mutant
	// state is killed, survived, timed out, equivalent, unbuildable,
	// broken or stopped.
	//
	// timed out means the tests ran past the bound with none failing, so
	// nothing is known of it. broken means the tests could not be run on
	// it, and failing says why. stopped means the run was cancelled
	// before it.
	state string
	// failing is the tests that failed on it first, comma separated.
	failing string
}

// NewOutcome is what became of this mutant: its state, and the tests
// that failed on it or why they could not run.
func NewOutcome(mutant Mutant, state, failing string) Outcome {
	return Outcome{mutant: mutant, state: state, failing: failing}
}

// Mutant is the mutant the outcome is of.
func (made Outcome) Mutant() Mutant { return made.mutant }

// State is what became of the mutant: killed, survived, timed out,
// equivalent, unbuildable, broken or stopped.
func (made Outcome) State() string { return made.state }

// Failing is the tests that failed on it first, comma separated, or
// why the tests could not run when it is broken.
func (made Outcome) Failing() string { return made.failing }

// Suite is one package's tests run unchanged. it holds how long they took
// and how many mutants wait on them.
type Suite struct {
	dir     string
	took    time.Duration
	mutants int
}

// NewSuite is the unchanged suite of the package in dir, which took that
// long and has that many mutants waiting on it.
func NewSuite(dir string, took time.Duration, mutants int) Suite {
	return Suite{dir: dir, took: took, mutants: mutants}
}

// Dir is the package's directory from the root.
func (suite Suite) Dir() string { return suite.dir }

// Took is how long the unchanged suite ran.
func (suite Suite) Took() time.Duration { return suite.took }

// Mutants is how many mutants wait on the suite.
func (suite Suite) Mutants() int { return suite.mutants }

// Alone is how long the suite's mutants would take one after another.
func (suite Suite) Alone() time.Duration {
	return suite.took * time.Duration(suite.mutants)
}

// Report is what a run found.
type Report struct {
	outcomes []Outcome
	// untested is packages with source but no tests. nothing was run there.
	untested []string
	// broken is the packages whose tests fail, or do not build, before any
	// change, and the modules go could not list, with what go said. their
	// mutants were not run.
	broken []string
	// stale is the names in .mutants the source no longer has.
	stale []string
	// suites is each sound package's unchanged suite, timed, with the
	// count of its mutants.
	suites []Suite
}

// NewReport is a report of these outcomes, the packages with source and
// no tests, the packages whose tests could not be run, the stale lines of
// .mutants and the suites.
func NewReport(outcomes []Outcome, untested, broken, stale []string,
	suites []Suite) Report {
	return Report{outcomes: outcomes, untested: untested, broken: broken,
		stale: stale, suites: suites}
}

// Outcomes is what became of each mutant, in the run's order.
func (found Report) Outcomes() []Outcome { return found.outcomes }

// Untested is the packages with source but no tests.
func (found Report) Untested() []string { return found.untested }

// Broken is the packages whose tests fail before any change, and the
// modules go could not list, with what go said.
func (found Report) Broken() []string { return found.broken }

// Stale is the names in .mutants the source no longer has.
func (found Report) Stale() []string { return found.stale }

// Suites is each sound package's unchanged suite, timed.
func (found Report) Suites() []Suite { return found.suites }

// Estimate is the longest the run should take with that many workers. it
// counts every mutant as running its package's whole unchanged suite. a
// bound, not a promise.
func (found Report) Estimate(workers int) time.Duration {
	var total time.Duration
	for _, suite := range found.suites {
		total += suite.Alone()
	}
	return total / time.Duration(max(count.One, workers))
}

// tenth is how finely About rounds, defined as a tenth of a second.
const tenth = 100 * time.Millisecond

// minuteOfHour is how many minutes an hour holds, defined as sixty.
const minuteOfHour = 60

// About formats a duration in hours, minutes, or seconds.
func About(took time.Duration) string {
	took = took.Round(tenth)
	switch {
	case took >= time.Hour:
		hours := int(took.Hours())
		minutes := int(took.Minutes()) % minuteOfHour
		return fmt.Sprintf("%dh%02dm", hours, minutes)
	case took >= time.Minute:
		minutes := int(took.Minutes())
		seconds := int(took.Seconds()) % minuteOfHour
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	return fmt.Sprintf("%.1fs", took.Seconds())
}

// Lines is the report as written to a file, one mutant a line: its
// state, where it is, its name, and the tests that failed on it.
// ParseAgain reads it back.
func (found Report) Lines() []string {
	var out []string
	for _, made := range found.outcomes {
		out = append(out, made.line())
	}
	return out
}

// line is an outcome as the report file carries it.
func (made Outcome) line() string {
	return fmt.Sprintf(
		"%-10s %s:%d %s\t%s",
		made.state,
		made.mutant.file,
		made.mutant.line,
		made.mutant.Name(),
		made.failing,
	)
}

// ParseAgain reads an earlier report and returns the names of the
// mutants that survived or timed out, for Options.Again. both are
// unknown until they run again.
func ParseAgain(text string) map[string]bool {
	again := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		rest, found := strings.CutPrefix(line, "survived")
		if !found {
			rest, found = strings.CutPrefix(line, "timed out")
		}
		if !found {
			continue
		}
		// the name runs to the tab, whitespace and all
		rest, _, _ = strings.Cut(strings.TrimLeft(rest, " "), "\t")
		// file:line comes before the name; the name starts after it
		_, name, _ := strings.Cut(rest, " ")
		again[name] = true
	}
	return again
}

// pkg is one package go would build. it has its directory from the root,
// its module, the files go compiles here, and whether it has tests.
type pkg struct {
	dir     string
	module  string
	sources []string
	tested  bool
}

// work is one mutant and the package it is in.
type work struct {
	mutant Mutant
	in     pkg
}

// gather is every mutant the options want, over every tested package.
// packages with source but no tests are untested.
func gather(root string, pkgs []pkg, opts Options) ([]work, []string, error) {
	var all []work
	var untested []string
	for _, in := range pkgs {
		if !in.tested {
			untested = append(untested, in.dir)
			opts.say(
				"untested: %s has source and no tests; "+
					"nothing will run there",
				in.dir,
			)
			continue
		}
		for _, source := range in.sources {
			found, err := of(root, source)
			if err != nil {
				built := runtime.Version()
				explained := toolchain.Explain(root, built, err)
				return nil, nil, explained
			}
			for _, mutant := range found {
				if opts.wants(mutant) {
					all = append(all, work{mutant, in})
				}
			}
		}
	}
	return all, untested, nil
}

// tellPlanned says how many mutants there are and in how many packages.
func tellPlanned(opts Options, all []work) {
	selected := map[string]bool{}
	for _, job := range all {
		selected[job.in.dir] = true
	}
	noun := "packages"
	if len(selected) == count.One {
		noun = "package"
	}
	opts.say(
		"mutants: %d in %d %s at %s; each suite runs unchanged first",
		len(all),
		len(selected),
		noun,
		opts,
	)
}

// noDuration is no time at all.
var noDuration time.Duration

// Run is the mutation run over the tree at root. each mutant the options
// want runs against its package's tests. survivors that .mutants names
// are marked equivalent, and names it has that the source does not are
// stale.
func Run(ctx context.Context, root string, opts Options) (*Report, error) {
	if opts.now == nil || opts.files == nil || opts.quietFor <= noDuration {
		return nil, errors.New("a run needs Nowed, QuietAfter " +
			"and Writing: the caller owns the clock and the disk")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	equivalents, err := equivalents(root)
	if err != nil {
		return nil, err
	}
	pkgs, broken, err := packages(root)
	if err != nil {
		return nil, err
	}
	all, untested, err := gather(root, pkgs, opts)
	if err != nil {
		return nil, err
	}
	report := NewReport(nil, untested, broken, nil, nil)
	tellPlanned(opts, all)
	done := runAll(ctx, root, all, opts)
	// the copies go when Run returns, after the report has been handed over
	defer os.RemoveAll(done.temp)
	report.outcomes, report.suites = done.outcomes, done.suites
	report.broken = append(report.broken, done.broken...)
	if done.refused != nil {
		return &report, done.refused
	}
	for at := range report.outcomes {
		made := &report.outcomes[at]
		if made.state == "survived" &&
			equivalents[made.mutant.Name()] != "" {
			made.state = "equivalent"
		}
	}
	if opts.only == nil && opts.again == nil {
		report.stale = stale(equivalents, all)
	}
	if !opts.bench {
		opts.settle(report)
	}
	return &report, ctx.Err()
}

// stale finds names in equivalents not used by any mutant.
func stale(equivalents map[string]string, all []work) []string {
	seen := map[string]bool{}
	for _, job := range all {
		seen[job.mutant.Name()] = true
	}
	var out []string
	for name := range equivalents {
		if !seen[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// wants says whether the options include a mutant.
func (asked Options) wants(mutant Mutant) bool {
	if asked.only != nil && !asked.only.MatchString(mutant.where()) {
		return false
	}
	return asked.again == nil || asked.again[mutant.Name()]
}

// skipped is whether the search for modules passes over a directory, as go
// does, and over vendored trees and bin at the root.
func skipped(root, path, name string) bool {
	switch name {
	case ".git", "testdata", "vendor", "node_modules":
		return true
	case "bin":
		return filepath.Dir(path) == root
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// modules is the directories under root holding a go.mod, from the root.
func modules(root string) ([]string, error) {
	finding := &finding{root: root}
	return finding.out, filepath.WalkDir(root, finding.visit)
}

// finding is a walk of a tree for its modules, and the ones found so
// far, from the root.
type finding struct {
	root string
	out  []string
}

// visit reads one entry: a directory go would skip is skipped, and a
// go.mod names its directory a module.
func (found *finding) visit(path string, entry fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	if entry.IsDir() {
		skip := path != found.root &&
			skipped(found.root, path, entry.Name())
		if skip {
			return filepath.SkipDir
		}
		return nil
	}
	if entry.Name() == "go.mod" {
		// the path is under the root the walk began at, so Rel can
		rel, _ := filepath.Rel(found.root, filepath.Dir(path))
		found.out = append(found.out, rel)
	}
	return nil
}

// packages finds every module under root and asks go for the packages in
// each. whatever go cannot list is returned as broken, with what go said.
func packages(root string) ([]pkg, []string, error) {
	found, err := modules(root)
	if err != nil {
		return nil, nil, err
	}
	var pkgs []pkg
	var broken []string
	for _, module := range found {
		listed, said := list(root, module)
		pkgs = append(pkgs, listed...)
		broken = append(broken, said...)
	}
	return pkgs, broken, nil
}

// listFormat is the format for go list output. it shows dir, go files,
// test count, and error if any, tab apart, in the order the listed
// fields name.
const listFormat = "{{.Dir}}\t{{join .GoFiles \" \"}}\t" +
	"{{len .TestGoFiles}}\t{{len .XTestGoFiles}}\t" +
	"{{if .Error}}{{printf \"%q\" .Error.Err}}{{end}}"

// the fields of one go list line, as listFormat defines them.
const (
	dirField     = 0    // defined by listFormat: .Dir
	sourcesField = 1    // defined by listFormat: the go files
	testsField   = 2    // defined by listFormat: how many test files
	xtestsField  = 3    // defined by listFormat: how many external
	errorField   = 4    // defined by listFormat: the error, quoted
	listedFields = 5    // defined by listFormat: five, tab apart
	noTestFiles  = "00" // defined here: no test files of either kind
)

// listed is one package as go list printed it, read by its fields.
type listed struct {
	fields []string
}

// field is one of the line's fields by its place.
func (line listed) field(at int) string { return line.fields[at] }

// list finds packages in a module, without the workspace. broken lines
// are modules go cannot list or packages with errors.
func list(root, module string) (pkgs []pkg, broken []string) {
	cmd := exec.Command("go", "list", "-e", "-f", listFormat, "./...")
	cmd.Dir = filepath.Join(root, module)
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		said := err.Error()
		if errors.As(err, &exit) {
			said = firstLine(exit.Stderr, err)
		}
		return nil, []string{module + ": go list: " + said}
	}
	for text := range strings.SplitSeq(string(out), "\n") {
		line := listed{fields: strings.Split(text, "\t")}
		if len(line.fields) != listedFields {
			// the lines between packages, and the empty last one
			continue
		}
		dir, err := relative(root, line.field(dirField))
		if err != nil {
			said := line.field(dirField) + ": " + err.Error()
			broken = append(broken, said)
			continue
		}
		if quoted := line.field(errorField); quoted != "" {
			// quoted, so a message with a line break or a tab in it
			// is still one field
			said, err := strconv.Unquote(quoted)
			if err != nil {
				said = quoted
			}
			broken = append(broken, dir+": "+said)
			continue
		}
		if line.field(sourcesField) == "" {
			// nothing go compiles here, so nothing to change
			continue
		}
		testFiles := line.field(testsField) + line.field(xtestsField)
		found := pkg{dir: dir, module: module,
			tested: testFiles != noTestFiles}
		for _, name := range strings.Fields(line.field(sourcesField)) {
			source := filepath.Join(dir, name)
			found.sources = append(found.sources, source)
		}
		pkgs = append(pkgs, found)
	}
	return pkgs, broken
}

// relative is dir from root, with symlinks resolved.
func relative(root, dir string) (string, error) {
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	return filepath.Rel(root, dir)
}

// copyTree copies the repository into dir, leaving out .git and bin at the
// root and keeping each file's permissions. symbolic links are not copied.
// The copy stops at the next entry once ctx is cancelled.
func copyTree(ctx context.Context, root, dir string) error {
	copying := copying{ctx: ctx, root: root, dir: dir}
	return filepath.WalkDir(root, copying.visit)
}

// copying is a copy of the tree at root into dir.
type copying struct {
	ctx       context.Context
	root, dir string
}

// folderMode is the mode a copied directory is made with: the owner
// writes, everyone reads and enters, defined as 0755 in sys/stat.h's
// bits.
const folderMode = 0o755

// visit copies one entry: a directory is made, a regular file is
// written with its permissions, and anything else is left.
func (copy copying) visit(path string, entry fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	if err := copy.ctx.Err(); err != nil {
		return err
	}
	// the path is under the root the walk began at, so there is a way
	// from one to the other
	rel, _ := filepath.Rel(copy.root, path)
	to := filepath.Join(copy.dir, rel)
	if entry.IsDir() {
		atRoot := filepath.Dir(path) == copy.root
		if atRoot && (entry.Name() == ".git" || entry.Name() == "bin") {
			return filepath.SkipDir
		}
		return os.MkdirAll(to, folderMode)
	}
	if !entry.Type().IsRegular() {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// a file just read has a mode; if it is gone it keeps the owner's
	perm := os.FileMode(ownerWrites)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	return os.WriteFile(to, content, perm)
}

// copyTrees makes one tree per worker in a temp dir. caller removes it.
func copyTrees(ctx context.Context, root string, workers int,
	place string) (string, []string, error) {
	// the place is made if it was told and is not there
	if place != "" {
		if err := os.MkdirAll(place, folderMode); err != nil {
			return "", nil, err
		}
	}
	temp, err := os.MkdirTemp(place, "game-mutants-")
	if err != nil {
		return "", nil, err
	}
	trees := make([]string, max(count.One, workers))
	for i := range trees {
		trees[i] = filepath.Join(temp, fmt.Sprintf("worker%d", i))
		err := copyTree(ctx, root, trees[i])
		if err == nil {
			err = os.MkdirAll(scratchOf(trees[i]), folderMode)
		}
		if err != nil {
			os.RemoveAll(temp)
			return "", nil, err
		}
	}
	return temp, trees, nil
}

// scratchOf is the temp folder a worker's go test is given: a folder
// beside its tree, inside the folder of the run's copies. What go and the
// tests leave there when they are killed goes with the copies.
//
// It's beside the tree and not in it, so no mutant sees it.
func scratchOf(tree string) string {
	return filepath.Join(filepath.Dir(tree), "tmp-"+filepath.Base(tree))
}

// ran is what runAll made of the mutants. It holds their outcomes and
// the packages that could not be run. It holds the suites too. temp is
// the folder holding the copies of the tree, which the caller removes.
// It is empty when there are none.
//
// refused is why the run did not go on to the mutants, when it did not.
type ran struct {
	outcomes []Outcome
	broken   []string
	suites   []Suite
	temp     string
	refused  error
}

// suiteMargin is how many times a suite's own time a mutant's timeout
// must be. The suite is timed alone in the first copy.
//
// Mutants' tests run beside the other workers' on a machine that is
// doing other things. The package is built again for each. So they take
// longer than the suite did.
//
// Twice is the least that has held. With six workers a suite of 54s was
// held by a timeout of 120s. On a suite of 1m09s a timeout of 60s timed
// out 273 mutants the tests had not caught.
//
// It's defined here: a judgement from those runs, not a rule of go test.
const suiteMargin = 2

// tooShort is the refusal to start when the timeout is less than
// suiteMargin times the time of the dearest suite. It is nothing when
// the timeout is enough.
//
// Mutants that time out are not known to be killed. Each one the tests
// do not catch would be lost to a timeout that cannot be met.
func tooShort(suites []Suite, timeout time.Duration) error {
	var dear Suite
	for _, suite := range suites {
		if suite.took > dear.took {
			dear = suite
		}
	}
	needed := suiteMargin * dear.took
	if timeout >= needed {
		return nil
	}
	// the timeout to use is said in whole seconds, and never less
	whole := needed.Truncate(time.Second)
	if whole < needed {
		whole += time.Second
	}
	return fmt.Errorf(
		"the unchanged suite of %s took %s and --timeout is %s; "+
			"mutants run beside each other take longer than the "+
			"suite alone, so those the tests do not catch would "+
			"time out and could hide survivors; use --timeout %s "+
			"or more",
		dear.dir,
		About(dear.took),
		About(timeout),
		whole,
	)
}

// unreached is the outcome of each mutant of all as a run stopped before it.
func unreached(all []work) []Outcome {
	var out []Outcome
	for _, job := range all {
		out = append(out, NewOutcome(job.mutant, "stopped", ""))
	}
	return out
}

// runAll runs each suite unchanged in the first tree and says what the
// whole run would cost. Unless this is a bench it then runs the mutants
// of the sound packages over every tree. If the run is stopped while the
// trees are copied then nothing has run. Its mutants are stopped.
func runAll(
	ctx context.Context,
	root string,
	all []work,
	opts Options,
) ran {
	temp, trees, err := copyTrees(ctx, root, opts.workers, opts.place)
	if err != nil && ctx.Err() != nil {
		if opts.bench {
			return ran{}
		}
		return ran{outcomes: unreached(all)}
	}
	if err != nil {
		return ran{broken: []string{"copying the tree: " + err.Error()}}
	}
	first := trees[count.First]
	kept, broken, rest, suites := sound(ctx, first, all, opts)
	if count.Positive(len(kept)) && ctx.Err() == nil {
		bound := Report{suites: suites}.Estimate(len(trees))
		opts.say(
			"estimate: at most about %s, %d mutants at %d workers",
			About(bound),
			len(kept),
			len(trees),
		)
	}
	if opts.bench {
		return ran{broken: broken, suites: suites, temp: temp}
	}
	if refused := tooShort(suites, opts.timeout); refused != nil {
		return ran{broken: broken, suites: suites, temp: temp,
			refused: refused}
	}
	return ran{
		outcomes: runKept(ctx, trees, kept, rest, opts),
		broken:   broken,
		suites:   suites,
		temp:     temp,
	}
}

// runKept runs the kept mutants over the trees and returns every outcome in
// order. mutants never reached are marked stopped.
func runKept(
	ctx context.Context,
	trees []string,
	kept, rest []work,
	opts Options,
) []Outcome {
	outcomes := make([]Outcome, len(kept), len(kept)+len(rest))
	for i, job := range kept {
		outcomes[i] = NewOutcome(job.mutant, "stopped", "")
	}
	for _, job := range rest {
		unreached := NewOutcome(job.mutant, "stopped", "")
		outcomes = append(outcomes, unreached)
	}
	running := &running{
		ctx: ctx, kept: kept, outcomes: outcomes, opts: opts,
		next: make(chan int), done: make(chan int),
	}
	for _, tree := range trees {
		running.group.Add(count.One)
		go running.worker(tree)
	}
	go running.feed()
	go running.finish()
	follow(running.done, outcomes, len(kept), opts)
	return outcomes
}

// running is the mutants being run over the trees: the ones to run,
// where each outcome goes, the channel the next index comes on and the
// channel a finished index goes out on.
type running struct {
	ctx      context.Context
	kept     []work
	outcomes []Outcome
	opts     Options
	next     chan int
	done     chan int
	group    sync.WaitGroup
}

// worker runs each mutant it is handed in one tree, until next closes.
func (run *running) worker(tree string) {
	defer run.group.Done()
	for index := range run.next {
		job, bound := run.kept[index], run.opts.timeout
		files := run.opts.files
		run.outcomes[index] = runOne(run.ctx, tree, job, bound, files)
		run.done <- index
	}
}

// feed hands out each mutant's index in order, and stops when the run
// is cancelled.
func (run *running) feed() {
	defer close(run.next)
	for i := range run.kept {
		select {
		case <-run.ctx.Done():
			return
		case run.next <- i:
		}
	}
}

// finish closes done once every worker has.
func (run *running) finish() {
	run.group.Wait()
	close(run.done)
}

// the bounds the docs give: an unchanged suite has five times a mutant's
// timeout, a build as long again as its tests, and go test five seconds
// to let go of its output once it is stopped.
const (
	suiteTimes = 5               // defined by the docs: five times
	buildTimes = 2               // defined by the docs: as long again
	waitDelay  = 5 * time.Second // defined by the docs: five seconds
)

// everyFifty is how often follow speaks by count: every fifty mutants
// done, defined by the docs.
const everyFifty = 50

// follow reads the finished mutants as they come and says how far the
// run has got, every fifty or after thirty quiet seconds, with the time
// so far and about how much is left.
func follow(done <-chan int, outcomes []Outcome, total int, opts Options) {
	tick := time.NewTicker(opts.quietFor)
	defer tick.Stop()
	progress := &progress{opts: opts, total: total,
		counts: map[string]int{}, tick: tick}
	progress.start = opts.now()
	progress.last = progress.start
	for {
		select {
		case index, more := <-done:
			if !more {
				return
			}
			progress.finished++
			progress.counts[outcomes[index].state]++
			opts.tellDecided(outcomes[index])
			if progress.finished%everyFifty == count.Nothing &&
				progress.finished < total {
				progress.report()
			}
		case <-tick.C:
			quiet := opts.now().Sub(progress.last)
			if quiet >= opts.quietFor && progress.finished < total {
				progress.report()
			}
		}
	}
}

// tellDecided gives decided the line of a survivor or timeout.
func (asked Options) tellDecided(made Outcome) {
	if asked.decided == nil {
		return
	}
	if made.state == "survived" || made.state == "timed out" {
		asked.decided(made.line())
	}
}

// progress is how far a run has got: how many are done, by state, and
// when it started and last spoke.
type progress struct {
	opts     Options
	total    int
	finished int
	counts   map[string]int
	start    time.Time
	last     time.Time
	tick     *time.Ticker
}

// report says how far the run has got, and about how much is left at
// the pace so far.
func (far *progress) report() {
	elapsed := far.opts.now().Sub(far.start)
	if count.None(far.finished) {
		far.opts.say(
			"mutants: none of %d done yet, %s so far",
			far.total,
			About(elapsed),
		)
	} else {
		left := elapsed / time.Duration(far.finished)
		left *= time.Duration(far.total - far.finished)
		far.opts.say(
			"mutants: %d of %d done: killed %d, "+
				"survived %d%s; %s so far, "+
				"about %s left",
			far.finished,
			far.total,
			far.counts["killed"],
			far.counts["survived"],
			lateOnes(far.counts["timed out"]),
			About(elapsed),
			About(left),
		)
	}
	far.last = far.opts.now()
	far.tick.Reset(far.opts.quietFor)
}

// sound runs each package's suite unchanged in a copy, with five times a
// mutant's time, and keeps the mutants of the packages whose suites pass.
// broken lists the packages whose suites do not. a cancelled ctx returns the
// mutants not yet judged.
func sound(
	ctx context.Context,
	tree string,
	all []work,
	opts Options,
) (kept []work, broken []string, rest []work, suites []Suite) {
	judging := &judging{ctx: ctx, tree: tree, opts: opts,
		passed: map[string]bool{}, waiting: map[string]int{}}
	for i, job := range all {
		if _, tried := judging.passed[job.in.dir]; !tried {
			if cancelled := judging.suite(job.in); cancelled {
				return judging.kept, judging.broken, all[i:],
					judging.counted()
			}
		}
		if judging.passed[job.in.dir] {
			judging.kept = append(judging.kept, job)
			judging.waiting[job.in.dir]++
		}
	}
	return judging.kept, judging.broken, nil, judging.counted()
}

// judging is the unchanged suites being run: which packages passed,
// how many mutants wait on each, and what is kept and broken so far.
type judging struct {
	ctx     context.Context
	tree    string
	opts    Options
	passed  map[string]bool
	waiting map[string]int
	kept    []work
	broken  []string
	suites  []Suite
}

// suite runs one package's unchanged tests and says whether the run
// was cancelled under it.
func (judge *judging) suite(in pkg) bool {
	began := judge.opts.now()
	bound := suiteTimes * judge.opts.timeout
	output, err, _ := test(judge.ctx, judge.tree, in, bound)
	if judge.ctx.Err() != nil {
		return true
	}
	judge.passed[in.dir] = err == nil
	if err != nil {
		said := firstLine(output, err)
		judge.broken = append(judge.broken, in.dir+": "+said)
		judge.opts.say("unchanged: %s fails: %s", in.dir, said)
		return false
	}
	took := judge.opts.now().Sub(began)
	judge.suites = append(judge.suites, NewSuite(in.dir, took, 0))
	judge.opts.say("unchanged: %s passes, %s", in.dir, About(took))
	return false
}

// counted gives each suite the number of mutants waiting on it.
func (judge *judging) counted() []Suite {
	for i := range judge.suites {
		judge.suites[i].mutants = judge.waiting[judge.suites[i].dir]
	}
	return judge.suites
}

// ownerWrites is the mode a mutated file is written with: the owner
// writes and everyone reads, defined as 0644 in sys/stat.h's bits.
const ownerWrites = 0o644

// Files is the file mutation a mutant run needs.
type Files interface {
	Chmod(string, fs.FileMode) error
	WriteFile(string, []byte, fs.FileMode) error
}

type localFiles struct{}

// Disk is the files of this machine, which a run changes for real.
func Disk() Files { return localFiles{} }

// Chmod changes one file's mode.
func (localFiles) Chmod(path string, mode fs.FileMode) error {
	return os.Chmod(path, mode)
}

// WriteFile writes one file with its mode.
func (localFiles) WriteFile(path string, body []byte, mode fs.FileMode) error {
	return os.WriteFile(path, body, mode)
}

// runOne writes one mutant into a worker's copy of the tree, runs its
// package's tests there, puts the original back, and says what became of it.
func runOne(
	ctx context.Context,
	tree string,
	job work,
	timeout time.Duration,
	files Files,
) Outcome {
	out := NewOutcome(job.mutant, "broken", "")
	path := filepath.Join(tree, job.mutant.file)
	original, err := os.ReadFile(path)
	if err != nil {
		out.failing = err.Error()
		return out
	}
	// the copy keeps the file's mode. a read-only source is still
	// editable here.
	if err := files.Chmod(path, ownerWrites); err != nil {
		out.failing = err.Error()
		return out
	}
	defer files.WriteFile(path, original, ownerWrites)
	err = files.WriteFile(path, job.mutant.source, ownerWrites)
	if err != nil {
		out.failing = err.Error()
		return out
	}
	output, err, timedOut := test(ctx, tree, job.in, timeout)
	if ctx.Err() != nil {
		out.state = "stopped"
		return out
	}
	out.state, out.failing = classify(output, err, timedOut)
	return out
}

// test runs one package's tests in a copy of the tree, under nice and
// with the workspace off so the copy stands alone. it says what go test
// printed and whether it ran out of time.
func test(
	ctx context.Context,
	tree string,
	in pkg,
	timeout time.Duration,
) ([]byte, error, bool) {
	// the build is not the tests' to time, so it has as long again
	bound, cancel := context.WithTimeout(ctx, buildTimes*timeout)
	defer cancel()
	// the package is in its module, so there is a way from one to the other
	inModule, _ := filepath.Rel(in.module, in.dir)
	// the first test failure stops the run. the source is checked where
	// it is written, not here.
	cmd := exec.CommandContext(
		bound,
		"nice", "go", "test", "-count=1", "-failfast", "-vet=off",
		"-timeout", timeout.String(), "./"+filepath.ToSlash(inModule),
	)
	cmd.Dir = filepath.Join(tree, in.module)
	// the temp folder is the worker's own, inside the run's copies
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=",
		"TMPDIR="+scratchOf(tree))
	cmd.WaitDelay = waitDelay
	// go leads a group of its own, and stopping the run stops the group
	procgroup.Leading(cmd)
	output, err := cmd.CombinedOutput()
	return output, err, bound.Err() != nil
}

// lateOnes is the timed-out count as the progress line carries it, and
// nothing when there are none.
func lateOnes(late int) string {
	if count.None(late) {
		return ""
	}
	return fmt.Sprintf(", timed out %d", late)
}

// failedTest finds the tests go test reports as failed.
var failedTest = regexp.MustCompile(`(?m)^\s*--- FAIL: (?P<test>\S+)`)

// every is the count regexp has defined as all the matches there are.
const every = -1

// classify reads what go test said of a mutant: survived when the tests
// passed, unbuildable when the change does not compile, killed when a
// test failed, timed out when the tests ran past the bound with none
// failing, and broken when go could not run the tests at all.
func classify(output []byte, err error, timedOut bool) (string, string) {
	if err == nil {
		return "survived", ""
	}
	if bytes.Contains(output, []byte("[build failed]")) ||
		bytes.Contains(output, []byte("[setup failed]")) {
		return "unbuildable", ""
	}
	var tests []string
	named := failedTest.SubexpIndex("test")
	for _, match := range failedTest.FindAllSubmatch(output, every) {
		tests = append(tests, string(match[named]))
	}
	late := timedOut ||
		bytes.Contains(output, []byte("panic: test timed out"))
	switch {
	case late && count.None(len(tests)):
		return "timed out", ""
	case late:
		tests = append(tests, "(timed out)")
	case bytes.Contains(output, []byte("panic:")):
		tests = append(tests, "(panicked)")
	}
	if count.None(len(tests)) && !bytes.Contains(output, []byte("FAIL")) {
		return "broken", firstLine(output, err)
	}
	return "killed", strings.Join(tests, ",")
}

// firstLine is the first line go printed that says something, skipping "#"
// headings. with none, it is the error.
func firstLine(output []byte, err error) string {
	text := strings.TrimSpace(string(output))
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return line
		}
	}
	return err.Error()
}
