// game is the verbs a go project needs that go itself does not know it
// needs: check, build, release, bounce, sweep and version. it runs in any
// shell and needs only git and go. there is no makefile, because game
// does everything a makefile would.
//
// game only prints text. if it ever draws in a window, another program
// will do the drawing.
//
//	game check                    gofmt -l, go vet, go test, exit code kept
//	game build                    every cmd/* into bin/, stamped with the
//	                              commit and the commit's time; the
//	                              same sources build the same bytes
//	game build NAME               a target the config describes: its steps
//	                              in order, in its directory, under nice,
//	                              the output kept in bin/NAME.log
//	game clean [--cache]          go clean, and bin/ away; --cache also
//	                              forgets go's build and test caches
//	game lint [--look]            the rules in your config's `lint`
//	                              lines. wide code and shouting are
//	                              counted, and --look lists them
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
//	game release push TAG         pushes that commit and its tag to dist,
//	                              the clean remote; nothing else goes there
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
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/janearc/game/internal/bounce"
	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/lint"
	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/stack"
	"github.com/janearc/game/internal/sticky"
	"github.com/janearc/game/internal/sweep"
)

// game build and bootstrap stamp build with the commit, and built with
// the commit's time.
var (
	build = "dev"
	built = ""
)

// cwd is the directory game was run in, which is the repository.
func cwd() string {
	d, _ := os.Getwd()
	return d
}

// neverWords returns the never-words this run sweeps with. cmd/never
// bakes them in when game is built. the tests build from a tree with
// none, so they set made-up words here.
var neverWords = sweep.Baked

// main is the verb table; each verb is a function below.
func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	r := repo.Repo{Dir: cwd()}
	cfg, err := config.Load(config.Whose(), os.Getenv("HOME"), r.Dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "game:", err)
		os.Exit(2)
	}
	// the settings this repository needs are copied from your config into
	// the environment of whatever game runs, and no others. a build or a
	// run that needs a setting you haven't set refuses, and says why the
	// setting is needed.
	for _, kv := range cfg.Env() {
		k, v, _ := strings.Cut(kv, "=")
		os.Setenv(k, v)
	}
	if verb := os.Args[1]; verb == "build" || verb == "run" {
		if miss := cfg.Missing(); len(miss) > 0 {
			for _, m := range miss {
				fmt.Fprintf(
					os.Stderr,
					"game: %s is needed: %s\n",
					m.Name,
					m.Why,
				)
			}
			fmt.Fprintln(
				os.Stderr,
				"game: set it in "+
					"~/.config/game/config as \"set "+
					"NAME value\"",
			)
			os.Exit(2)
		}
	}
	switch os.Args[1] {
	case "version", "--age":
		fmt.Println(age())
	case "check":
		err = check(r)
	case "build":
		if len(os.Args) > 2 {
			switch os.Args[2] {
			case "stack":
				err = buildStack(r, cfg, os.Args[3:])
			case "docker":
				err = buildDocker(r, cfg)
			default:
				err = buildTarget(r, cfg, os.Args[2])
			}
			if err == nil {
				paint(r, cfg)
			}
			break
		}
		if err = buildBinaries(r, cfg); err == nil {
			paint(r, cfg)
		}
	case "verify":
		err = verify(r, cfg)
	case "run":
		if len(os.Args) < 3 {
			usage()
			os.Exit(2)
		}
		err = runNamed(r, cfg, os.Args[2])
	case "lint":
		fs := flag.NewFlagSet("lint", flag.ExitOnError)
		count := fs.Bool(
			"count",
			false,
			"the tallies alone, without the findings",
		)
		fs.Bool("look", false, "kept: every finding is listed now")
		fs.Parse(os.Args[2:])
		l, e := described(cfg)
		if e != nil {
			err = e
			break
		}
		if l.Empty() {
			fmt.Fprintln(os.Stderr,
				"lint: hey buddy, i live to lint. where is "+
					"that config? add lint lines to "+
					"~/.config/game/config or .game",
			)
			break
		}
		found, e := l.Run(r.Dir)
		if e != nil {
			err = e
			break
		}
		looks := map[string]int{}
		for _, f := range found {
			if f.Look {
				looks[f.Rule]++
			}
			if !*count {
				fmt.Println(f)
			}
		}
		for rule, n := range looks {
			fmt.Printf("look: %d %s\n", n, rule)
		}
		// lint.Verdict decides the exit code. it is its own function
		// so that a test can call it; see its comment.
		if _, _, e := lint.Verdict(found); e != nil {
			err = e
		} else {
			fmt.Println("lint: clean")
		}
	case "clean":
		fs := flag.NewFlagSet("clean", flag.ExitOnError)
		cache := fs.Bool(
			"cache",
			false,
			"also forget go's build and test caches, "+
				"for a cold run",
		)
		fs.Parse(os.Args[2:])
		err = clean(r, cfg, *cache)
	case "trees":
		mine, held := cfg.Mine()
		if !held {
			fmt.Printf("a local build: %v\n", cfg.NoBlock())
		}
		fmt.Printf("road: %s\n", orNone(mine.Road, cfg.Dotfile))
		fmt.Printf("dist: %s\n", orNone(mine.Dist, cfg.Dotfile))
		err = guard(r, mine.Dist)
	case "push":
		err = pushRoad(r, cfg)
	case "stick":
		err = doStick(r, cfg)
	case "sticky":
		err = doSticky(r, cfg)
	case "unstick":
		err = doUnstick(r, cfg)
	case "release":
		if len(os.Args) > 2 && os.Args[2] == "stack" {
			if len(os.Args) < 4 {
				usage()
				os.Exit(2)
			}
			fs := flag.NewFlagSet("release stack", flag.ExitOnError)
			from := fs.String(
				"from",
				"dist",
				"take each member from its road or its dist",
			)
			mean := fs.Bool(
				"i-mean-it",
				false,
				"assemble even though the lint has things "+
					"to fix or to look at",
			)
			fs.Parse(os.Args[4:])
			err = releaseStack(r, cfg, os.Args[3], *from, *mean)
			break
		}
		if len(os.Args) > 2 && os.Args[2] == "diff" {
			tag := ""
			if len(os.Args) > 3 {
				tag = os.Args[3]
			}
			err = releaseDiff(r, cfg, tag)
			break
		}
		if len(os.Args) > 2 && os.Args[2] == "push" {
			if len(os.Args) < 4 {
				usage()
				os.Exit(2)
			}
			fs := flag.NewFlagSet("release push", flag.ExitOnError)
			publish := fs.Bool(
				"publish",
				false,
				"say it out loud: this dist is a remote and "+
					"pushing to it publishes",
			)
			fs.Parse(os.Args[4:])
			err = releasePush(r, cfg, os.Args[3], *publish)
			break
		}
		fs := flag.NewFlagSet("release", flag.ExitOnError)
		mean := fs.Bool(
			"i-mean-it",
			false,
			"release even though the lint has things to "+
				"fix or to look at",
		)
		if len(os.Args) < 3 || strings.HasPrefix(os.Args[2], "-") {
			usage()
			os.Exit(2)
		}
		fs.Parse(os.Args[3:])
		err = releasePrepare(r, cfg, os.Args[2], *mean)
	case "bounce":
		if cfg.Author == "" {
			err = fmt.Errorf(
				"bounce needs an author: \"author " +
					"NAME\" in ~/.config/game/config",
			)
			break
		}
		var hits []bounce.Hit
		if len(os.Args) > 2 {
			hits, err = bounce.Range(r, os.Args[2], cfg.Author)
		} else {
			hits, err = bounce.Staged(r, cfg.Author)
		}
		if err != nil {
			break
		}
		if len(hits) > 0 {
			fmt.Println(
				"bounce: the author in the third " +
					"person, in added lines:",
			)
			for _, h := range hits {
				fmt.Println("  " + h.Line)
			}
			if os.Getenv("BOUNCE_OK") != "" {
				fmt.Println(
					"bounce: signed off " +
						"(BOUNCE_OK); passing",
				)
			} else {
				err = fmt.Errorf("refused. reword to the " +
					"first person, or " +
					"BOUNCE_OK=1 to sign it off")
			}
		}
	case "sweep":
		rev := "HEAD"
		if len(os.Args) > 2 {
			rev = os.Args[2]
		}
		var hits []sweep.Hit
		hits, err = sweep.History(r, rev)
		if err != nil {
			break
		}
		never := neverWords()
		th, e := sweep.Tree(
			r,
			rev,
			never,
			cfg.Author,
			cfg.SweepAllow,
			cfg.Exclude...)
		if e != nil {
			err = e
			break
		}
		hits = append(hits, th...)
		for _, h := range hits {
			fmt.Printf("sweep: %s in %s\n", h.Kind, h.File)
		}
		// a sweep with no never-words can't check for them, so it
		// never reports clean.
		switch {
		case len(hits) > 0:
			err = fmt.Errorf("%d hit(s)", len(hits))
		case never.Empty():
			err = fmt.Errorf("%s has none of the fixed "+
				"patterns, but this game was built without "+
				"never-words, so none were looked for", rev)
		default:
			fmt.Printf("sweep: %s is clean\n", rev)
		}
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "game:", err)
		os.Exit(1)
	}
}

