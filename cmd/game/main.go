// game is the verbs a go project needs that go itself does not know it
// needs: check, build, release, bounce, sweep and version. it runs in any
// shell and needs only git and go. there is no makefile, because game
// does everything a makefile would.
//
// game only prints text. if it ever draws in a window, another program
// will do the drawing.
//
//	game check                    gofmt -l, go vet, go test, exit code kept
//	game mutants [--only RE] [--again FILE] [--report FILE]
//	             [--workers N] [--timeout 20s] [--bench]
//	                              the mutation run: every tested package's
//	                              source changed one small thing at a
//	                              time, its tests run against each change,
//	                              in copies of the tree with go.work off.
//	                              survivors, packages that cannot be run
//	                              and stale lines in .mutants are listed
//	                              and are the exit code. .mutants in the
//	                              root names the changes that make no
//	                              difference, NAME :: WHY, one per line.
//	                              --bench runs the suites unchanged, says
//	                              how long the whole would take, and stops.
//	                              --timeout must be at least twice the
//	                              unchanged suite's time; with less the run
//	                              refuses to start and says the suite's time
//	                              and the timeout to give.
//	                              the copies are made in $TMPDIR when that
//	                              is a folder outside /var/folders, where
//	                              Xcode does not measure coverage, and in
//	                              game/mutants in the user cache folder
//	                              (~/Library/Caches) when it is not. each
//	                              run's copies are in a game-mutants-*
//	                              folder there, removed when the run ends.
//	                              SIGINT or SIGTERM stops the run: the
//	                              workers are stopped, with the go test
//	                              each runs and every process that
//	                              started, --report is written with every
//	                              mutant finished and a first line
//	                              "partial run: N of M mutants not
//	                              reached", the copies are removed, the
//	                              tally is told, and the exit is nonzero
//	game build                    every cmd/* into bin/, stamped with the
//	                              commit and the commit's time; the
//	                              same sources build the same bytes
//	game build NAME               a target the config describes: its steps
//	                              in order, in its directory, under nice,
//	                              the output kept in bin/NAME.log
//	game aslr [c SOURCE...]       build, then check the binaries are
//	             [xcode -scheme S] position-independent; no word checks
//	             [go]             every Mach-O already in bin/
//	game leaks c SOURCE... [-- A] build, run under the address sanitizer,
//	             leaks xcode      then under leaks. xcode tests under the
//	             leaks go         sanitizer and runs its executables under
//	                              leaks; go catches what cgo holds
//	game clean [--cache]          go clean, and bin/ away; --cache also
//	                              forgets go's build and test caches
//	game lint [--look]            the rules in your config's `lint`
//	                              lines. wide code and shouting are
//	                              counted, and --look lists them
//	game harden                   source, binaries and modules, held to the
//	             [xcode -scheme S] hardened default; xcode adds the scheme's
//	                              build settings and entitlements
//	game trees                    the road and the dist for this project
//	game push                     the branch to the road, the dirty remote
//	game stick                    claims this repository and pushes the
//	                              claim, refusing on main or a claim
//	                              someone else already holds
//	game sticky                   fast-forwards this branch to the road's
//	                              tip; every build after does the same
//	game unstick                  releases the claim and pushes that
//	game release TAG [--i-mean-it]
//	                              runs every check, needs zero lint
//	                              findings, then makes one flat commit
//	game release diff TAG         what that commit would publish
//	game release push TAG         pushes that commit and its tag to dist,
//	                              the clean remote; nothing else goes there.
//	                              the word and the tag read in either
//	                              order; a word release does not know
//	                              stops it and says what was meant
//	game run NAME                 a run the config describes, in the
//	                              foreground, in your terminal, not nice'd;
//	                              tmux's variables are removed, so a
//	                              program that opens its own window
//	                              doesn't think it is inside tmux
//	game bounce [RANGE]           stops what is staged, or a commit or
//	                              range, that mentions the author in
//	                              the third person, until the author
//	                              approves it
//	game sweep [REV]              checks the tree at a revision (default
//	                              HEAD) for what must not ship
//	game version                  which commit this binary is, and how
//	                              old that commit is
//
// settings come from your ~/.config/game/config first, then from .game
// in the repository, one setting per line. the never-words list, the
// remotes and the values live only in your config. a repository never
// holds them. in ~/.config/game/config:
//
//	author  Ada                         your name; bounce holds changes
//	                                    that use it in the third person
//	words   ~/.config/game/sweep.words  the never-words, baked in by
//	                                    cmd/never when game is built
//
// and a block per project you build, its lines indented under it:
//
//	project game                        a project of yours
//		road ~/roots/game-road.git   its dirty remote: game push
//		dist git@github.com:ada/game its clean remote: game release
//		set CONTACT ada@example.com  a value it needs
//
// if your config has no block for a project, game can still check, build,
// lint and sweep it, but it can't push or release it, because there is
// nowhere to send it. that makes game safe to run on a repository that
// isn't yours, like one you cloned to try.
//
// and in .game, the repository's:
//
//	name    game                        the project, which picks its block
//	needs   CONTACT who to reach        a setting it needs, and why
//	exclude spec-docs/                  left alone by the sweep; repeatable
//	rapid   checks 1000                 under game check, the packages
//	                                    whose tests import rapid run this
//	                                    many checks of each property; in
//	                                    either file
//	lint    width 80                    a lint rule; repeatable. any lint
//	                                    line here replaces all of your
//	                                    config's lint rules
//	target  NAME DIR :: COMMAND ...     a build beyond go's; one line per
//	                                    step, in order. test and test-*
//	                                    targets run before every release
//	run     NAME DIR :: COMMAND ...     a thing to run, the same way
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/janearc/game/internal/bounce"
	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
	"github.com/janearc/game/internal/patch"
	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/stack"
	"github.com/janearc/game/internal/sticky"
	"github.com/janearc/game/internal/sweep"
)

// build and built are the commit and its time, stamped when game is built.
var (
	build       = "dev"
	built       = ""
	stampedWho  = ""
	stampedSids = ""
)

// cwd is the directory game was run in, which is the repository.
func cwd() string {
	dir, _ := os.Getwd()
	return dir
}

// neverWords is the list of words this run sweeps. cmd/never
// includes them when game is built. tests use made-up words here.
//
//ninja:vow decoy a test binary bakes none in, so TestMain gives it some
var neverWords = baked

// baked is the never-words this build was baked with.
func baked() sweep.Never { return sweep.Baked(sweep.Embedded()) }

// a verb is what main runs for its word. it returns flag.ErrHelp if --help
// is given, after printing what it does.
type verb func(here repo.Repo, cfg config.Config, args []string) error

// verbs is the table main dispatches on. help-claude may name only these.
var verbs = map[string]verb{
	"version":       ageVerb,
	"--age":         ageVerb,
	"help-claude":   helpVerb,
	"--help-claude": helpVerb,
	"check":         withoutWords(check),
	"mutants":       mutantsVerb,
	"build":         buildVerb,
	"build-c":       buildCVerb,
	"build-xcode":   buildXcodeVerb,
	"cover":         coverVerb,
	"fuzz":          fuzzVerb,
	"race":          raceVerb,
	"aslr":          aslrVerb,
	"leaks":         leaksVerb,
	"harden":        hardenVerb,
	"verify":        verifyVerb,
	"patch":         patchVerb,
	"run":           runVerb,
	"lint":          lintVerb,
	"clean":         cleanVerb,
	"trees":         treesVerb,
	"who":           whoVerb,
	"push":          withoutWords(pushRoad),
	"stick":         bareVerb("stick", aboutStick, doStick),
	"sticky":        bareVerb("sticky", aboutSticky, doSticky),
	"unstick":       bareVerb("unstick", aboutUnstick, doUnstick),
	"release":       releaseVerb,
	"bounce":        bounceVerb,
	"sweep":         sweepVerb,
	"gargle-balls-dario": bareVerb("gargle-balls-dario", aboutGargle,
		doGargle),
}

// main is game, run in the directory it was started in.
func main() {
	_, args, _ := verbWord(os.Args) // the first word is game itself
	code := runPzm(args, cwd(), config.Whose(), os.Getenv("HOME"),
		os.Stdin, os.Stdout, os.Stderr)
	os.Exit(code)
}

// game's exit codes, as the shell has defined them: success, a verb's
// failure, and bad usage.
const (
	exitOK     = 0 // defined by the shell: success
	exitFailed = 1 // defined by convention: the verb failed
	exitUsage  = 2 // defined by convention: a word game cannot start on
)

// play loads the config whose names over the repository's in dir,
// settles the environment and runs the verb args names first. it takes
// what it reads, so a test can give its own, and it is game's exit code:
// 2 for a word it cannot start on, 1 for a verb's error, 0 otherwise.
func play(args []string, dir, whose, home string) int {
	word, rest, said := verbWord(args)
	if !said {
		usage()
		return exitUsage
	}
	here := repo.At(dir, repo.Exec())
	cfg, err := config.Load(whose, home, here.Dir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "game:", err)
		return exitUsage
	}
	if err := settle(cfg, word, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "game:", err)
		return exitUsage
	}
	do, known := verbs[word]
	if !known {
		usage()
		return exitUsage
	}
	err = do(here, cfg, rest)
	// --help printed what the verb does and did nothing. that is not a
	// failure.
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "game:", err)
		return exitFailed
	}
	return exitOK
}

// settle copies the settings this repository needs from your config into
// the environment of whatever game runs, and no others. for build and
// run it says each one still missing on errs, and refuses.
func settle(cfg config.Config, word string, errs io.Writer) error {
	for _, pair := range cfg.Env() {
		name, value, _ := strings.Cut(pair, "=")
		os.Setenv(name, value)
	}
	if word != "build" && word != "run" {
		return nil
	}
	miss := cfg.Missing()
	for _, need := range miss {
		fmt.Fprintf(errs, "game: %s is needed: %s\n", need.Name(),
			need.Why())
	}
	if count.Positive(len(miss)) {
		return errors.New("set it in ~/.config/game/config as " +
			"\"set NAME value\"")
	}
	return nil
}

// wordless is a verb that takes no words: what it does, and what to
// say of it when --help asks.
type wordless struct {
	name, about string
	do          func(repo.Repo, config.Config) error
}

// ignoring runs the verb whatever words it was given.
func (verb wordless) ignoring(here repo.Repo, cfg config.Config,
	_ []string) error {
	return verb.do(here, cfg)
}

// refusing runs the verb only when it was given no words; --help says
// what it does, and any other word is refused.
func (verb wordless) refusing(here repo.Repo, cfg config.Config,
	args []string) error {
	if err := bare(os.Stderr, verb.name, verb.about, args); err != nil {
		return err
	}
	return verb.do(here, cfg)
}

// withoutWords is a verb that ignores any given words.
func withoutWords(do func(repo.Repo, config.Config) error) verb {
	return wordless{do: do}.ignoring
}

// bareVerb is a verb that takes no words. --help says what it does and does
// nothing, and any other word is refused.
func bareVerb(
	name, about string,
	do func(repo.Repo, config.Config) error,
) verb {
	return wordless{name: name, about: about, do: do}.refusing
}

