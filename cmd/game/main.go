// game is the verbs a go project needs that go itself does not know it
// needs: check, build, release, bounce and sweep. it runs in any
// shell and needs only git and go. there is no makefile, because game
// does everything a makefile would.
//
// a few verbs run one tool more, and only when asked: tar for release,
// staticcheck for lint, gosec and govulncheck for harden (findings when
// they are missing), nice for a target or a mutants run, docker for
// build docker.
//
// on a Mac, aslr and leaks run otool and leaks, and build runs codesign.
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
//	game aslr [go]                build, then check the binaries are
//	                              position-independent; no word checks
//	                              every Mach-O already in bin/
//	game leaks go                 build, run under leaks, which catches
//	                              what cgo holds
//	game clean [--cache]          go clean, and bin/ away; --cache also
//	                              forgets go's build and test caches
//	game lint [--look]            the rules in your config's `lint`
//	                              lines. wide code and shouting are
//	                              counted, and --look lists them
//	game harden                   Go source and modules, held to the
//	                              hardened default
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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
	"github.com/janearc/puzzlemouth/pzm"
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

// a verb is what game runs for its word. it reads its words and flags from
// the pzm call.
type verb func(here repo.Repo, cfg config.Config, call pzm.Call) error

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

// ignoringWords is a verb that runs whatever words it was given.
type ignoringWords struct {
	do func(repo.Repo, config.Config, io.Writer) error
}

// run does the verb on the call's output and ignores the words.
func (ignoring ignoringWords) run(here repo.Repo, cfg config.Config,
	call pzm.Call) error {
	return ignoring.do(here, cfg, call.Out())
}

// withoutWords is a verb that ignores any given words.
func withoutWords(
	do func(repo.Repo, config.Config, io.Writer) error,
) verb {
	return ignoringWords{do: do}.run
}

// running is do with the runner game uses for real, which is run.
func running(
	do func(repo.Repo, config.Config, runner, io.Writer) error,
) func(repo.Repo, config.Config, io.Writer) error {
	return func(here repo.Repo, cfg config.Config, out io.Writer) error {
		return do(here, cfg, run, out)
	}
}

// refusingWords is a verb that takes no words: any word is refused, and
// the refusal says what the verb does.
type refusingWords struct {
	name, about string
	do          func(repo.Repo, config.Config, io.Writer) error
}

// run does the verb on the call's output when it was given no words.
func (refusing refusingWords) run(here repo.Repo, cfg config.Config,
	call pzm.Call) error {
	err := noWords(refusing.name, refusing.about, call.Args())
	if err != nil {
		return err
	}
	return refusing.do(here, cfg, call.Out())
}

// bareVerb is a verb that takes no words.
func bareVerb(
	name, about string,
	do func(repo.Repo, config.Config, io.Writer) error,
) verb {
	return refusingWords{name: name, about: about, do: do}.run
}

// mutantsVerb is game mutants: Go, by game's own mutator.
func mutantsVerb(here repo.Repo, _ config.Config, call pzm.Call) error {
	told, err := mutantsAsked(call)
	if err != nil {
		return err
	}
	return mutate(here, told, call)
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

// runner is run, or a test's stand-in for it.
type runner func(dir, name string, args ...string) (string, error)

// noCount is the count of a step that counts nothing.
var noCount int