// usage prints the usage. the caller exits.
func usage() {
	fmt.Fprintln(
		os.Stderr,
		"game check | build [NAME] | run NAME | clean "+
			"[--cache] | lint [--look] |\n"+
			"trees | push | stick | sticky | unstick | "+
			"release TAG [--i-mean-it] | release push TAG | "+
			"release diff |\n"+
			"     bounce [RANGE] | sweep [REV] | version",
	)
}

// age says which commit this binary was built from, and how old that
// commit is.
//
// built is the commit's time, not the time of the build. the same sources
// always make the same bytes, so a build has no time of its own.
func age() string {
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
	t, err := time.Parse(time.RFC3339, built)
	if err != nil {
		return fmt.Sprintf("game %s, committed at %s", build, built)
	}
	return fmt.Sprintf(
		"game %s, committed %s ago",
		build,
		time.Since(t).Round(time.Second),
	)
}

// project returns the project's name, which finds its remotes in your
// config. config.Load chose it from .game's name line, go.mod's module or
// the directory's name, so every message and every lookup uses the same
// name. cfg.NamedBy says which of those it came from.
func project(r repo.Repo, cfg config.Config) string {
	return cfg.Named
}

// orNone returns a remote, or says there is none and where to add it.
func orNone(url, dotfile string) string {
	if url == "" {
		return "(none; add it to " + dotfile + ")"
	}
	return url
}

// pushRoad sends the branch you are on to the road, the dirty remote, and
// says so first, so a push is never mistaken for a release.
func pushRoad(r repo.Repo, cfg config.Config) error {
	name := project(r, cfg)
	road, err := roadFor(r, cfg)
	if err != nil {
		return err
	}
	mine, _ := cfg.Mine()
	dist := mine.Dist
	if road == dist {
		return fmt.Errorf(
			"the road and dist for %s are the same "+
				"remote; they never are",
			name,
		)
	}
	if err := guard(r, dist); err != nil {
		return err
	}
	branch, err := r.Branch()
	if err != nil || branch == "HEAD" {
		return fmt.Errorf(
			"not on a branch; check one out before " +
				"pushing to the road",
		)
	}
	fmt.Printf("road (dirty): %s, branch %s\n", road, branch)
	_, err = r.Git("push", road, branch)
	return err
}