// ageVerb is game version, and game --age.
func ageVerb(repo.Repo, config.Config, []string) error {
	fmt.Println(age())
	return nil
}

// helpVerb is game help-claude.
func helpVerb(repo.Repo, config.Config, []string) error {
	fmt.Fprint(os.Stdout, helpClaude)
	return nil
}

// mutantsVerb is game mutants: Go by game's own mutator, or, named by its
// first word, another language by that language's tool.
func mutantsVerb(here repo.Repo, _ config.Config, args []string) error {
	word, rest, _ := verbWord(args)
	if mutate := foreignMutators[word]; mutate != nil {
		return mutate(here.Dir(), rest, run, os.Stdout, os.Stderr)
	}
	return mutate(here, args)
}

// verifyVerb is game verify, the child a stack member runs.
func verifyVerb(here repo.Repo, cfg config.Config, _ []string) error {
	return verify(here, cfg, os.Stdout)
}

// buildVerb is game build. it builds the binaries, a named target, docker or
// the stack, and shows the art after a build that passed.
func buildVerb(here repo.Repo, cfg config.Config, args []string) error {
	info, err := os.Stdout.Stat()
	coloured := colour(os.Getenv("NO_COLOR"), info, err)
	return buildTo(here, cfg, args, os.Stdout, coloured)
}

// buildTo is buildVerb, saying what it built on out, and the art too when
// coloured.
func buildTo(here repo.Repo, cfg config.Config, args []string, out io.Writer,
	coloured bool) error {
	return buildToUsing(here, cfg, args, out, coloured, selfVerify)
}

// buildToUsing is buildTo with the child verification given.
func buildToUsing(
	here repo.Repo,
	cfg config.Config,
	args []string,
	out io.Writer,
	coloured bool,
	selfCheck func() (func(string) *exec.Cmd, error),
) error {
	var err error
	word, rest, said := verbWord(args)
	switch {
	case !said:
		err = buildBinariesBy(here, cfg, madeBy(here), run, out,
			coloured)
	case word == "stack":
		child, bad := selfCheck()
		if bad != nil {
			return bad
		}
		err = buildStack(here, cfg, rest, out, child)
	case word == "docker":
		err = buildDocker(here, cfg, exec.Command, out)
	default:
		err = buildTargetTo(here, cfg, word, out)
	}
	if err == nil && coloured {
		paintTo(out, here.Dir(), project(here, cfg))
	}
	return err
}

// runVerb is game run NAME.
func runVerb(here repo.Repo, cfg config.Config, args []string) error {
	name, _, said := verbWord(args)
	if !said {
		usage()
		os.Exit(exitUsage)
	}
	return runNamed(here, cfg, name)
}

// lintVerb is game lint. it lists every finding unless --count, and
// lint.Verdict decides the exit code.
func lintVerb(here repo.Repo, cfg config.Config, args []string) error {
	return lintTo(here, cfg, args, os.Stdout, os.Stderr)
}

// lintTo is game lint, listing and tallying on out, and saying on errs
// that there are no rules.
func lintTo(here repo.Repo, cfg config.Config, args []string,
	out, errs io.Writer) error {
	fs := flag.NewFlagSet("lint", flag.ExitOnError)
	count := fs.Bool(
		"count",
		false,
		"the tallies alone, without the findings",
	)
	fs.Bool("look", false, "kept: every finding is listed now")
	fs.Parse(args)
	rules, err := described(cfg)
	if err != nil {
		return err
	}
	if rules.Empty() {
		fmt.Fprintln(errs,
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
		if !*count {
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

// cleanVerb is game clean [--cache].
func cleanVerb(here repo.Repo, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("clean", flag.ExitOnError)
	cache := fs.Bool(
		"cache",
		false,
		"also forget go's build and test caches, "+
			"for a cold run",
	)
	fs.Parse(args)
	return clean(here, cfg, *cache)
}

// treesVerb is game trees. it shows the block this repository matched, its
// road and dist, and whether the dist is guarded.
func treesVerb(here repo.Repo, cfg config.Config, _ []string) error {
	return treesTo(here, cfg, os.Stdout)
}

// treesTo is game trees, saying the block on out.
func treesTo(here repo.Repo, cfg config.Config, out io.Writer) error {
	mine, held := cfg.Mine()
	if !held {
		fmt.Fprintf(out, "a local build: %v\n", cfg.NoBlock())
	}
	road := orNone(mine.Road(), cfg.Dotfile())
	dist := orNone(mine.Dist(), cfg.Dotfile())
	fmt.Fprintf(out, "road: %s\n", road)
	fmt.Fprintf(out, "dist: %s\n", dist)
	return guard(here, mine.Dist(), out)
}

// releaseVerb handles four forms: TAG, diff TAG, push TAG and stack TAG.
func releaseVerb(here repo.Repo, cfg config.Config, args []string) error {
	form, tag, rest, err := releaseForm(args)
	if err != nil {
		return err
	}
	fs, mean, from, publish := releaseFlags(form)
	fs.Parse(rest)
	if err := leftover(fs, tag); err != nil {
		return err
	}
	switch form {
	case "stack":
		return releaseStack(here, cfg, tag, *from, *mean, os.Stdout,
			ours())
	case "diff":
		return releaseDiff(here, cfg, tag)
	case "push":
		return releasePush(here, cfg, tag, *publish, os.Stdout, ours())
	}
	return releasePrepare(here, cfg, tag, *mean, os.Stdout, ours())
}

// releaseFlags returns the flags a release form takes. a flag the form does
// not take stays at its zero, so it is refused as unknown.
func releaseFlags(form string) (*flag.FlagSet, *bool, *string, *bool) {
	fs := flag.NewFlagSet(
		strings.TrimSpace("release "+form), flag.ExitOnError,
	)
	mean, from, publish := new(bool), new(string), new(bool)
	switch form {
	case "":
		mean = fs.Bool(
			"i-mean-it",
			false,
			"release even though the lint has things to "+
				"fix or to look at",
		)
	case "stack":
		from = fs.String(
			"from",
			"dist",
			"take each member from its road or its dist",
		)
		mean = fs.Bool(
			"i-mean-it",
			false,
			"assemble even though the lint has things "+
				"to fix or to look at",
		)
	case "push":
		publish = fs.Bool(
			"publish",
			false,
			"say it out loud: this dist is a remote and "+
				"pushing to it publishes",
		)
	}
	return fs, mean, from, publish
}

// bounceVerb is game bounce, over what is staged or a range given. added
// lines that name the author in the third person are held until signed off
// in the environment.
func bounceVerb(here repo.Repo, cfg config.Config, args []string) error {
	signed := os.Getenv("BOUNCE_OK") != ""
	return bounceTo(here, cfg, args, signed, os.Stdout)
}

// bounceTo is game bounce, passing what it holds when signed, and saying
// what it held on out.
func bounceTo(here repo.Repo, cfg config.Config, args []string, signed bool,
	out io.Writer) error {
	if cfg.Author() == "" {
		return fmt.Errorf(
			"bounce needs an author: \"author " +
				"NAME\" in ~/.config/game/config",
		)
	}
	var hits []bounce.Hit
	var err error
	if rng, _, said := verbWord(args); said {
		hits, err = bounce.Range(here, rng, cfg.Author())
	} else {
		hits, err = bounce.Staged(here, cfg.Author())
	}
	if err != nil || count.None(len(hits)) {
		return err
	}
	fmt.Fprintln(
		out,
		"bounce: the author in the third "+
			"person, in added lines:",
	)
	for _, hit := range hits {
		fmt.Fprintln(out, "  "+hit.Line())
	}
	if signed {
		fmt.Fprintln(
			out,
			"bounce: signed off "+
				"(BOUNCE_OK); passing",
		)
		return nil
	}
	return fmt.Errorf("refused. reword to the " +
		"first person, or " +
		"BOUNCE_OK=1 to sign it off")
}

// sweepVerb is game sweep [REV]. it checks the history for trailers and the
// tree at HEAD or REV for the fixed patterns, the never-words and the
// author. with no never-words it never reports clean.
func sweepVerb(here repo.Repo, cfg config.Config, args []string) error {
	return sweepTo(here, cfg, args, neverWords(), os.Stdout)
}

// sweepTo is game sweep, looking for never and saying each hit on out.
func sweepTo(here repo.Repo, cfg config.Config, args []string,
	never sweep.Never, out io.Writer) error {
	rev := "HEAD"
	if asked, _, said := verbWord(args); said {
		rev = asked
	}
	hits, err := sweep.History(here, rev)
	if err != nil {
		return fmt.Errorf("the history of %s: %w", rev, err)
	}
	treeHits, err := sweep.Tree(
		here,
		rev,
		never,
		cfg.Author(),
		cfg.SweepAllow(),
		cfg.Exclude()...)
	if err != nil {
		return err
	}
	hits = append(hits, treeHits...)
	for _, hit := range hits {
		fmt.Fprintf(out, "sweep: %s\n", hit)
	}
	switch {
	case count.Positive(len(hits)):
		return fmt.Errorf("%d hit(s)", len(hits))
	case never.Empty():
		return fmt.Errorf("%s has none of the fixed "+
			"patterns, but this game was built without "+
			"never-words, so none were looked for", rev)
	}
	fmt.Fprintf(out, "sweep: %s is clean\n", rev)
	return nil
}

// usage prints the usage. the caller exits.
func usage() {
	fmt.Fprintln(
		os.Stderr,
		"game check | mutants [--only RE] [--again FILE] "+
			"[--report FILE] [--bench] |\n"+
			"     mutants mutate-c|mutate-xcode|mutate-text ... "+
			"|\n"+
			"     build [NAME] | build stack | "+
			"build-c [-o NAME] [-with LIB] SOURCE... |\n"+
			"     build-xcode -scheme S [-with LIB] |\n"+
			"     cover [c SOURCE... | xcode -scheme S] |\n"+
			"     fuzz c SOURCE... | "+
			"race [c SOURCE... | xcode -scheme S] | "+
			"aslr [c SOURCE... |\n"+
			"     xcode -scheme S | go] | "+
			"leaks [c SOURCE... | xcode -scheme S "+
			"| go] |\n"+
			"     patch [--dry] [NAME] | run NAME |\n"+
			"     harden [xcode -scheme S] |\n"+
			"     clean [--cache] | lint [--look] |\n"+
			"trees | who | push | stick | sticky | unstick | "+
			"release TAG [--i-mean-it] | release push TAG | "+
			"release diff |\n"+
			"     bounce [RANGE] | sweep [REV] | version | "+
			"help-claude |\n"+
			"     gargle-balls-dario",
	)
}

// what the pairing verbs do, printed by --help instead of doing it.
const (
	aboutStick = "claims this repository: a committed .sticky with your " +
		"name, pushed to the road. refused on main, or when someone " +
		"else holds it."
	aboutSticky = "follows the claim: fast-forwards this branch to the " +
		"holder's commit, now and on every build. refused over " +
		"anything divergent."
	aboutUnstick = "lets go of the claim: removes .sticky, commits, pushes."
)

// saying is one verb's name and what it does, for its --help.
type saying struct {
	out   io.Writer
	name  string
	about string
}

// usage says what the verb does, as flag's --help asks.
func (said saying) usage() {
	fmt.Fprintf(said.out, "game %s: %s\n", said.name, said.about)
}

// bare reads the words after a verb that takes none. --help prints what
// the verb does and returns flag.ErrHelp, so the verb is not done. any
// other word is an error that says the same.
func bare(out io.Writer, name, about string, args []string) error {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = saying{out: out, name: name, about: about}.usage
	if err := fs.Parse(args); err != nil {
		return err
	}
	if word, _, said := verbWord(fs.Args()); said {
		return fmt.Errorf(
			"%s takes no words, and %q is one. game %s: %s",
			name,
			word,
			name,
			about,
		)
	}
	return nil
}

// age says which commit this binary was built from, and how old that commit
// is. built is the commit's time, not the build's.
func age() string {
	return ageOf(build, built, time.Now())
}

// ageOf is age for a binary stamped with build and built, asked at now.
func ageOf(build, built string, now time.Time) string {
	// we weren't built by game, so we give them this.
	if built == "" && build == "dev" {
		return "game, built from a tree by go rather than by game: " +
			"no commit and no commit time in it"
	}
	if built == "" {
		return fmt.Sprintf(
			"game %s, with no commit time in it", build,
		)
	}
	when, err := time.Parse(time.RFC3339, built)
	if err != nil {
		return fmt.Sprintf("game %s, committed at %s", build, built)
	}
	return fmt.Sprintf(
		"game %s, committed %s ago",
		build,
		now.Sub(when).Round(time.Second),
	)
}

// project returns the name used for messages and lookups.
func project(here repo.Repo, cfg config.Config) string {
	return cfg.Named()
}

// orNone returns a remote, or says there is none and where to add it.
func orNone(url, dotfile string) string {
	if url == "" {
		return "(none; add it to " + dotfile + ")"
	}
	return url
}

// pushRoad sends the branch you are on to the road, and says so first,
// so a push is never mistaken for a release.
func pushRoad(here repo.Repo, cfg config.Config) error {
	return pushRoadTo(here, cfg, os.Stdout)
}

// pushRoadTo is pushRoad, saying the push on out.
func pushRoadTo(here repo.Repo, cfg config.Config, out io.Writer) error {
	name := project(here, cfg)
	road, err := roadFor(here, cfg)
	if err != nil {
		return err
	}
	mine, _ := cfg.Mine()
	dist := mine.Dist()
	if road == dist {
		return fmt.Errorf(
			"the road and dist for %s are the same "+
				"remote; they never are",
			name,
		)
	}
	if err := guard(here, dist, out); err != nil {
		return err
	}
	branch, err := here.Branch()
	if err != nil || branch == "HEAD" {
		return fmt.Errorf(
			"not on a branch; check one out before " +
				"pushing to the road",
		)
	}
	fmt.Fprintf(out, "pushed %s to the road, %s\n", branch, road)
	_, err = here.Git("push", road, branch)
	return err
}

// roadFor returns this project's road or an error explaining why.
func roadFor(here repo.Repo, cfg config.Config) (string, error) {
	name := project(here, cfg)
	mine, held := cfg.Mine()
	if !held {
		return "", cfg.NoBlock()
	}
	if mine.Road() == "" {
		return "", fmt.Errorf(
			"no road for %s; its block in %s has none",
			name,
			cfg.Dotfile(),
		)
	}
	err := there(mine.Road(), here.Dir(), name, cfg.Dotfile())
	if err != nil {
		return "", err
	}
	return mine.Road(), nil
}

// there checks that a remote given as a path on this machine exists. if
// it does not, the error names the path, the project and the config file
// it came from. a relative path is resolved from the repository. urls are
// not checked.
func there(remote_, dir, name, dotfile string) error {
	if repo.Elsewhere(remote_) {
		return nil
	}
	at := remote_
	if !filepath.IsAbs(at) {
		at = filepath.Join(dir, at)
	}
	if _, err := os.Stat(at); err == nil {
		return nil
	}
	where := ""
	if at != remote_ {
		where = " (looked at " + at + ")"
	}
	return fmt.Errorf(
		"the road for %s is not there: %s%s; it is named in %s "+
			"under \"project %s\"",
		name, remote_, where, dotfile, name,
	)
}

// doStick is game stick. it claims this repository for your author name
// by committing .sticky, then pushes, so the other person's game sees
// the claim.
func doStick(here repo.Repo, cfg config.Config) error {
	return stickTo(here, cfg, os.Stdout)
}

// stickTo is doStick, saying what it did on out.
func stickTo(here repo.Repo, cfg config.Config, out io.Writer) error {
	if cfg.Author() == "" {
		return fmt.Errorf(
			"stick needs an author: \"author NAME\" in " +
				"~/.config/game/config",
		)
	}
	at, err := roadFor(here, cfg)
	if err != nil {
		return err
	}
	// the road's tip first, when the road has this branch. a claim
	// committed behind it cannot be pushed.
	if err := catchUp(here, at, out); err != nil {
		return err
	}
	made, err := sticky.Stick(here, at, cfg.Author())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "stick: stuck on %s by %s\n", made.Branch(),
		made.Holder())
	if err := pushRoadTo(here, cfg, out); err != nil {
		return fmt.Errorf(
			"stuck here, but the claim did not reach the "+
				"road: %v; game push sends it",
			err,
		)
	}
	return nil
}

// catchUp fast-forwards this branch to the road's tip when the road is
// ahead, and says so on out. a detached head is left for Stick to refuse.
func catchUp(here repo.Repo, road string, out io.Writer) error {
	branch, err := here.Branch()
	if err != nil || branch == "HEAD" {
		return err
	}
	behind, err := sticky.Behind(here, road, branch)
	if err != nil || count.None(behind) {
		return err
	}
	tip, _, err := sticky.Sync(here, road, branch)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "stick: caught up to the road at %s first\n",
		repo.Short(tip))
	return nil
}

// doSticky is game sticky, the pairing verb. it fast-forwards this
// branch to the road's tip, and every build does the same while the
// claim holds.
func doSticky(here repo.Repo, cfg config.Config) error {
	return stickyTo(here, cfg, os.Stdout)
}

// stickyTo is doSticky, saying where it is stuck on out.
func stickyTo(here repo.Repo, cfg config.Config, out io.Writer) error {
	at, err := roadFor(here, cfg)
	if err != nil {
		return err
	}
	branch, err := onBranch(here)
	if err != nil {
		return err
	}
	tip, _, err := sticky.Sync(here, at, branch)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "sticky: stuck to %s\n", repo.Short(tip))
	return nil
}

// onBranch is the branch checked out, or why there is none to name.
func onBranch(here repo.Repo) (string, error) {
	branch, err := here.Branch()
	if err != nil {
		return "", fmt.Errorf("no branch to name: %w", err)
	}
	return branch, nil
}

// doUnstick is game unstick. it releases the claim, commits and pushes,
// so the other person sees the claim is gone.
func doUnstick(here repo.Repo, cfg config.Config) error {
	return unstickTo(here, cfg, os.Stdout)
}

// unstickTo is doUnstick, saying what it did on out.
func unstickTo(here repo.Repo, cfg config.Config, out io.Writer) error {
	// check the road first. releasing the claim is a commit, and if it
	// can't be pushed, the other person still sees the claim.
	at, err := roadFor(here, cfg)
	if err != nil {
		return err
	}
	// and take the other person's changes first, for the same reason.
	branch, err := onBranch(here)
	if err != nil {
		return err
	}
	if behind, err := sticky.Behind(here, at, branch); err != nil {
		return err
	} else if count.Positive(behind) {
		return mergeFirst()
	}
	tip, err := sticky.Unstick(here)
	if err != nil {
		return err
	}
	fmt.Fprintf(
		out,
		"unstick: no longer sticky %s; cleared for publish\n",
		repo.Short(tip),
	)
	if err := pushRoadTo(here, cfg, out); err != nil {
		return fmt.Errorf(
			"unstuck here, but the road still carries the claim: "+
				"%v; game push sends the release",
			err,
		)
	}
	return nil
}

// releaseDiff is game release diff TAG. it shows what a release now would
// send to dist, with the sweep's refusals first.
func releaseDiff(here repo.Repo, cfg config.Config, tag string) error {
	return releaseDiffTo(here, cfg, tag, neverWords(), os.Stdout)
}

// releaseDiffTo is releaseDiff, sweeping for never and saying it on out.
func releaseDiffTo(here repo.Repo, cfg config.Config, tag string,
	never sweep.Never, out io.Writer) error {
	name, dist, err := distFor(cfg, "nothing to diff against")
	if err != nil {
		return err
	}
	opts := releaseOptions(name, dist, tag, cfg)
	// the setting values are the release's refusal, not the diff's
	opts = opts.Refusing(nil)
	diff, err := release.Diff(here, opts)
	if err != nil {
		return err
	}
	what := name
	if tag != "" {
		what = name + " " + tag
	}
	if never.Empty() {
		fmt.Fprintf(out, "release diff %s: this game was built "+
			"without never-words, so the sweep below did not "+
			"look for them, and a release would refuse\n", what)
	}
	tellDiff(out, what, diff)
	return nil
}

// difference is what tellDiff reads of a release.Difference, so a test
// can hand it one of its own.
type difference interface {
	Same() bool
	Parent() string
	Tree() string
	Stat() string
	Hits() []sweep.Hit
	Patch() string
}

// tellDiff prints a diff. it says if there is nothing to send, else the two
// ends, the stat, what the sweep would refuse, and the patch.
func tellDiff(out io.Writer, what string, diff difference) {
	if diff.Same() {
		fmt.Fprintf(
			out,
			"release diff %s: dist already has this tree, "+
				"so a release would change nothing\n",
			what,
		)
		return
	}
	from := "an empty dist"
	if diff.Parent() != "" {
		from = repo.Short(diff.Parent())
	}
	to := repo.Short(diff.Tree())
	fmt.Fprintf(out, "release diff %s: %s -> %s\n\n", what, from, to)
	// Repo.Git trims the stat, so the leading space and the trailing
	// newline are put back here.
	fmt.Fprintf(out, " %s\n", diff.Stat())
	// the sweep's hits are printed first, because any one of them
	// stops the release.
	for _, hit := range diff.Hits() {
		fmt.Fprintf(out, "would be refused: %s\n", hit)
	}
	fmt.Fprintln(out)
	fmt.Fprint(out, diff.Patch())
}

// releaseForms is the words release knows after itself. any other word
// that is not a flag is a tag.
var releaseForms = map[string]bool{"diff": true, "push": true, "stack": true}

// releaseForm reads the words after release into a form, a tag and the
// flags, with the form and the tag in either order. a word release does not
// know stops it, and a form that needs a tag says so.
func releaseForm(args []string) (form, tag string, flags []string, err error) {
	var words []string
	flags = nil
	for i, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flags = args[i:]
			break
		}
		words = append(words, arg)
	}
	for _, word := range words {
		switch {
		case releaseForms[word] && form == "":
			form = word
		case !releaseForms[word] && tag == "":
			tag = word
		default:
			return "", "", nil, unknownWord(word, firstTag(words))
		}
	}
	if tag == "" && form != "diff" {
		want := "game release TAG"
		if form != "" {
			want = "game release " + form + " TAG"
		}
		return "", "", nil, fmt.Errorf(
			"release wants a tag: %s; nothing was prepared",
			want,
		)
	}
	return form, tag, flags, nil
}