// roadFor returns this project's road, the dirty remote, or an error
// saying why there isn't one: no block, a block with no road, or a road
// that doesn't exist. every verb that uses the road calls this, so a
// mistyped path gets a clear error from game before git tries to fetch.
func roadFor(r repo.Repo, cfg config.Config) (string, error) {
	name := project(r, cfg)
	mine, held := cfg.Mine()
	if !held {
		return "", cfg.NoBlock()
	}
	if mine.Road == "" {
		return "", fmt.Errorf(
			"no road for %s; its block in %s has none",
			name,
			cfg.Dotfile,
		)
	}
	if err := there(mine.Road, r.Dir, name, cfg.Dotfile); err != nil {
		return "", err
	}
	return mine.Road, nil
}

// there checks that a remote given as a path on this machine exists. if
// it doesn't, the error names the path, the project and the config file
// it came from. a relative path is resolved from the repository, as git
// does. urls are not checked here.
func there(remote_, dir, name, dotfile string) error {
	if remote(remote_) {
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
// by committing a file at the root, then pushes it, because the other
// person's game can only see the claim once it's pushed.
func doStick(r repo.Repo, cfg config.Config) error {
	if cfg.Author == "" {
		return fmt.Errorf(
			"stick needs an author: \"author NAME\" in " +
				"~/.config/game/config",
		)
	}
	at, err := roadFor(r, cfg)
	if err != nil {
		return err
	}
	// the road's tip first, when the road has this branch: a claim
	// committed behind it cannot be pushed, and neither could the
	// game push the old message pointed at.
	if err := catchUp(r, at); err != nil {
		return err
	}
	if _, err := sticky.Stick(r, at, cfg.Author); err != nil {
		return err
	}
	fmt.Println("stick: stuck")
	if err := pushRoad(r, cfg); err != nil {
		return fmt.Errorf(
			"stuck here, but the claim did not reach the "+
				"road: %v; game push sends it",
			err,
		)
	}
	return nil
}

// catchUp fast-forwards this branch to the road's tip when the road is
// ahead, and says so. a detached head is left for Stick to refuse.
func catchUp(r repo.Repo, road string) error {
	branch, err := r.Branch()
	if err != nil || branch == "HEAD" {
		return err
	}
	n, err := sticky.Behind(r, road, branch)
	if err != nil || n == 0 {
		return err
	}
	tip, _, err := sticky.Sync(r, road, branch)
	if err != nil {
		return err
	}
	fmt.Printf("stick: caught up to the road at %s first\n", tip[:7])
	return nil
}

// doSticky is game sticky, the pairing verb. it fast-forwards this
// branch to the road's tip now, and every build after does the same
// while the claim holds.
func doSticky(r repo.Repo, cfg config.Config) error {
	at, err := roadFor(r, cfg)
	if err != nil {
		return err
	}
	branch, err := r.Branch()
	if err != nil {
		return err
	}
	tip, _, err := sticky.Sync(r, at, branch)
	if err != nil {
		return err
	}
	fmt.Printf("sticky: stuck to %s\n", tip[:7])
	return nil
}

// doUnstick is game unstick. it releases the claim, commits and pushes,
// so the other person sees the claim is gone, and a later release has no
// trace of the session.
func doUnstick(r repo.Repo, cfg config.Config) error {
	// check the road first. releasing the claim is a commit, and if it
	// can't be pushed, the other person still sees the claim.
	at, err := roadFor(r, cfg)
	if err != nil {
		return err
	}
	// and take the other person's changes first, for the same reason.
	branch, err := r.Branch()
	if err != nil {
		return err
	}
	if n, err := sticky.Behind(r, at, branch); err != nil {
		return err
	} else if n > 0 {
		return mergeFirst()
	}
	tip, err := sticky.Unstick(r)
	if err != nil {
		return err
	}
	fmt.Printf(
		"unstick: no longer sticky %s; cleared for publish\n", tip[:7],
	)
	if err := pushRoad(r, cfg); err != nil {
		return fmt.Errorf(
			"unstuck here, but the road still carries the claim: "+
				"%v; game push sends the release",
			err,
		)
	}
	return nil
}

// releaseDiff shows what a release would change on dist.
func releaseDiff(r repo.Repo, cfg config.Config, tag string) error {
	name := project(r, cfg)
	mine, held := cfg.Mine()
	dist := mine.Dist
	if !held {
		return cfg.NoBlock()
	}
	if dist == "" {
		return fmt.Errorf(
			"%s has no dist in its block in %s, so there is "+
				"nothing to diff against",
			name,
			cfg.Dotfile,
		)
	}
	d, err := release.Diff(
		r,
		release.Options{Name: name, Dist: dist, Tag: tag,
			Never:   neverWords(),
			Exclude: cfg.Exclude,
			Allow:   cfg.SweepAllow,
			Omit:    cfg.Omit,
			Author:  cfg.Author,
			License: cfg.License,
		},
	)
	if err != nil {
		return err
	}
	what := name
	if tag != "" {
		what = name + " " + tag
	}
	if neverWords().Empty() {
		fmt.Printf("release diff %s: this game was built "+
			"without never-words, so the sweep below did not "+
			"look for them, and a release would refuse\n", what)
	}
	if d.Same {
		fmt.Printf(
			"release diff %s: dist already has this tree, "+
				"so a release would change nothing\n",
			what,
		)
		return nil
	}
	from := "an empty dist"
	if d.Parent != "" {
		from = d.Parent[:7]
	}
	fmt.Printf("release diff %s: %s -> %s\n\n", what, from, d.Tree[:7])
	// Repo.Git trims, so the stat arrives without git's leading space
	// on the first line and without its trailing newline. Both are put
	// back here rather than in Diff, so the value stays the plain text.
	fmt.Printf(" %s\n", d.Stat)
	// the sweep's hits are printed first, because any one of them
	// stops the release.
	for _, h := range d.Hits {
		fmt.Printf("would be refused: %s in %s\n", h.Kind, h.File)
	}
	fmt.Println()
	fmt.Print(d.Patch)
	return nil
}

// releasePrepare is game release TAG.
func releasePrepare(
	r repo.Repo,
	cfg config.Config,
	tag string,
	mean bool,
) error {
	if err := stamped(); err != nil {
		return err
	}
	if _, stuck, err := sticky.Stuck(r.Dir); err != nil {
		return err
	} else if stuck {
		return fmt.Errorf(
			"stuck: unstick first, so the flat tree this " +
				"release makes carries no trace of the claim",
		)
	}
	name := project(r, cfg)
	mine, held := cfg.Mine()
	dist := mine.Dist
	if !held {
		return cfg.NoBlock()
	}
	if dist == "" {
		return fmt.Errorf(
			"%s has no dist in its block in %s, so there is "+
				"nowhere for a release to go: this tree "+
				"builds locally and that is all",
			name,
			cfg.Dotfile,
		)
	}
	if err := guard(r, dist); err != nil {
		return err
	}
	if miss := cfg.Missing(); len(miss) > 0 {
		for _, m := range miss {
			fmt.Printf("release: %s is needed: %s\n", m.Name, m.Why)
		}
		return fmt.Errorf(
			"refused: set the needed settings in " +
				"~/.config/game/config first",
		)
	}
	l, err := described(cfg)
	if err != nil {
		return err
	}
	found, err := l.Run(r.Dir)
	if err != nil {
		return err
	}
	if len(found) > 0 && !mean {
		for _, f := range found {
			fmt.Println(f)
		}
		return fmt.Errorf(
			"refused: the lint found %d; a release "+
				"takes none (--i-mean-it overrides)",
			len(found),
		)
	}
	if _, e := os.Stat(filepath.Join(r.Dir, "go.mod")); e == nil {
		if err := check(r); err != nil {
			return err
		}
	}
	for _, t := range cfg.Targets {
		if t.Name == "test" || strings.HasPrefix(t.Name, "test-") {
			if err := buildTarget(r, cfg, t.Name); err != nil {
				return err
			}
		}
	}
	vals := values(cfg)
	res, err := release.Prepare(
		r,
		release.Options{Name: name, Dist: dist, Tag: tag,
			Never:   neverWords(),
			Exclude: cfg.Exclude,
			Allow:   cfg.SweepAllow,
			Omit:    cfg.Omit,
			Author:  cfg.Author,
			License: cfg.License,
			Values:  vals,
		},
	)
	if err != nil {
		return err
	}
	fmt.Printf(
		"release %s %s: prepared %s, tree %s\n",
		name,
		tag,
		res.Commit[:7],
		res.Tree,
	)
	fmt.Printf(
		"send it with: game release push %s, to dist (clean): %s\n",
		tag,
		dist,
	)
	return nil
}

// stamped refuses to make or push a release from a binary that can't say
// which commit it came from.
func stamped() error {
	if built != "" && build != "dev" {
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
	out := make([]string, 0, len(cfg.Set))
	for _, v := range cfg.Set {
		out = append(out, v)
	}
	return out
}

// releaseStack is game release stack TAG.
//
// by default the members come from their dists.
func releaseStack(
	r repo.Repo,
	cfg config.Config,
	tag string,
	from string,
	mean bool,
) error {
	if err := stamped(); err != nil {
		return err
	}
	name := project(r, cfg)
	mine, held := cfg.Mine()
	if !held {
		return cfg.NoBlock()
	}
	if mine.Dist == "" {
		return fmt.Errorf(
			"%s has no dist in its block in %s, so there is "+
				"nowhere for a stack to go",
			name,
			cfg.Dotfile,
		)
	}
	if len(cfg.Stack) == 0 {
		return fmt.Errorf(
			"no members; a stack names them in .game as " +
				"\"stack NAME REF\"",
		)
	}
	if from != "road" && from != "dist" {
		return fmt.Errorf("--from is road or dist, not %q", from)
	}
	remotes := cfg.Remotes(from == "dist")
	members := make([]release.Member, 0, len(cfg.Stack))
	for _, m := range cfg.Stack {
		members = append(members, release.Member{
			Name: m[0],
			Ref:  m[1],
			URL:  remotes[m[0]],
		})
	}
	res, err := release.Stack(r, release.Options{
		Name:    name,
		Dist:    mine.Dist,
		Tag:     tag,
		Never:   neverWords(),
		Exclude: cfg.Exclude,
		Allow:   cfg.SweepAllow,
		Omit:    cfg.Omit,
		Author:  cfg.Author,
		Values:  values(cfg),
	}, members)
	if err != nil {
		return err
	}
	if err := stackLint(r, cfg, res, mean); err != nil {
		_, _ = r.Git("update-ref", "-d", release.Ref(tag))
		return err
	}
	if res.Skipped {
		fmt.Printf(
			"stack %s %s: dist already has this tree at %s\n",
			name,
			tag,
			res.Commit[:7],
		)
		return nil
	}
	fmt.Printf(
		"stack %s %s: %d members from their %ss, prepared %s\n",
		name,
		tag,
		len(res.Members),
		from,
		res.Commit[:7],
	)
	if res.Work != "" {
		fmt.Println("  go.work: " + strings.Join(res.Members, " "))
	}
	fmt.Printf(
		"send it with: game release push %s, to dist (clean): %s\n",
		tag,
		mine.Dist,
	)
	return nil
}

// prefixed puts the member's directory in front of each of its lint allow
// lines.
func prefixed(name string, rules []config.Rule) []config.Rule {
	out := make([]config.Rule, 0, len(rules))
	for _, r := range rules {
		if r.Name == "allow" && len(r.Args) > 0 {
			args := append([]string(nil), r.Args...)
			args[0] = name + "/" + args[0]
			r = config.Rule{Name: r.Name, Args: args}
		}
		out = append(out, r)
	}
	return out
}

// stackLint is the strictest lint.
func stackLint(
	r repo.Repo,
	cfg config.Config,
	res release.Stacked,
	mean bool,
) error {
	sets := [][]config.Rule{cfg.Lint}
	for name, game := range res.Games {
		var c config.Config
		if err := c.Parse(
			strings.NewReader(game), ".game", "", false,
		); err != nil {
			continue
		}
		sets = append(sets, prefixed(name, c.Lint))
	}
	l, err := described(config.Config{Lint: config.Strictest(sets...)})
	if err != nil {
		return err
	}
	if l.Empty() {
		return nil
	}
	dir, err := os.MkdirTemp("", "game-stack-lint")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := export(r, res.Commit, dir); err != nil {
		return err
	}
	found, err := l.Run(dir)
	if err != nil {
		return err
	}
	fix, look := 0, 0
	for _, f := range found {
		fmt.Println(f)
		if f.Look {
			look++
			continue
		}
		fix++
	}
	if fix == 0 && look == 0 {
		fmt.Println("stack lint: clean, by every member's rules")
		return nil
	}
	fmt.Printf(
		"stack lint: %d to fix, %d to look at, by every member's "+
			"rules\n",
		fix,
		look,
	)
	if mean {
		return nil
	}
	return fmt.Errorf(
		"the assembly is not clean by the rules its members keep; " +
			"pay it in the members, or say --i-mean-it",
	)
}

// export writes a commit's tree into a directory.
func export(r repo.Repo, commit, dir string) error {
	archive := exec.Command("git", "-C", r.Dir, "archive", commit)
	untar := exec.Command("tar", "-x", "-C", dir)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	untar.Stdin = pipe
	if err := untar.Start(); err != nil {
		return err
	}
	if err := archive.Run(); err != nil {
		return err
	}
	return untar.Wait()
}

// stuckPush refuses release push on a claimed repository.
func stuckPush(r repo.Repo, cfg config.Config) error {
	mine, _ := cfg.Mine()
	branch, err := r.Branch()
	if err != nil {
		return err
	}
	if mine.Road != "" {
		road, err := roadFor(r, cfg)
		if err != nil {
			return err
		}
		n, err := sticky.Behind(r, road, branch)
		if err != nil {
			return fmt.Errorf(
				"stuck, and the road could not be read: %v",
				err,
			)
		}
		if n > 0 {
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

// releasePush is game release push TAG.
//
// it refuses while the repository is claimed.
func releasePush(
	r repo.Repo,
	cfg config.Config,
	tag string,
	publish bool,
) error {
	if _, stuck, err := sticky.Stuck(r.Dir); err != nil {
		return err
	} else if stuck {
		return stuckPush(r, cfg)
	}
	name := project(r, cfg)
	mine, held := cfg.Mine()
	dist := mine.Dist
	if !held {
		return cfg.NoBlock()
	}
	if dist == "" {
		return fmt.Errorf(
			"%s has no dist in its block in %s, so there is "+
				"nowhere for a release to go: this tree "+
				"builds locally and that is all",
			name,
			cfg.Dotfile,
		)
	}
	if err := guard(r, dist); err != nil {
		return err
	}
	if err := stamped(); err != nil {
		return err
	}
	if remote(dist) && !publish {
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
	res, err := release.Push(
		r,
		release.Options{Name: name, Dist: dist, Tag: tag},
	)
	if err != nil {
		return err
	}
	fmt.Printf(
		"dist (clean): %s: main at %s, tagged %s\n",
		dist,
		res.Commit[:7],
		tag,
	)
	paint(r, cfg)
	return nil
}

// hookMark is the line that says a pre-push hook is game's to rewrite.
const hookMark = "# game: the guard on dist"

// guard installs the pre-push hook that refuses any push to dist that
// didn't come from game release push. if a pre-push hook is already there
// and isn't game's, guard leaves it alone and says so.
func guard(r repo.Repo, dist string) error {
	if dist == "" {
		return nil
	}
	dir, err := r.Git("rev-parse", "--git-path", "hooks")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(r.Dir, dir)
	}
	path := filepath.Join(dir, "pre-push")
	if b, err := os.ReadFile(path); err == nil &&
		!strings.Contains(string(b), hookMark) {
		fmt.Printf(
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
		strings.Split(release.Guard, "=")[0] +
		"\" != 1 ]; then\n" +
		"echo \"refused: $2 is dist; only game release push " +
		"sends there\" >&2\n" +
		"  exit 1\nfi\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(script), 0o755)
}

// check passes when gofmt has nothing to say, vet passes and the tests
// pass. its exit code is the tests' own, never a pipe's.
func check(r repo.Repo) error {
	if _, err := os.Stat(filepath.Join(r.Dir, "go.mod")); err != nil {
		fmt.Println(
			"check: no go.mod, so nothing for gofmt, vet or go " +
				"test; the test targets are the check here",
		)
		return nil
	}
	out, err := run(r.Dir, "gofmt", "-l", ".")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf(
			"gofmt would change: %s",
			strings.ReplaceAll(strings.TrimSpace(out), "\n", " "),
		)
	}
	if _, err := run(r.Dir, "go", "vet", "./..."); err != nil {
		return err
	}
	out, err = run(r.Dir, "go", "test", "./...")
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" && !strings.Contains(line, "no test files") {
			fmt.Println(line)
		}
	}
	return err
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
func paint(r repo.Repo, cfg config.Config) {
	painted(r.Dir, project(r, cfg))
}

// painted shows one directory's art, art/<project>.ansi or
// art/header.ansi, and returns whether it found any. paint says when it
// is shown.
func painted(dir, name string) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	for _, at := range []string{name + ".ansi", "header.ansi"} {
		b, err := os.ReadFile(filepath.Join(dir, "art", at))
		if err != nil {
			continue
		}
		os.Stdout.Write(b)
		if len(b) > 0 && b[len(b)-1] != '\n' {
			fmt.Println()
		}
		return true
	}
	return false
}

// buildBinaries builds every cmd/* into bin/, stamped so version can say
// which commit and how old.
//
// the stamp is the commit and the commit's own time, never the clock,
// and the tree's path is trimmed out, so the same sources build the
// same bytes in any tree at any hour. a binary's hash then says which
// sources made it.
//
// while the repository is claimed, it first fast-forwards this branch to
// the road's tip, as game sticky does, and reports what moved, so you
// only type the pairing verb once. every binary it builds after that is
// marked sticky build, in its own colour.
func buildBinaries(r repo.Repo, cfg config.Config) error {
	entries, err := os.ReadDir(filepath.Join(r.Dir, "cmd"))
	if err != nil {
		return fmt.Errorf("no cmd/ directory to build")
	}
	_, stuck, err := sticky.Stuck(r.Dir)
	if err != nil {
		return err
	}
	if stuck {
		if err := syncBuild(r, cfg); err != nil {
			return err
		}
	}
	commit, _ := r.Git("rev-parse", "--short", "HEAD")
	if clean, _ := r.Clean(); !clean {
		commit += "-dirty"
	}
	when, _ := r.Git("log", "-1", "--format=%cI")
	stamp := fmt.Sprintf(
		"-X main.build=%s -X main.built=%s", commit, when,
	)
	os.MkdirAll(filepath.Join(r.Dir, "bin"), 0o755)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		_, err := run(
			r.Dir, "go", "build", "-trimpath", "-ldflags", stamp,
			"-o", filepath.Join("bin", e.Name()),
			"./cmd/"+e.Name(),
		)
		if err != nil {
			return err
		}
		verb := "build"
		if stuck {
			verb = "sticky build"
		}
		line := fmt.Sprintf("%s: bin/%s %s", verb, e.Name(), commit)
		if stuck {
			line = stickyColour(line)
		}
		fmt.Println(line)
	}
	return nil
}

// syncBuild is the fast-forward a build does while the repository is
// claimed. read dem docs pls: README.md, "human-agent pairing".
func syncBuild(r repo.Repo, cfg config.Config) error {
	mine, held := cfg.Mine()
	if !held || mine.Road == "" {
		return nil
	}
	road, err := roadFor(r, cfg)
	if err != nil {
		return err
	}
	branch, err := r.Branch()
	if err != nil {
		return err
	}
	tip, moved, err := sticky.Sync(r, road, branch)
	if err != nil {
		return err
	}
	if moved {
		fmt.Printf("build: updates from %s merged\n", tip[:7])
	}
	return nil
}

// stickyColour marks a line in the colour a sticky build gets, the
// same gate paint uses: only to a terminal, and never with NO_COLOR
// set, so a pipe, a log and a child game see plain text.
func stickyColour(line string) string {
	if os.Getenv("NO_COLOR") != "" {
		return line
	}
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return line
	}
	return "\x1b[36m" + line + "\x1b[0m"
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
func clean(r repo.Repo, cfg config.Config, cache bool) error {
	for _, dir := range cleanable(r) {
		if _, err := run(dir, "go", "clean", "./..."); err != nil {
			return err
		}
		if !cache {
			continue
		}
		if _, err := run(
			dir, "go", "clean", "-cache", "-testcache",
		); err != nil {
			return err
		}
	}
	gone := 0
	for _, dir := range append([]string{r.Dir}, members(r, cfg)...) {
		n, err := sweepBin(r, filepath.Join(dir, "bin"))
		if err != nil {
			return err
		}
		gone += n
	}
	sum := filepath.Join(r.Dir, "go.work.sum")
	if _, err := os.Stat(sum); err == nil {
		if err := os.Remove(sum); err != nil {
			return err
		}
		fmt.Println(
			"clean: go.work.sum away; the build writes it again",
		)
	}
	fmt.Printf(
		"clean: %d files of build output away%s\n",
		gone,
		map[bool]string{
			true:  ", caches forgotten",
			false: "",
		}[cache],
	)
	return nil
}

// sweepBin removes the build output in one bin/ and counts the files it
// removed. it asks git first.
//
// a member may keep real scripts in bin/, and clean must never delete
// source. bin/ itself is removed only if it was untracked and is empty
// afterwards.
func sweepBin(r repo.Repo, bin string) (int, error) {
	if _, err := os.Stat(bin); err != nil {
		return 0, nil
	}
	entries, err := os.ReadDir(bin)
	if err != nil {
		return 0, err
	}
	gone := 0
	for _, e := range entries {
		at := filepath.Join(bin, e.Name())
		tracked, err := r.Git("ls-files", "--", at)
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
		return gone, err
	}
	if len(left) == 0 {
		if err := os.Remove(bin); err != nil {
			return gone, err
		}
	}
	return gone, nil
}

// cleanable returns the directories go clean can work in.
//
// a stack checkout has no module at its root, only a go.work that names
// the members. "go clean ./..." fails there, because the root isn't in
// the work file, so game cleans each member instead. a tree with no go
// in it has nothing for go to clean.
func cleanable(r repo.Repo) []string {
	if _, err := os.Stat(filepath.Join(r.Dir, "go.mod")); err == nil {
		return []string{r.Dir}
	}
	b, err := os.ReadFile(filepath.Join(r.Dir, "go.work"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "use ")
		line = strings.Trim(line, "()")
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "./") {
			continue
		}
		out = append(out, filepath.Join(r.Dir, line))
	}
	return out
}

// members is the stack members this repository declares, as directories,
// so their build output is cleaned along with the root's. a member that
// is not a go module still has a bin/ if its own targets write one.
func members(r repo.Repo, cfg config.Config) []string {
	var out []string
	for _, m := range cfg.Stack {
		dir := filepath.Join(r.Dir, m[0])
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		out = append(out, dir)
	}
	return out
}

// described is the lint the config describes.
func described(cfg config.Config) (lint.Lint, error) {
	rules := make([]struct {
		Name string
		Args []string
	}, 0, len(cfg.Lint))
	for _, r := range cfg.Lint {
		rules = append(rules, struct {
			Name string
			Args []string
		}{r.Name, r.Args})
	}
	return lint.Describe(rules)
}

// buildTarget runs a target's steps from your config in order, in its
// directory, under nice, so other work on the machine keeps its share.
// everything it prints goes to bin/NAME.log, and the last lines are
// shown. the first step that fails stops the build, and is named.
func buildTarget(r repo.Repo, cfg config.Config, name string) error {
	var t *config.Target
	for i := range cfg.Targets {
		if cfg.Targets[i].Name == name {
			t = &cfg.Targets[i]
		}
	}
	if t == nil {
		names := make([]string, 0, len(cfg.Targets))
		for _, x := range cfg.Targets {
			names = append(names, x.Name)
		}
		return fmt.Errorf(
			"no target called %q; described: %s",
			name,
			strings.Join(names, ", "),
		)
	}
	os.MkdirAll(filepath.Join(r.Dir, "bin"), 0o755)
	logPath := filepath.Join(r.Dir, "bin", name+".log")
	logf, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logf.Close()
	start := time.Now()
	for i, step := range t.Steps {
		fmt.Printf(
			"build %s: step %d of %d: %s\n",
			name,
			i+1,
			len(t.Steps),
			strings.Join(step.Args, " "),
		)
		fmt.Fprintf(
			logf,
			"== step %d, in %s: %s\n",
			i+1,
			step.Dir,
			strings.Join(step.Args, " "),
		)
		dir := step.Dir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(r.Dir, dir)
		}
		// a program given as a relative path, like ./gen, is in the
		// step's directory, not the shell's. nice would look for it
		// from its own, so the path is joined to the step's directory.
		prog := step.Args[0]
		if strings.Contains(prog, "/") && !filepath.IsAbs(prog) {
			prog = filepath.Join(dir, prog)
		}
		args := append([]string{"-n", "19", prog}, step.Args[1:]...)
		cmd := exec.Command("nice", args...)
		cmd.Dir = dir
		cmd.Stdout, cmd.Stderr = logf, logf
		if err := cmd.Run(); err != nil {
			tail(logPath, 12)
			return fmt.Errorf(
				"build %s: step %d failed after %s; "+
					"the log is %s",
				name,
				i+1,
				time.Since(start).Round(time.Second),
				logPath,
			)
		}
	}
	tail(logPath, 4)
	fmt.Printf(
		"build %s: done in %s; the log is %s\n",
		name,
		time.Since(start).Round(time.Second),
		logPath,
	)
	return nil
}

// tail prints the last n lines of a file, for the moment after a build.
func tail(path string, n int) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for _, l := range lines {
		fmt.Println("  " + l)
	}
}

// runNamed runs a named run in your terminal, without tmux's variables.
func runNamed(r repo.Repo, cfg config.Config, name string) error {
	var t *config.Target
	for i := range cfg.Runs {
		if cfg.Runs[i].Name == name {
			t = &cfg.Runs[i]
		}
	}
	if t == nil {
		names := make([]string, 0, len(cfg.Runs))
		for _, x := range cfg.Runs {
			names = append(names, x.Name)
		}
		return fmt.Errorf(
			"no run called %q; described: %s",
			name,
			strings.Join(names, ", "),
		)
	}
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMUX=") ||
			strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		env = append(env, kv)
	}
	for _, step := range t.Steps {
		dir := step.Dir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(r.Dir, dir)
		}
		prog := step.Args[0]
		if strings.Contains(prog, "/") && !filepath.IsAbs(prog) {
			prog = filepath.Join(dir, prog)
		}
		cmd := exec.Command(prog, step.Args[1:]...)
		cmd.Dir, cmd.Env = dir, env
		cmd.Stdin, cmd.Stdout = os.Stdin, os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf(
				"run %s: %s: %w",
				name,
				strings.Join(step.Args, " "),
				err,
			)
		}
	}
	return nil
}