// firstTag is the first word that is not a form, or TAG.
func firstTag(words []string) string {
	for _, word := range words {
		if !releaseForms[word] {
			return word
		}
	}
	return "TAG"
}

// unknownWord is the error for a word release does not know.
func unknownWord(word, tag string) error {
	return fmt.Errorf(
		"release: %q is not a word release knows; nothing was "+
			"prepared and nothing was pushed. did you mean game "+
			"release diff %s, or game release push %s",
		word,
		tag,
		tag,
	)
}

// leftover is the error for a word left after the flags.
func leftover(fs *flag.FlagSet, tag string) error {
	if word, _, said := verbWord(fs.Args()); said {
		return unknownWord(word, tag)
	}
	return nil
}

// noDistForRelease is why a block with no dist cannot release.
const noDistForRelease = "nowhere for a release to go: this tree " +
	"builds locally and that is all"

// distFor is the project's name and dist, or why no dist is available.
func distFor(cfg config.Config, why string) (name, dist string, err error) {
	name = cfg.Named()
	mine, held := cfg.Mine()
	if !held {
		return name, "", cfg.NoBlock()
	}
	if mine.Dist() == "" {
		return name, "", fmt.Errorf(
			"%s has no dist in its block in %s, so there is %s",
			name,
			cfg.Dotfile(),
			why,
		)
	}
	return name, mine.Dist(), nil
}

// releaseOptions returns the release package's options. they hold the
// project, its dist and tag, and everything the sweep refuses.
func releaseOptions(name, dist, tag string, cfg config.Config) release.Options {
	return release.For(name, dist, tag).
		Swept(neverWords(), cfg.Exclude(), cfg.SweepAllow()).
		Omitting(cfg.Omit()).
		By(cfg.Author()).
		Licensed(cfg.License()).
		Refusing(values(cfg)).
		PatchedFrom(cfg.Patches())
}

// releaseChecks is what a release passes before a commit is cut. every
// needed setting is set, the lint is at zero unless meant, go's own
// checks pass where there is a module, and the test targets pass.
//
// with lint harden in the .game, the layers beyond the source rules
// run last, over binaries freshly built, and their findings refuse the
// release as the lint's do, unless meant.
func releaseChecks(here repo.Repo, cfg config.Config, mean bool, by runner,
	out io.Writer) error {
	if err := needsSet(cfg, out); err != nil {
		return err
	}
	rules, err := described(cfg)
	if err != nil {
		return err
	}
	if err := lintClean(here, rules, mean, out); err != nil {
		return err
	}
	if _, err := os.Stat(here.Path("go.mod")); err == nil {
		if err := checkBy(here, cfg, by, out); err != nil {
			return err
		}
	}
	for _, target := range cfg.Targets() {
		if !testTarget(target.Name()) {
			continue
		}
		err := buildTargetTo(here, cfg, target.Name(), out)
		if err != nil {
			return err
		}
	}
	if !rules.Harden() {
		return nil
	}
	beyond, err := hardenBuilt(here, cfg, by, out)
	if err != nil {
		return err
	}
	return refuseHardened(beyond, mean, out)
}

// needsSet refuses a release while a needed setting is unset, saying
// each on out.
func needsSet(cfg config.Config, out io.Writer) error {
	miss := cfg.Missing()
	if count.None(len(miss)) {
		return nil
	}
	for _, need := range miss {
		fmt.Fprintf(out, "release: %s is needed: %s\n", need.Name(),
			need.Why())
	}
	return fmt.Errorf("refused: set the needed settings in " +
		"~/.config/game/config first")
}

// lintClean refuses a release on any lint finding unless meant, saying
// each on out.
func lintClean(here repo.Repo, rules lint.Lint, mean bool,
	out io.Writer) error {
	found, err := rules.Run(here.Dir())
	if err != nil {
		return err
	}
	if count.None(len(found)) || mean {
		return nil
	}
	for _, finding := range found {
		fmt.Fprintln(out, finding)
	}
	return fmt.Errorf("refused: the lint found %d; a release "+
		"takes none (--i-mean-it overrides)", len(found))
}

// hardenBuilt is the harden layers beyond the source rules, over
// binaries built now rather than whatever bin/ held: a stale bin/
// audited clean would be a false claim about the tree being released.
func hardenBuilt(here repo.Repo, cfg config.Config, by runner,
	out io.Writer) ([]lint.Finding, error) {
	if st, err := os.Stat(here.Path("cmd")); err == nil &&
		st.IsDir() {
		if err := buildBinariesBy(here, cfg, madeBy(here), by, out,
			false); err != nil {
			return nil, err
		}
	}
	return hardenBeyondSource(here.Dir(), by)
}

// refuseHardened says each harden finding on out and refuses, unless
// meant: --i-mean-it is the glass to break when a release has to go
// out over a finding, and it breaks here as it does over the lint.
func refuseHardened(found []lint.Finding, mean bool, out io.Writer) error {
	if none(found) {
		return nil
	}
	for _, finding := range found {
		fmt.Fprintln(out, finding)
	}
	if mean {
		fmt.Fprintf(out, "release: harden found %d, and you meant it\n",
			len(found))
		return nil
	}
	return fmt.Errorf(
		"refused: harden found %d; a release "+
			"takes none (--i-mean-it overrides)",
		len(found),
	)
}

// testTarget returns true if the name is "test" or starts with "test-".
func testTarget(name string) bool {
	return name == "test" || strings.HasPrefix(name, "test-")
}

// releasePrepare is game release TAG. it runs the gate and the checks, then
// keeps one flat commit at the release ref for its push.
func releasePrepare(
	here repo.Repo,
	cfg config.Config,
	tag string,
	mean bool,
	out io.Writer,
	made stamp,
) error {
	if err := made.stamped(); err != nil {
		return err
	}
	if _, stuck, err := sticky.Stuck(here.Dir()); err != nil {
		return err
	} else if stuck {
		return fmt.Errorf(
			"stuck: unstick first, so the flat tree this " +
				"release makes carries no trace of the claim",
		)
	}
	name, dist, err := distFor(cfg, noDistForRelease)
	if err != nil {
		return err
	}
	if err := guard(here, dist, out); err != nil {
		return err
	}
	if err := releaseChecks(here, cfg, mean, run, out); err != nil {
		return err
	}
	res, err := release.Prepare(here, releaseOptions(name, dist, tag, cfg))
	if err != nil {
		return err
	}
	commit := repo.Short(res.Commit())
	fmt.Fprintf(out, "release %s %s: prepared %s, tree %s\n", name, tag,
		commit, res.Tree())
	fmt.Fprintf(
		out,
		"send it with: game release push %s, to dist (clean): %s\n",
		tag,
		dist,
	)
	return nil
}

// stamp is the commit a game was built from and that commit's time: for
// this binary, build and built. a release is given one rather than
// reading them, so a test can give its own.
type stamp struct {
	commit, at string
}

// ours is this binary's stamp.
func ours() stamp { return stamp{commit: build, at: built} }

// stamped refuses to make or push a release from a binary that can't say
// which commit it came from.
func (made stamp) stamped() error {
	if made.at != "" && made.commit != "dev" {
		return nil
	}
	return fmt.Errorf(
		"this game was built by go rather than by game, so it cannot " +
			"say which commit it is. a release is a claim and it " +
			"wants a tool with provenance: run sh " +
			"bootstrap.sh, or game build, and try again",
	)
}

// values returns the setting values from your config.
func values(cfg config.Config) []string {
	set := cfg.Set()
	out := make([]string, count.Nothing, len(set))
	for _, value := range set {
		out = append(out, value)
	}
	return out
}

// releaseStack is game release stack TAG. it assembles the members, from
// their roads or their dists, into one flat commit, holds it to every
// member's lint, and keeps it at the release ref for its push.
func releaseStack(
	here repo.Repo,
	cfg config.Config,
	tag string,
	from string,
	mean bool,
	out io.Writer,
	made stamp,
) error {
	if err := made.stamped(); err != nil {
		return err
	}
	name, dist, err := distFor(cfg, "nowhere for a stack to go")
	if err != nil {
		return err
	}
	// no members is refused by release.Stack, in the same words
	remotes, err := remotesFrom(cfg, from)
	if err != nil {
		return err
	}
	if _, stuck, err := sticky.Stuck(here.Dir()); err != nil {
		return err
	} else if stuck {
		return fmt.Errorf(
			"stuck: unstick first, so the stack this release " +
				"makes carries no trace of the claim",
		)
	}
	if err := releaseChecks(here, cfg, mean, run, out); err != nil {
		return err
	}
	opts := releaseOptions(name, dist, tag, cfg)
	// a stack keeps each member's licence; none is laid over it
	by := lintOfTheAssembly{here: here, cfg: cfg, mean: mean, out: out,
		builds: goAssembly{here: here, cfg: cfg, by: run, out: out,
			command: exec.Command}}
	opts = opts.Licensed("").Qualifying(by)
	members := stackMembers(cfg, remotes)
	res, err := release.Stack(here, opts, members)
	if err != nil {
		return err
	}
	tellStacked(out, name, tag, from, dist, res)
	return nil
}

// the assembly's lint is what release.Stack asks before it keeps a stack.
var _ release.Qualifier = lintOfTheAssembly{}

// lintOfTheAssembly is the qualifier that holds a stack to every member's
// rules and to go's own checks before its ref is written, so a refused
// stack keeps nothing.
type lintOfTheAssembly struct {
	here   repo.Repo
	cfg    config.Config
	mean   bool
	out    io.Writer
	builds assemblyBuilds
}

// Qualify lints the assembled commit by the rules its members keep. Then
// it vets and tests every go module of the assembly.
func (by lintOfTheAssembly) Qualify(assembled release.Stacked) error {
	err := stackLint(by.here, by.cfg, assembled.Commit(), assembled.Games(),
		by.mean, by.out)
	if err != nil {
		return err
	}
	return stackBuilds(by.builds, assembled.Commit(), assembled.Modules())
}

// remotesFrom is the remote of each project, from its road or from its
// dist. Any other word for where members come from is refused.
func remotesFrom(
	cfg config.Config, from string,
) (map[string]string, error) {
	switch from {
	case "road":
		return cfg.Remotes(false), nil
	case "dist":
		return cfg.Remotes(true), nil
	}
	return nil, fmt.Errorf("--from is road or dist, not %q", from)
}

// stackMembers is the stack's members with the remote each comes from.
func stackMembers(
	cfg config.Config,
	remotes map[string]string,
) []release.Member {
	stacked := cfg.Stack()
	members := make([]release.Member, count.Nothing, len(stacked))
	for _, member := range stacked {
		name := member.Name()
		taken := release.Taken(name, member.Ref(), remotes[name],
			cfg.IsTheirs(name))
		members = append(members, taken)
	}
	return members
}

// assembly is what tellStacked reads of a release.Stacked, so a test
// can hand it one of its own.
type assembly interface {
	Skipped() bool
	Commit() string
	Members() []string
	Work() string
	Patched(member string) int
}

// tellStacked says what the assembly produced: dist already holding
// the tree, or the commit prepared, its workspace and how to send it.
func tellStacked(
	out io.Writer,
	name, tag, from, dist string,
	res assembly,
) {
	commit := repo.Short(res.Commit())
	if res.Skipped() {
		fmt.Fprintf(out,
			"stack %s %s: dist already has this tree at %s\n",
			name, tag, commit)
		return
	}
	members := len(res.Members())
	fmt.Fprintf(out,
		"stack %s %s: %d members from their %ss, prepared %s\n",
		name, tag, members, from, commit)
	if res.Work() != "" {
		members := strings.Join(res.Members(), " ")
		fmt.Fprintln(out, "  go.work: "+members)
	}
	for _, member := range res.Members() {
		if laid := res.Patched(member); count.Positive(laid) {
			word := "patches"
			if laid == count.One {
				word = "patch"
			}
			fmt.Fprintf(out, "  %s: %d %s laid\n", member, laid,
				word)
		}
	}
	fmt.Fprintf(
		out,
		"send it with: game release push %s, to dist (clean): %s\n",
		tag,
		dist,
	)
}

// prefixed adds the member's directory to each allow line.
func prefixed(name string, rules []config.Rule) []config.Rule {
	out := make([]config.Rule, count.Nothing, len(rules))
	for _, rule := range rules {
		if file, lints, said := verbWord(rule.Args()); said &&
			rule.Name() == "allow" {
			under := append([]string{name + "/" + file}, lints...)
			rule = config.Ruled(rule.Name(), under...)
		}
		out = append(out, rule)
	}
	return out
}

// memberRules is the strictest of the stack's own lint and every member's,
// with each member's allowances moved under its name. a .game that does not
// parse adds none.
func memberRules(cfg config.Config,
	games map[string]string) (lint.Lint, error) {
	sets := [][]config.Rule{cfg.Lint()}
	for name, game := range games {
		var member config.Config
		if err := member.Parse(
			strings.NewReader(game), ".game", "", false,
		); err != nil {
			continue
		}
		rules := prefixed(name, member.Lint())
		sets = append(sets, rules)
	}
	return lint.Describe(config.Strictest(sets...))
}

// lintExported exports a commit to a scratch dir and runs rules on it.
func lintExported(
	here repo.Repo,
	commit string,
	rules lint.Lint,
) ([]lint.Finding, error) {
	dir, err := os.MkdirTemp("", "game-stack-lint")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := export(here, commit, dir); err != nil {
		return nil, err
	}
	return rules.Run(dir)
}

// placed is a path, asked whether it sits under a member's directory.
type placed string

// under is whether the path is under the member's directory.
func (path placed) under(name string) bool {
	return strings.HasPrefix(string(path), name+"/")
}

// notTheirs is the findings less those in a member marked theirs, which
// keeps its own style and is not held to ours, and how many those were.
// a staticcheck finding names its file at the start of its text.
func notTheirs(
	found []lint.Finding, theirs []string,
) ([]lint.Finding, int) {
	var kept []lint.Finding
	var left int
	for _, finding := range found {
		where := finding.File()
		if finding.Rule() == "staticcheck" {
			where = finding.Text()
		}
		if slices.ContainsFunc(theirs, placed(where).under) {
			left++
			continue
		}
		kept = append(kept, finding)
	}
	return kept, left
}

// stackLint holds the assembled commit to every member's rules. it exports
// and lints the tree, says every finding, and refuses the stack on any
// unless meant. a member marked theirs is held to its own rules only.
func stackLint(
	here repo.Repo,
	cfg config.Config,
	commit string,
	games map[string]string,
	mean bool,
	out io.Writer,
) error {
	rules, err := memberRules(cfg, games)
	if err != nil {
		return err
	}
	if rules.Empty() {
		return nil
	}
	found, err := lintExported(here, commit, rules)
	if err != nil {
		return err
	}
	found, left := notTheirs(found, cfg.Theirs())
	if count.Positive(left) {
		fmt.Fprintf(out, "stack lint: %d in members marked theirs, "+
			"left to their own rules\n", left)
	}
	return stackVerdict(found, mean, out)
}

// stackVerdict says each finding and the counts, and refuses the stack
// on any unless meant.
func stackVerdict(found []lint.Finding, mean bool, out io.Writer) error {
	for _, finding := range found {
		fmt.Fprintln(out, finding)
	}
	fix, look, err := lint.Verdict(found)
	if err == nil {
		fmt.Fprintln(out, "stack lint: clean, by every member's rules")
		return nil
	}
	fmt.Fprintf(out, "stack lint: %d to fix, %d to look at, by every "+
		"member's rules\n", fix, look)
	if mean {
		return nil
	}
	return fmt.Errorf(
		"the assembly is not clean by the rules its members keep; " +
			"pay it in the members, or say --i-mean-it",
	)
}

// export writes a commit's tree into a directory.
func export(here repo.Repo, commit, dir string) error {
	return exportUsing(here, commit, dir, exec.Command)
}

// exportUsing is export with the process factory given.
func exportUsing(
	here repo.Repo,
	commit, dir string,
	command func(string, ...string) *exec.Cmd,
) error {
	archive := command("git", "-C", here.Dir(), "archive", commit)
	untar := command("tar", "-x", "-C", dir)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	untar.Stdin = pipe
	var said strings.Builder
	untar.Stderr = &said
	if err := untar.Start(); err != nil {
		return err
	}
	archiveErr := archive.Run()
	tarErr := untar.Wait()
	var refused *exec.ExitError
	if errors.As(archiveErr, &refused) && refused.Exited() {
		return archiveErr
	}
	if tarErr != nil {
		text := strings.TrimSpace(said.String())
		return fmt.Errorf("tar: %s: %w", text, tarErr)
	}
	return archiveErr
}

// stuckPush refuses release push on a claimed repository.
func stuckPush(here repo.Repo, cfg config.Config) error {
	mine, _ := cfg.Mine()
	branch, err := onBranch(here)
	if err != nil {
		return err
	}
	if mine.Road() != "" {
		road, err := roadFor(here, cfg)
		if err != nil {
			return err
		}
		behind, err := sticky.Behind(here, road, branch)
		if err != nil {
			return fmt.Errorf(
				"stuck, and the road could not be read: %v",
				err,
			)
		}
		if count.Positive(behind) {
			return mergeFirst()
		}
	}
	return fmt.Errorf(
		"stuck: unstick first, then release again, so the flat " +
			"commit carries no trace of the claim",
	)
}

// mergeFirst is the error when the road is ahead of a claimed tree.
func mergeFirst() error {
	return fmt.Errorf(
		"cannot un-stick, sticker has changes which require " +
			"merge; pull or build first",
	)
}

// releasePush sends the prepared commit and its tag to dist.
func releasePush(
	here repo.Repo,
	cfg config.Config,
	tag string,
	publish bool,
	out io.Writer,
	made stamp,
) error {
	if _, stuck, err := sticky.Stuck(here.Dir()); err != nil {
		return err
	} else if stuck {
		return stuckPush(here, cfg)
	}
	name, dist, err := distFor(cfg, noDistForRelease)
	if err != nil {
		return err
	}
	if err := guard(here, dist, out); err != nil {
		return err
	}
	if err := made.stamped(); err != nil {
		return err
	}
	if here.Leaves(dist) && !publish {
		return fmt.Errorf(
			"%s is not a path on this machine, so this push "+
				"publishes. that is a deliberate act and it "+
				"wants the word: game release push %s "+
				"--publish. preparing a release needs no "+
				"such thing",
			dist,
			tag,
		)
	}
	res, err := release.Push(here, release.For(name, dist, tag))
	if err != nil {
		return err
	}
	commit := repo.Short(res.Commit())
	fmt.Fprintf(out, "dist (clean): %s: main at %s, tagged %s\n", dist,
		commit, tag)
	paint(here, cfg)
	return nil
}

// hookMark is the line that says a pre-push hook is game's to rewrite.
const hookMark = "# game: the guard on dist"

// guard installs the pre-push hook that refuses any push to dist that
// did not come from game release push. a pre-push hook that is not
// game's is left alone, and guard says so.
func guard(here repo.Repo, dist string, out io.Writer) error {
	if dist == "" {
		return nil
	}
	dir, err := here.Git("rev-parse", "--git-path", "hooks")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(dir) {
		dir = here.Path(dir)
	}
	path := filepath.Join(dir, "pre-push")
	if hook, err := os.ReadFile(path); err == nil &&
		!strings.Contains(string(hook), hookMark) {
		fmt.Fprintf(
			out,
			"guard: %s is not game's; dist is not guarded here\n",
			path,
		)
		return nil
	}
	script := "#!/bin/sh\n" + hookMark +
		". only game release push sends to it.\n" +
		"dist='" + strings.ReplaceAll(
		dist,
		"'",
		"'\\''",
	) + "'\n" +
		"if [ \"$2\" = \"$dist\" ] && [ \"$" +
		guardName() +
		"\" != 1 ]; then\n" +
		"echo \"refused: $2 is dist; only game release push " +
		"sends there\" >&2\n" +
		"  exit 1\nfi\n"
	if err := os.MkdirAll(dir, folderMode); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(script), runnable)
}

// runnable is the mode a hook is written with: the owner writes, and
// everyone reads and runs it, defined as 0755 in sys/stat.h's bits.
const runnable = 0o755

// guardName is the name of the guard's variable, before its equals
// sign in release.Guard.
func guardName() string {
	name, _, _ := strings.Cut(release.Guard, "=")
	return name
}

// check passes when gofmt has nothing to say, vet passes and the tests
// pass. its exit code is the tests' own, never a pipe's.
func check(here repo.Repo, cfg config.Config) error {
	return checkBy(here, cfg, run, os.Stdout)
}

// checkBy is check, running go and gofmt with by and saying on out.
func checkBy(here repo.Repo, cfg config.Config, by runner,
	out io.Writer) error {
	if _, err := os.Stat(here.Path("go.mod")); err != nil {
		fmt.Fprintln(
			out,
			"check: no go.mod, so nothing for gofmt, vet or go "+
				"test; the test targets are the check here",
		)
		return nil
	}
	gofmt := gofmtFor(here.Dir(), by, os.Stderr)
	said, err := by(here.Dir(), gofmt, "-l", ".")
	if err != nil {
		return err
	}
	if changed := strings.TrimSpace(said); changed != "" {
		return fmt.Errorf("gofmt would change: %s",
			strings.ReplaceAll(changed, "\n", " "))
	}
	if _, err := by(here.Dir(), "go", "vet", "./..."); err != nil {
		return err
	}
	return tests(here.Dir(), cfg, by, out)
}