// verify is what a stack build's child game runs in a member's clone:
// check, the test targets, the binaries and the lint. it reports each to
// the parent as an event on stdout.
//
// the exit code says whether it passed. the lint count is reported but
// doesn't fail the build, because the table at the end shows it.
func verify(r repo.Repo, cfg config.Config) error {
	enc := json.NewEncoder(os.Stdout)
	tell := func(step, state, kind, text string, count int) {
		enc.Encode(
			stack.Event{
				Step:  step,
				State: state,
				Kind:  kind,
				Text:  text,
				Count: count,
			},
		)
	}
	failed := func(step string, err error) error {
		text := err.Error()
		if step == "check" && strings.Contains(text, "go test") {
			step = "test"
		}
		tell(step, "fail", stack.Kind(text), text, 0)
		return err
	}
	if _, e := os.Stat(filepath.Join(r.Dir, "go.mod")); e == nil {
		tell("check", "start", "", "", 0)
		if err := check(r); err != nil {
			return failed("check", err)
		}
		tell("check", "ok", "", "", 0)
	}
	for _, t := range cfg.Targets {
		if t.Name == "test" || strings.HasPrefix(t.Name, "test-") {
			tell("test", "start", "", t.Name, 0)
			if err := buildTarget(r, cfg, t.Name); err != nil {
				return failed("test", err)
			}
			tell("test", "ok", "", t.Name, 0)
		}
	}
	if st, e := os.Stat(filepath.Join(r.Dir, "cmd")); e == nil &&
		st.IsDir() {
		tell("build", "start", "", "", 0)
		if err := buildBinaries(r, cfg); err != nil {
			return failed("build", err)
		}
		tell("build", "ok", "", "", 0)
	}
	l, err := described(cfg)
	if err != nil {
		return failed("lint", err)
	}
	found, err := l.Run(r.Dir)
	if err != nil {
		return failed("lint", err)
	}
	tell("lint", "ok", "", "", len(found))
	return nil
}