// tests runs go test with by and says what it said on out. with `rapid
// checks N` in the config, the packages whose tests import
// pgregory.net/rapid run apart from the rest, with that many checks of
// each property.
func tests(dir string, cfg config.Config, by runner,
	out io.Writer) error {
	withRapid := count.Positive(cfg.RapidChecks())
	plain, rapid, err := testable(dir, withRapid, by)
	if err != nil {
		return err
	}
	var failed error
	if count.Positive(len(rapid)) {
		checks := fmt.Sprintf("-rapid.checks=%d", cfg.RapidChecks())
		args := append(append([]string{"test"}, rapid...), checks)
		said, err := by(dir, "go", args...)
		failed = sayTests(out, said, err)
	}
	if count.Positive(len(plain)) {
		// both groups run whatever the first said, so every failure is
		// seen, as one go test ./... would show them.
		args := append([]string{"test"}, plain...)
		said, err := by(dir, "go", args...)
		err = sayTests(out, said, err)
		if failed == nil {
			failed = err
		}
	}
	return failed
}

// sayTests prints go test's lines, less the packages with no tests, and
// keeps its error.
func sayTests(out io.Writer, said string, err error) error {
	for _, line := range worthSaying(said) {
		fmt.Fprintln(out, line)
	}
	return err
}

// worthSaying returns go test's lines less the empty ones and the packages
// with no tests.
func worthSaying(out string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" && !strings.Contains(line, "no test files") {
			lines = append(lines, line)
		}
	}
	return lines
}

// testable lists what go test is given, ./... unless rapid is asked for.
// then the packages whose tests import rapid directly go apart from the
// rest.
func testable(dir string, rapid bool, by runner) (plain,
	withRapid []string, err error) {
	if !rapid {
		return []string{"./..."}, nil, nil
	}
	out, err := by(
		dir,
		"go", "list", "-f",
		"{{.ImportPath}} {{.TestImports}} {{.XTestImports}}",
		"./...",
	)
	if err != nil {
		return nil, nil, err
	}
	plain, withRapid = splitRapid(out)
	return plain, withRapid, nil
}

// splitRapid reads go list's lines, a package and its test imports each,
// into the packages whose tests import rapid and the rest.
func splitRapid(listing string) (plain, withRapid []string) {
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		path, imports, _ := strings.Cut(line, " ")
		switch {
		case path == "":
		case strings.Contains(imports, "pgregory.net/rapid"):
			withRapid = append(withRapid, path)
		default:
			plain = append(plain, path)
		}
	}
	return plain, withRapid
}

// paint shows the repository's own art once.
//
// look at it. somebody drew this project a whole ansi, one cell at a
// time, in escape codes, and put it in art/<project>.ansi (or
// art/header.ansi). it rules. it absolutely rules.
//
// so every single time a build works, it goes on the screen. fuck yeah
// ansi. fuuuuuck yeah.
//
// it only prints to a terminal, so pipes, logs and child games see
// nothing, and NO_COLOR turns it off.
func paint(here repo.Repo, cfg config.Config) {
	painted(here.Dir(), project(here, cfg))
}

// painted shows one directory's art, art/<project>.ansi or
// art/header.ansi, and says whether it found any.
func painted(dir, name string) bool {
	info, err := os.Stdout.Stat()
	if !colour(os.Getenv("NO_COLOR"), info, err) {
		return false
	}
	return paintTo(os.Stdout, dir, name)
}

// colour is whether art goes out at all: not when NO_COLOR is set, and
// only to a terminal, which a character device is and a file or a pipe
// is not.
func colour(noColor string, out os.FileInfo, err error) bool {
	return noColor == "" && err == nil &&
		out.Mode()&os.ModeCharDevice != noMode
}

// noMode is a file mode with no bits set.
var noMode os.FileMode

// paintTo writes a directory's art to out, and says whether it had any.
func paintTo(out io.Writer, dir, name string) bool {
	art, found := artFor(dir, name)
	if found {
		show(out, art)
	}
	return found
}

// artFor is a directory's art: art/<name>.ansi, or else art/header.ansi,
// and whether there was any.
func artFor(dir, name string) ([]byte, bool) {
	for _, at := range []string{name + ".ansi", "header.ansi"} {
		art, err := os.ReadFile(filepath.Join(dir, "art", at))
		if err == nil {
			return art, true
		}
	}
	return nil, false
}

// show writes art, ending it with a newline if it has none of its own.
func show(out io.Writer, art []byte) {
	out.Write(art)
	if count.Positive(len(art)) && !bytes.HasSuffix(art, []byte("\n")) {
		fmt.Fprintln(out)
	}
}

// buildBinaries builds every cmd/* into bin/ with a stamp, for --age and
// --who to read.
//
// the stamp is the commit and its time, never the clock. it names who
// built it and the session lines that end the commits behind it. those
// lines are what the messages say, not a signature.
//
// the tree's path is trimmed, so one builder makes the same bytes from
// the same sources in any tree. -X writes the builder in, so two
// builders make two binaries.
//
// while the repository is claimed, a build first fast-forwards this
// branch to the road's tip as game sticky does. it reports what moved.
// every binary it builds after that is marked sticky build in its own
// colour.
func buildBinaries(here repo.Repo, cfg config.Config) error {
	info, err := os.Stdout.Stat()
	on := colour(os.Getenv("NO_COLOR"), info, err)
	return buildBinariesBy(here, cfg, madeBy(here), run, os.Stdout, on)
}

// madeBy is who is building here: the session id in the environment and
// this machine.
func madeBy(here repo.Repo) string {
	host, _ := os.Hostname()
	return builder(here, os.Getenv("CLAUDE_CODE_SESSION_ID"), host)
}

// buildBinariesBy is buildBinaries built by maker. it runs go with by and
// says on out what it built. sticky builds are in their colour when
// coloured.
func buildBinariesBy(here repo.Repo, cfg config.Config, maker string,
	by runner, out io.Writer, coloured bool) error {
	entries, err := os.ReadDir(here.Path("cmd"))
	if err != nil {
		return fmt.Errorf("no cmd/ directory to build")
	}
	_, stuck, err := sticky.Stuck(here.Dir())
	if err != nil {
		return err
	}
	if stuck {
		if err := syncBuildTo(here, cfg, out); err != nil {
			return err
		}
	}
	commit, stamp := stamped(here, maker)
	os.MkdirAll(here.Path("bin"), folderMode)
	if err := bakeNever(here, cfg, by); err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		program := filepath.Join("bin", name)
		_, err := by(here.Dir(), "go", "build", "-trimpath", "-ldflags",
			stamp, "-o", program, "./cmd/"+name)
		if err != nil {
			return err
		}
		if err := signGo(here.Dir(), program, by); err != nil {
			return err
		}
		verb := "build"
		if stuck {
			verb = "sticky build"
		}
		line := fmt.Sprintf("%s: bin/%s %s", verb, name, commit)
		if stuck {
			line = stickyColour(line, coloured)
		}
		fmt.Fprintln(out, line)
	}
	return nil
}

// bakeNever puts game's configured never-word hashes where go embeds them.
func bakeNever(here repo.Repo, cfg config.Config, by runner) error {
	if project(here, cfg) != "game" {
		return nil
	}
	_, err := by(here.Dir(), "go", "run", "./cmd/never")
	return err
}

// stamped is the commit a build is at, marked dirty when the tree is,
// and the linker flags that write it, its time, its maker and the
// session lines into the binary.
func stamped(here repo.Repo, maker string) (commit, stamp string) {
	commit, _ = here.Git("rev-parse", "--short", "HEAD")
	if clean, _ := here.Clean(); !clean {
		commit += "-dirty"
	}
	when, _ := here.Git("log", "-1", "--format=%cI")
	sids := sessions(here)
	stamp = fmt.Sprintf(
		"-X main.build=%s -X main.built=%s -X 'main.stampedWho=%s' "+
			"-X main.stampedSids=%s -X 'main.who=%s' "+
			"-X main.sids=%s",
		commit, when, maker, sids, maker, sids,
	)
	return commit, stamp
}

// signGo signs a Go binary ad hoc on a Mac, so one built for Intel is
// signed as the linker already signs one for Apple silicon, and game
// harden finds it signed either way. Elsewhere there is no codesign and
// nothing to do.
//
// the hardened runtime is left off: it brings library validation, which
// refuses any dylib a cgo program loads that is not signed by the same
// identity, and ad hoc is no identity at all. harden lets Go binaries
// off that flag for the same reason.
func signGo(dir, program string, by runner) error {
	return signGoOn(dir, program, by, runtime.GOOS)
}

// signGoOn is signGo for one operating system.
func signGoOn(dir, program string, by runner, goos string) error {
	if goos != "darwin" {
		return nil
	}
	_, err := by(dir, signer, "-s", "-", "-f", program)
	return err
}

// syncBuild is the fast-forward a build does while the repository is
// claimed. see README.md, "human-agent pairing".
func syncBuild(here repo.Repo, cfg config.Config) error {
	return syncBuildTo(here, cfg, os.Stdout)
}

// syncBuildTo is syncBuild, saying on out what it merged.
func syncBuildTo(here repo.Repo, cfg config.Config, out io.Writer) error {
	mine, held := cfg.Mine()
	if !held || mine.Road() == "" {
		return nil
	}
	road, err := roadFor(here, cfg)
	if err != nil {
		return err
	}
	branch, err := here.Branch()
	if err != nil {
		return fmt.Errorf("no branch to sync: %w", err)
	}
	tip, moved, err := sticky.Sync(here, road, branch)
	if err != nil {
		return err
	}
	if moved {
		fmt.Fprintf(out, "build: updates from %s merged\n",
			repo.Short(tip))
	}
	return nil
}

// stickyColour returns the line in a sticky build's colour when coloured,
// which colour decides as it does for paint: only to a terminal, and
// never with NO_COLOR set.
func stickyColour(line string, coloured bool) string {
	if !coloured {
		return line
	}
	return "\x1b[36m" + line + "\x1b[0m"
}

// gofmtFor is the gofmt of the toolchain that builds the module at dir.
// go switches to a newer toolchain when go.mod asks for one, and the
// gofmt on PATH does not follow it, so it cannot read the newer syntax.
// with no toolchain to ask, it is the gofmt on PATH.
func gofmtFor(dir string, by runner, errs io.Writer) string {
	root, err := by(dir, "go", "env", "GOROOT")
	if err != nil {
		fmt.Fprintf(errs, "check: go env GOROOT failed, so the "+
			"gofmt on PATH: %v\n", err)
		return "gofmt"
	}
	own := filepath.Join(strings.TrimSpace(root), "bin", "gofmt")
	if _, err := os.Stat(own); err != nil {
		return "gofmt"
	}
	return own
}

// run execs a command in a given directory and returns err if nonzero.
func run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		return out.String(), fmt.Errorf(
			"%s %s: %s",
			name,
			strings.Join(args, " "),
			msg,
		)
	}
	return out.String(), nil
}

// clean is go clean, which knows what go build left behind, plus bin/,
// which go does not know about because it is ours.
func clean(here repo.Repo, cfg config.Config, cache bool) error {
	return cleanBy(here, cfg, cache, run, os.Stdout)
}

// runner is run, or a test's stand-in for it.
type runner func(dir, name string, args ...string) (string, error)

// cleanBy is clean, running go with by and saying what went on out.
func cleanBy(here repo.Repo, cfg config.Config, cache bool, by runner,
	out io.Writer) error {
	for _, dir := range cleanable(here) {
		if _, err := by(dir, "go", "clean", "./..."); err != nil {
			return err
		}
		if !cache {
			continue
		}
		if _, err := by(
			dir, "go", "clean", "-cache", "-testcache",
		); err != nil {
			return err
		}
	}
	var gone int
	dirs := append([]string{here.Dir()}, members(here, cfg)...)
	for _, dir := range dirs {
		swept, err := sweepBin(here, filepath.Join(dir, "bin"))
		if err != nil {
			return err
		}
		gone += swept
	}
	if err := sweepSum(here, out); err != nil {
		return err
	}
	fmt.Fprintf(
		out,
		"clean: %d files of build output away%s\n",
		gone,
		map[bool]string{
			true:  ", caches forgotten",
			false: "",
		}[cache],
	)
	return nil
}

// sweepSum removes the go.work.sum the build wrote. a tracked one remains.
func sweepSum(here repo.Repo, out io.Writer) error {
	sum := here.Path("go.work.sum")
	if _, err := os.Stat(sum); err != nil {
		return nil
	}
	tracked, err := here.Git("ls-files", "--", "go.work.sum")
	if err != nil {
		return err
	}
	if tracked != "" {
		fmt.Fprintln(out, "clean: go.work.sum is tracked; left")
		return nil
	}
	if err := os.Remove(sum); err != nil {
		return err
	}
	fmt.Fprintln(
		out, "clean: go.work.sum away; the build writes it again",
	)
	return nil
}

// sweepBin removes build files from one bin/ and returns the count.
// it checks git first to avoid deleting source or tracked files.
// bin/ is removed only if untracked and empty after.
func sweepBin(here repo.Repo, bin string) (int, error) {
	if _, err := os.Stat(bin); err != nil {
		return count.Nothing, nil
	}
	entries, err := os.ReadDir(bin)
	if err != nil {
		return count.Nothing, err
	}
	var gone int
	for _, entry := range entries {
		at := filepath.Join(bin, entry.Name())
		tracked, err := here.Git("ls-files", "--", at)
		if err != nil {
			return gone, err
		}
		if strings.TrimSpace(tracked) != "" {
			continue
		}
		if err := os.RemoveAll(at); err != nil {
			return gone, err
		}
		gone++
	}
	left, err := os.ReadDir(bin)
	if err != nil {
		return gone, fmt.Errorf("%s, swept: %w", bin, err)
	}
	if count.None(len(left)) {
		if err := os.Remove(bin); err != nil {
			return gone, err
		}
	}
	return gone, nil
}

// cleanable returns directories go clean can work in.
// a stack checkout has no module at its root, only a go.work.
// go clean ./... fails there, so game cleans each member instead.
// a tree with no go has nothing for go to clean.
func cleanable(here repo.Repo) []string {
	if _, err := os.Stat(here.Path("go.mod")); err == nil {
		return []string{here.Dir()}
	}
	work, err := os.ReadFile(here.Path("go.work"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(work), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "use ")
		line = strings.Trim(line, "()")
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "./") {
			continue
		}
		out = append(out, here.Path(line))
	}
	return out
}

// members is the stack members this repository declares, as directories,
// so their build output is cleaned with the root's.
func members(here repo.Repo, cfg config.Config) []string {
	var out []string
	for _, member := range cfg.Stack() {
		dir := here.Path(member.Name())
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		out = append(out, dir)
	}
	return out
}

// described is the lint the config describes.
func described(cfg config.Config) (lint.Lint, error) {
	return lint.Describe(cfg.Lint())
}

// ownerOnly is the mode a settings file is written with: the owner
// alone reads and writes it, defined as 0600 in sys/stat.h's bits.
const ownerOnly = 0o600

// nicest is the nice level a target's steps run at: the lowest
// priority nice defines, as its own docs give it.
const nicest = "19"

// resolved is where a step runs and what it runs. Relative directories
// are taken from root. Programs written with a slash and not absolute
// are taken from the directory.
func resolved(root string, step config.Step) (string, string, []string) {
	dir := step.Dir()
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	prog, rest, _ := verbWord(step.Args())
	if strings.Contains(prog, "/") && !filepath.IsAbs(prog) {
		prog = filepath.Join(dir, prog)
	}
	return dir, prog, rest
}

// runStep runs one step in its directory, under nice. output is in the log.
// a relative path is joined to the step's directory first.
func runStep(root string, step config.Step, logf io.Writer) error {
	dir, prog, rest := resolved(root, step)
	args := append([]string{"-n", nicest, prog}, rest...)
	cmd := exec.Command("nice", args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = logf, logf
	return cmd.Run()
}

// targetNamed is the target of that name in the config, or an error
// naming the ones described.
func targetNamed(cfg config.Config, name string) (*config.Target, error) {
	found, err := named(cfg.Targets(), name)
	if err != nil {
		return nil, fmt.Errorf("no target called %q; described: %s",
			name, err)
	}
	return found, nil
}

// named is the target of that name among those described, or an error
// naming the ones that are.
func named(targets []config.Target, name string) (*config.Target, error) {
	for i := range targets {
		if targets[i].Name() == name {
			return &targets[i], nil
		}
	}
	names := make([]string, count.Nothing, len(targets))
	for _, other := range targets {
		names = append(names, other.Name())
	}
	return nil, errors.New(strings.Join(names, ", "))
}

// buildTarget is game build NAME. it runs the target's steps in order, each
// in its own directory, keeps the output in bin/NAME.log and shows its tail.
func buildTarget(here repo.Repo, cfg config.Config, name string) error {
	return buildTargetTo(here, cfg, name, os.Stdout)
}

// buildTargetTo is buildTarget, saying its steps and the log's tail on out.
func buildTargetTo(here repo.Repo, cfg config.Config, name string,
	out io.Writer) error {
	target, err := targetNamed(cfg, name)
	if err != nil {
		return err
	}
	os.MkdirAll(here.Path("bin"), folderMode)
	logPath := here.Path("bin", name+".log")
	logf, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logf.Close()
	start := time.Now()
	steps := target.Steps()
	for i, step := range steps {
		said := strings.Join(step.Args(), " ")
		at := count.Next(i)
		fmt.Fprintf(out, "build %s: step %d of %d: %s\n", name, at,
			len(steps), said)
		fmt.Fprintf(logf, "== step %d, in %s: %s\n", at, step.Dir(),
			said)
		if err := runStep(here.Dir(), step, logf); err != nil {
			tail(out, logPath, onFailure)
			return fmt.Errorf(
				"build %s: step %d failed after %s; "+
					"the log is %s",
				name,
				at,
				time.Since(start).Round(time.Second),
				logPath,
			)
		}
	}
	tail(out, logPath, onSuccess)
	fmt.Fprintf(
		out,
		"build %s: done in %s; the log is %s\n",
		name,
		time.Since(start).Round(time.Second),
		logPath,
	)
	return nil
}

// tail writes the last n lines of a file to out, after a build.
func tail(out io.Writer, path string, last int) {
	body, err := os.ReadFile(path)
	if err != nil {
		return
	}
	text := strings.TrimRight(string(body), "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > last {
		lines = lines[len(lines)-last:]
	}
	for _, line := range lines {
		fmt.Fprintln(out, "  "+line)
	}
}

// how much of a target's log is shown: twelve lines after a failure
// and four after a success, as picked here.
const (
	onFailure = 12 // defined here: enough to see why
	onSuccess = 4  // defined here: enough to see it ended
)

// runNamed runs a named run in your terminal, without tmux's variables.
func runNamed(here repo.Repo, cfg config.Config, name string) error {
	run, err := named(cfg.Runs(), name)
	if err != nil {
		return fmt.Errorf("no run called %q; described: %s", name, err)
	}
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMUX=") ||
			strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		env = append(env, kv)
	}
	for _, step := range run.Steps() {
		dir, prog, rest := resolved(here.Dir(), step)
		cmd := exec.Command(prog, rest...)
		cmd.Dir, cmd.Env = dir, env
		cmd.Stdin, cmd.Stdout = os.Stdin, os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			said := strings.Join(step.Args(), " ")
			return fmt.Errorf("run %s: %s: %w", name, said, err)
		}
	}
	return nil
}

// told writes events as JSON lines for the parent to read.
type told struct{ enc *json.Encoder }

// step is one event: a step starting, passing or failing.
func (say told) step(step, state, kind, text string, counted int) {
	say.enc.Encode(stack.Happened(step, state, kind, text, counted))
}

// failed reports a step's failure and returns the error. a check that
// failed in go test is reported as the test step, so the parent can tell
// flaky from failing.
func (say told) failed(step string, err error) error {
	text := err.Error()
	if step == "check" && strings.Contains(text, "go test") {
		step = "test"
	}
	say.step(step, "fail", stack.Kind(text), text, noCount)
	return err
}

// verify is game verify, the child a stack member runs. it runs the
// check where there is a module, the test targets, the build where
// there are commands, and the lint count, and tells each as it happens.
//
// with lint harden in the .game, the harden layers beyond the source
// rules run over what the build left, and a finding fails the member.
func verify(here repo.Repo, cfg config.Config, out io.Writer) error {
	say := told{json.NewEncoder(out)}
	if _, err := os.Stat(here.Path("go.mod")); err == nil {
		say.step("check", "start", "", "", noCount)
		if err := check(here, cfg); err != nil {
			return say.failed("check", err)
		}
		say.step("check", "ok", "", "", noCount)
	}
	for _, target := range cfg.Targets() {
		if !testTarget(target.Name()) {
			continue
		}
		say.step("test", "start", "", target.Name(), noCount)
		if err := buildTarget(here, cfg, target.Name()); err != nil {
			return say.failed("test", err)
		}
		say.step("test", "ok", "", target.Name(), noCount)
	}
	if st, err := os.Stat(here.Path("cmd")); err == nil &&
		st.IsDir() {
		say.step("build", "start", "", "", noCount)
		if err := buildBinaries(here, cfg); err != nil {
			return say.failed("build", err)
		}
		say.step("build", "ok", "", "", noCount)
	}
	rules, err := described(cfg)
	if err != nil {
		return say.failed("lint", err)
	}
	found, err := rules.Run(here.Dir())
	if err != nil {
		return say.failed("lint", err)
	}
	say.step("lint", "ok", "", "", len(found))
	if !rules.Harden() {
		return nil
	}
	say.step("harden", "start", "", "", noCount)
	beyond, err := hardenBeyondSource(here.Dir(), run)
	if err != nil {
		return say.failed("harden", err)
	}
	if hard := append(hardenOf(found), beyond...); !none(hard) {
		return say.failed("harden", firstToFix(hard))
	}
	say.step("harden", "ok", "", "", noCount)
	return nil
}

// noCount is the count of a step that counts nothing.
var noCount int

// firstToFix is the error a harden step fails with: how many there are
// to fix, and the first of them, so the stack's table has something to
// read.
func firstToFix(hard []lint.Finding) error {
	first := 0 // defined by go: a list begins at nothing
	return fmt.Errorf("harden: %d to fix; %s", len(hard), hard[first])
}