// buildStack builds every member of the stack in .game.
func buildStack(r repo.Repo, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("build stack", flag.ExitOnError)
	from := fs.String(
		"from",
		"dist",
		"where members come from: road or dist",
	)
	parallel := fs.Int(
		"parallel",
		1,
		"members built at once; one at a time shows each member's "+
			"art as it is reached and stops at a failure",
	)
	here := fs.Bool(
		"here",
		false,
		"build the members that are already in this clone, "+
			"which is what an assembled stack is",
	)
	fs.Parse(args)
	if len(cfg.Stack) == 0 {
		return fmt.Errorf(
			"no stack described; add \"stack NAME REF\" " +
				"lines to .game",
		)
	}
	remotes := cfg.Remotes(true)
	if *here {
		remotes = nil
	} else if *from == "road" {
		remotes = cfg.Remotes(false)
	} else if *from != "dist" {
		return fmt.Errorf("--from is road or dist, not %q", *from)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	members := make([]stack.Member, 0, len(cfg.Stack))
	for _, m := range cfg.Stack {
		members = append(members, stack.Member{Name: m[0], Ref: m[1]})
	}
	where, work := *from, filepath.Join(r.Dir, "bin", "stack")
	url := func(name string) (string, error) {
		if u := remotes[name]; u != "" {
			return u, there(u, r.Dir, name, cfg.Dotfile)
		}
		return "", fmt.Errorf(
			"no %s for %s in %s",
			*from,
			name,
			cfg.Dotfile,
		)
	}
	if *here {
		where, work, url = "this clone", r.Dir, nil
	}
	fmt.Printf("stack: %d members from %s\n", len(members), where)
	rows, err := stack.Run(context.Background(), stack.Options{
		Members: members,
		URL:     url,
		Work:    work,
		Child: func(string) *exec.Cmd {
			return exec.Command(self, "verify")
		},
		Parallel: *parallel,
		Out:      os.Stdout,
	})
	if err != nil {
		return err
	}
	stack.Table(os.Stdout, rows)
	clean := 0
	for _, row := range rows {
		if row.Result == "ok" && row.Lint == 0 {
			clean++
		}
	}
	// art wasn't shown because each member was a child writing to a
	// pipe. show it here.
	for _, row := range rows {
		if row.Result != "ok" {
			continue
		}
		painted(filepath.Join(work, row.Name), row.Name)
	}
	fmt.Printf("stack: %d of %d built clean\n", clean, len(rows))
	if clean != len(rows) {
		return fmt.Errorf(
			"stack: %d did not build clean",
			len(rows)-clean,
		)
	}
	return nil
}

// buildDocker builds an image of this repository.
func buildDocker(r repo.Repo, cfg config.Config) error {
	if _, err := os.Stat(filepath.Join(r.Dir, "Dockerfile")); err != nil {
		return fmt.Errorf("no Dockerfile to build")
	}
	name := project(r, cfg)
	commit, _ := r.Git("rev-parse", "--short", "HEAD")
	if clean, _ := r.Clean(); !clean {
		commit += "-dirty"
	}
	image := name + ":" + commit
	cmd := exec.Command("docker", "build", "-t", image, ".")
	cmd.Dir, cmd.Stdout, cmd.Stderr = r.Dir, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build %s: %w", image, err)
	}
	fmt.Printf("build docker: %s\n", image)
	if len(cfg.Needs) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(
		&b,
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  "+
			"name: %s-settings\ndata:\n",
		name,
	)
	for _, kv := range cfg.Env() {
		k, v, _ := strings.Cut(kv, "=")
		fmt.Fprintf(&b, "  %s: %q\n", k, v)
	}
	os.MkdirAll(filepath.Join(r.Dir, "bin"), 0o755)
	path := filepath.Join(r.Dir, "bin", name+"-settings.yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return err
	}
	fmt.Printf(
		"build docker: the settings it needs are in %s, for "+
			"the cluster, not the image\n",
		path,
	)
	return nil
}

// remote tells you if the dist is on another machine.
func remote(dist string) bool {
	for _, p := range []string{
		"http://", "https://", "ssh://", "git://",
	} {
		if strings.HasPrefix(dist, p) {
			return true
		}
	}
	return strings.Contains(dist, "@") && strings.Contains(dist, ":")
}