// selfVerify is the child a stack member gets: this same game, running
// verify in the member's directory.
func selfVerify() (func(string) *exec.Cmd, error) {
	return selfVerifyUsing(os.Executable)
}

// selfVerifyUsing is selfVerify with the executable lookup given.
func selfVerifyUsing(
	executable func() (string, error),
) (func(string) *exec.Cmd, error) {
	self, err := executable()
	if err != nil {
		return nil, err
	}
	return verifier(self).child, nil
}

// verifier is game itself, at its own path, to run verify in a member.
type verifier string

// child is the verify a member gets, which runs in its directory.
func (program verifier) child(string) *exec.Cmd {
	return exec.Command(string(program), "verify")
}

// stackFlags reads build stack's flags for where members come from, how
// many build at once, and whether they are already here.
func stackFlags(args []string) (from string, parallel int, here bool) {
	fs := flag.NewFlagSet("build stack", flag.ExitOnError)
	fs.StringVar(
		&from,
		"from",
		"dist",
		"where members come from: road or dist",
	)
	fs.IntVar(
		&parallel,
		"parallel",
		count.One,
		"members built at once; one at a time shows each member's "+
			"art as it is reached and stops at a failure",
	)
	fs.BoolVar(
		&here,
		"here",
		false,
		"build the members that are already in this clone, "+
			"which is what an assembled stack is",
	)
	fs.Parse(args)
	return from, parallel, here
}

// memberURL is where a member comes from, its remote of the kind asked
// for and checked to be there, or why there is none.
func memberURL(
	here repo.Repo,
	cfg config.Config,
	remotes map[string]string,
	from string,
) func(string) (string, error) {
	lookup := &remoting{here: here, cfg: cfg, remotes: remotes, from: from}
	return lookup.url
}

// remoting is where the stack's members come from: the remotes your
// config names, and which kind was asked for.
type remoting struct {
	here    repo.Repo
	cfg     config.Config
	remotes map[string]string
	from    string
}

// url is where one member lives, or why it cannot be found.
func (lookup *remoting) url(name string) (string, error) {
	if found := lookup.remotes[name]; found != "" {
		return found, there(found, lookup.here.Dir(), name,
			lookup.cfg.Dotfile())
	}
	return "", fmt.Errorf(
		"no %s for %s in %s",
		lookup.from,
		name,
		lookup.cfg.Dotfile(),
	)
}

// stackTries is how many attempts a member's clone or build gets at a
// failure on the network, defined here as the first and two more.
const stackTries = 3

// buildStack is game build stack. it builds every member, cloned at its ref
// or taken from this clone with --here, one at a time unless --parallel
// says more. the rows say what passed.
func buildStack(
	here repo.Repo,
	cfg config.Config,
	args []string,
	out io.Writer,
	child func(string) *exec.Cmd,
) error {
	from, parallel, inPlace := stackFlags(args)
	if stacked := cfg.Stack(); count.None(len(stacked)) {
		return fmt.Errorf(
			"no stack described; add \"stack NAME REF\" " +
				"lines to .game",
		)
	}
	var remotes map[string]string
	if !inPlace {
		// in place the members sit under their names; none is fetched
		var err error
		if remotes, err = remotesFrom(cfg, from); err != nil {
			return err
		}
	}
	members := membersOf(cfg)
	where, work := from, here.Path("bin", "stack")
	url := memberURL(here, cfg, remotes, from)
	if inPlace {
		where, work, url = "this clone", here.Dir(), nil
	}
	fmt.Fprintf(out, "stack: %d members from %s\n", len(members), where)
	patching := laid(here.Dir(), cfg.Patches())
	asked := stack.Build(members, work, child).From(url).
		Patching(patching).InParallel(parallel).Saying(out).
		Trying(stackTries).Clearing(stack.Clears(out)).
		Sleeping(time.Sleep).Jittered(rand.Int63n).
		Fixing(stack.GivingUp())
	rows, err := stack.Run(context.Background(), asked)
	if err != nil {
		return err
	}
	stack.Table(out, rows)
	return tellBuilt(out, rows, work)
}

// laid lays a member's patches on its fresh clone and says how many it
// laid. they come from patches/NAME in the stack at root or from NAME
// under a directory .game names.
func laid(root string, dirs []string) func(stack.Member, string) (int,
	error) {
	return laying{root: root, dirs: dirs}.lay
}

// laying is where a stack keeps its patches: its root, and the
// directories .game names beside patches/.
type laying struct {
	root string
	dirs []string
}

// lay lays one member's patches on its clone at dir and says how many.
func (patches laying) lay(member stack.Member, dir string) (int, error) {
	series, err := patch.Series(patches.root, patches.dirs, member.Name())
	if err != nil {
		return count.Nothing, err
	}
	results, err := patch.Lay(dir, series, false)
	if err != nil {
		return count.Nothing, err
	}
	return len(results), nil
}

// patchVerb is game patch. each stack member with patches is cloned fresh
// into bin/patch and its patches are laid, or with --dry checked and taken
// back. it says whether the patches still land at the refs the stack
// names, without building anything. NAME... limits it to those members.
func patchVerb(here repo.Repo, cfg config.Config, args []string) error {
	return patchTo(here, cfg, args, os.Stdout)
}

// patchTo is game patch, saying what became of each patch on out.
func patchTo(here repo.Repo, cfg config.Config, args []string,
	out io.Writer) error {
	fs := flag.NewFlagSet("patch", flag.ExitOnError)
	from := fs.String("from", "dist",
		"where members come from: road or dist")
	dry := fs.Bool("dry", false, "say whether each patch would land, "+
		"and leave each clone as it was")
	fs.Parse(args)
	if stacked := cfg.Stack(); count.None(len(stacked)) {
		return fmt.Errorf("no stack described; add " +
			"\"stack NAME REF\" lines to .game")
	}
	remotes, err := remotesFrom(cfg, *from)
	if err != nil {
		return err
	}
	url := memberURL(here, cfg, remotes, *from)
	asked := map[string]bool{}
	for _, name := range fs.Args() {
		asked[name] = true
	}
	var failed int
	for _, member := range membersOf(cfg) {
		if count.Positive(len(asked)) && !asked[member.Name()] {
			continue
		}
		delete(asked, member.Name())
		err := patchMember(here, cfg.Patches(), member, url, *dry, out)
		if err != nil {
			fmt.Fprintln(out, err)
			failed++
		}
	}
	for name := range asked {
		return fmt.Errorf("patch: %s is not a member of this stack",
			name)
	}
	if count.Positive(failed) {
		return fmt.Errorf("patch: %d member's patches did not land",
			failed)
	}
	return nil
}

// patchMember clones one member fresh into bin/patch at its ref and lays
// its patches there, from wherever the stack keeps them, saying of each
// whether it landed.
func patchMember(here repo.Repo, dirs []string, member stack.Member,
	url func(string) (string, error), dry bool, out io.Writer) error {
	series, err := patch.Series(here.Dir(), dirs, member.Name())
	if err != nil {
		return err
	}
	if count.None(len(series)) {
		fmt.Fprintf(out, "patch %s: no patches\n", member.Name())
		return nil
	}
	from, err := url(member.Name())
	if err != nil {
		return err
	}
	dir := here.Path("bin", "patch", member.Name())
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	said, err := exec.Command("git", "clone", "--quiet", "--depth", "1",
		"--branch", member.Ref(), from, dir).CombinedOutput()
	if err != nil {
		why := strings.TrimSpace(string(said))
		return fmt.Errorf("patch %s: clone at %s: %s", member.Name(),
			member.Ref(), why)
	}
	results, err := patch.Lay(dir, series, dry)
	landing := landed(dry, err == nil)
	for _, each := range results {
		if each.Landed() {
			file := filepath.Base(each.Patch())
			fmt.Fprintf(out, "patch %s: %s %s\n", member.Name(),
				file, landing)
		}
	}
	return err
}

// landed is what game patch says of a patch that applied: whether it
// stays, and whether the series it was in stayed with it.
func landed(dry, whole bool) string {
	switch {
	case dry && whole:
		return "would land"
	case dry:
		return "would land, but the series would not"
	case whole:
		return "lands"
	}
	return "landed, then taken back with the series"
}

// membersOf returns the stack's members in .game's order.
func membersOf(cfg config.Config) []stack.Member {
	stacked := cfg.Stack()
	members := make([]stack.Member, count.Nothing, len(stacked))
	for _, member := range stacked {
		named := stack.Named(member.Name(), member.Ref())
		members = append(members, named)
	}
	return members
}

// tellBuilt shows each member's art, which a child writing to a pipe could
// not, and counts what built clean. any that did not is the error.
func tellBuilt(out io.Writer, rows []stack.Row, work string) error {
	var clean int
	for _, row := range rows {
		if row.Result() == "ok" && count.None(row.Lint()) {
			clean++
		}
	}
	for _, row := range rows {
		paintOk(work, row.Name(), row.Result(), painted)
	}
	fmt.Fprintf(out, "stack: %d of %d built clean\n", clean, len(rows))
	if clean != len(rows) {
		return fmt.Errorf(
			"stack: %d did not build clean",
			len(rows)-clean,
		)
	}
	return nil
}

// buildDocker builds an image of this repository, starting docker with
// start and saying what it built on out.
func buildDocker(here repo.Repo, cfg config.Config,
	start func(string, ...string) *exec.Cmd, out io.Writer) error {
	if _, err := os.Stat(here.Path("Dockerfile")); err != nil {
		return fmt.Errorf("no Dockerfile to build")
	}
	name := project(here, cfg)
	commit, _ := here.Git("rev-parse", "--short", "HEAD")
	if clean, _ := here.Clean(); !clean {
		commit += "-dirty"
	}
	image := name + ":" + commit
	cmd := start("docker", "build", "-t", image, ".")
	cmd.Dir, cmd.Stdout, cmd.Stderr = here.Dir(), out, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build %s: %w", image, err)
	}
	fmt.Fprintf(out, "build docker: %s\n", image)
	if needs := cfg.Needs(); count.None(len(needs)) {
		return nil
	}
	var yaml strings.Builder
	fmt.Fprintf(
		&yaml,
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  "+
			"name: %s-settings\ndata:\n",
		name,
	)
	for _, kv := range cfg.Env() {
		key, value, _ := strings.Cut(kv, "=")
		fmt.Fprintf(&yaml, "  %s: %q\n", key, value)
	}
	os.MkdirAll(here.Path("bin"), folderMode)
	path := here.Path("bin", name+"-settings.yaml")
	settings := []byte(yaml.String())
	if err := os.WriteFile(path, settings, ownerOnly); err != nil {
		return err
	}
	fmt.Fprintf(
		out,
		"build docker: the settings it needs are in %s, for "+
			"the cluster, not the image\n",
		path,
	)
	return nil
}

// paintOk shows a member's art with paint when it built, and shows
// nothing of one that did not: a failure is told in the table.
func paintOk(work, name, result string, paint func(dir, name string) bool) {
	if result != "ok" {
		return
	}
	paint(filepath.Join(work, name), name)
}
