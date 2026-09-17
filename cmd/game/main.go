// game: the verbs a go project needs that go itself does not know are
// things. check, build, release, bounce, sweep, version. it runs in any
// shell and needs git and go and nothing else; there is no makefile
// because there is nothing left for one to say.
//
// the window it will one day draw in is another program's business.
//
//	game check                    gofmt -l, go vet, go test, exit code kept
//	game build                    every cmd/* into bin/, stamped with the
//	                              commit and the time it was built
//	game build NAME               a target the config describes: its steps
//	                              in order, in its directory, under nice,
//	                              the output kept in bin/NAME.log
//	game clean [--cache]          go clean, and bin/ away; --cache also
//	                              forgets go's build and test caches
//	game lint [--look]            whatever lint the config describes,
//	                              with `lint` lines; nothing described
//	                              is no lint. wide code and shouting are
//	                              counted, and listed with --look
//	game trees                    the road and the dist for this project
//	game push                     the branch to the road, the dirty remote
//	game release TAG [--i-mean-it]
//	                              every check, the lint at zero findings,
//	                              then a flat commit kept for its push
//	game release push TAG         that commit and its tag to dist, the
//	                              clean remote, and nothing else goes there
//	game run NAME                 a run the config describes, in the
//	                              foreground, in your terminal, not nice'd;
//	                              tmux's variables are dropped, since a
//	                              window is not inside your tmux
//	game bounce [RANGE]           what is staged, or a commit or range
//	game sweep [REV]              the tree at a revision, default HEAD
//	game version                  which commit this binary is, and its age
//
// settings come from ~/.config/game/config, the builder's, and then .game
// in the repository, one per line; the word list, the remotes and the
// values are the builder's alone, and a repository never holds them:
//
//	author  Ada                         the name the bounce refuses
//	words   ~/.config/game/sweep.words  the sweep's word list
//
// and a block per project you build, its lines indented under it:
//
//	project game                        a project of yours
//		road ~/roots/game-road.git   its dirty remote: game push
//		dist git@github.com:ada/game its clean remote: game release
//		set CONTACT ada@example.com  a value it needs
//
// A tree whose project has no block here builds and nothing else. There
// is nowhere for a release to go, so there is no release: that is the
// definition rather than a check, and it is how game is tried on a
// repository that is not yours.
//
// and in .game, the repository's:
//
//	name    game                        the project, which is its block
//	needs   CONTACT who to reach        a setting it needs, and why
//	exclude spec-docs/                  left alone by the sweep; repeatable
//	lint    width 80                    a lint rule; repeatable. a .game
//	                                    that has any replaces the dotfile's
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
	"github.com/janearc/game/internal/sweep"
)

// build and built are stamped by the makefile.
var (
	build = "dev"
	built = ""
)

// cwd is the repository: where game was run.
func cwd() string {
	d, _ := os.Getwd()
	return d
}

// main is the verb table; each verb is a function below.
func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	r := repo.Repo{Dir: cwd()}
	cfg, err := config.Load(gameHome(), os.Getenv("HOME"), r.Dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "game:", err)
		os.Exit(2)
	}
	// the settings a repository needs reach what game runs as environment,
	// from the builder's dotfile, and only those; a build or a run that
	// needs one that is not set refuses with the reason it is needed.
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
		if err = buildBinaries(r); err == nil {
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
			fmt.Println(
				"lint: nothing described; add lint " +
					"lines to ~/.config/game/config or " +
					".game",
			)
			break
		}
		found, e := l.Run(r.Dir)
		if e != nil {
			err = e
			break
		}
		fix, looks := 0, map[string]int{}
		for _, f := range found {
			if f.Look {
				looks[f.Rule]++
			} else {
				fix++
			}
			if !*count {
				fmt.Println(f)
			}
		}
		for rule, n := range looks {
			fmt.Printf("look: %d %s\n", n, rule)
		}
		if fix > 0 {
			err = fmt.Errorf("lint: %d to fix", fix)
		} else if len(looks) == 0 {
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
			fmt.Printf(
				"no project block for %s: a local build\n",
				project(r, cfg),
			)
		}
		fmt.Printf("road: %s\n", orNone(mine.Road))
		fmt.Printf("dist: %s\n", orNone(mine.Dist))
		err = guard(r, mine.Dist)
	case "push":
		err = pushRoad(r, cfg)
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
		th, e := sweep.Tree(
			r,
			rev,
			sweep.Words(cfg.Words),
			cfg.Author,
			cfg.SweepAllow,
			cfg.Exclude...)
		if e != nil {
			err = e
			break
		}
		hits = append(hits, th...)
		if len(hits) == 0 {
			fmt.Printf("sweep: %s is clean\n", rev)
		} else {
			for _, h := range hits {
				fmt.Printf("sweep: %s in %s\n", h.Kind, h.File)
			}
			err = fmt.Errorf("%d hit(s)", len(hits))
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

// usage is the one line to type when the verb was wrong.
func usage() {
	fmt.Fprintln(
		os.Stderr,
		"game check | build [NAME] | run NAME | clean "+
			"[--cache] | lint [--look] |\n"+
			"trees | push | release TAG [--i-mean-it] | "+
			"release push TAG |\n"+
			"     bounce [RANGE] | sweep [REV] | version",
	)
}

// age is which commit this binary is and how long ago it was built.
//
// a binary go built carries neither, and says so in words: the stamp is
// two -ldflags that only game passes, so an unstamped binary is one built
// straight out of a tree, which is fine and worth knowing.
func age() string {
	if built == "" && build == "dev" {
		return "game, built from a tree by go rather than by game: " +
			"no commit and no build time in it"
	}
	if built == "" {
		return fmt.Sprintf(
			"game %s, with no build time in it", build,
		)
	}
	t, err := time.Parse(time.RFC3339, built)
	if err != nil {
		return fmt.Sprintf("game %s, built at %s", build, built)
	}
	return fmt.Sprintf(
		"game %s, built %s ago",
		build,
		time.Since(t).Round(time.Second),
	)
}

// project is the project's name, which keys its remotes in the builder's
// config: .game's name, else the module path's last element, else the
// directory, since a checkout is named for whoever holds it.
func project(r repo.Repo, cfg config.Config) string {
	if cfg.Name != "" {
		return cfg.Name
	}
	if mod, err := os.ReadFile(filepath.Join(r.Dir, "go.mod")); err == nil {
		for _, line := range strings.Split(string(mod), "\n") {
			if strings.HasPrefix(line, "module ") {
				return filepath.Base(
					strings.TrimSpace(
						strings.TrimPrefix(
							line,
							"module ",
						),
					),
				)
			}
		}
	}
	return filepath.Base(r.Dir)
}

// orNone is a remote, or a word saying there is none.
func orNone(url string) string {
	if url == "" {
		return "(none; add it to ~/.config/game/config)"
	}
	return url
}

// pushRoad sends the branch you are on to the road, the dirty remote, and
// says so first, so a push is never mistaken for a release.
func pushRoad(r repo.Repo, cfg config.Config) error {
	name := project(r, cfg)
	mine, _ := cfg.Mine()
	road, dist := mine.Road, mine.Dist
	if road == "" {
		return fmt.Errorf(
			"no road for %s; open a \"project %s\" block in "+
				"~/.config/game/config with a road in it",
			name,
			name,
		)
	}
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

// releasePrepare is game release TAG: every check, then a flat commit kept
// for its push. the lint takes zero things to fix and zero to look at,
// because a rule described is a rule meant; --i-mean-it is the exception.
func releasePrepare(
	r repo.Repo,
	cfg config.Config,
	tag string,
	mean bool,
) error {
	name := project(r, cfg)
	mine, held := cfg.Mine()
	dist := mine.Dist
	if !held || dist == "" {
		return fmt.Errorf(
			"%s has no project block with a dist in "+
				"~/.config/game/config, so there is "+
				"nowhere for a release to go: this tree "+
				"builds locally and that is all",
			name,
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
			Words: sweep.Words(
				cfg.Words,
			),
			Exclude: cfg.Exclude,
			Allow:   cfg.SweepAllow,
			Omit:    cfg.Omit,
			Author:  cfg.Author,
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

// values is the builder's setting values, which a release refuses to
// carry: they are the builder's, and the release is everybody's.
func values(cfg config.Config) []string {
	out := make([]string, 0, len(cfg.Set))
	for _, v := range cfg.Set {
		out = append(out, v)
	}
	return out
}

// releaseStack is game release stack TAG: every member of this
// repository's stack laid under its own name in one flat commit, for
// somebody who wants the whole stack rather than a part of it.
//
// The members are taken from their dists by default, which is what a
// stranger would get; --from road assembles what is on the roads, which
// is how the assembly is tried before anything is published.
func releaseStack(
	r repo.Repo,
	cfg config.Config,
	tag string,
	from string,
	mean bool,
) error {
	name := project(r, cfg)
	mine, held := cfg.Mine()
	if !held || mine.Dist == "" {
		return fmt.Errorf(
			"%s has no project block with a dist in "+
				"~/.config/game/config, so there is nowhere "+
				"for a stack to go",
			name,
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
		Words:   sweep.Words(cfg.Words),
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

// prefixed moves a member's file-scoped allowances to where that member
// lands in the assembly, so a page a member exempted still names that
// page once every member sits under its own name.
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

// stackLint holds the whole assembly to every member's rules: the
// strictest of them, run over the assembled tree.
//
// A stack is published as one thing, so a rule one member lives by is a
// rule the stack lives by. --i-mean-it says it out loud instead.
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

// export writes a commit's tree into a directory, which is how the
// assembly is read by anything that works on files rather than on git.
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

// releasePush is game release push TAG: the prepared release to dist, the
// one thing that ever reaches it.
func releasePush(
	r repo.Repo,
	cfg config.Config,
	tag string,
	publish bool,
) error {
	name := project(r, cfg)
	mine, held := cfg.Mine()
	dist := mine.Dist
	if !held || dist == "" {
		return fmt.Errorf(
			"%s has no project block with a dist in "+
				"~/.config/game/config, so there is "+
				"nowhere for a release to go: this tree "+
				"builds locally and that is all",
			name,
		)
	}
	if err := guard(r, dist); err != nil {
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

// guard installs the pre-push hook that refuses any push to dist that did
// not come from game release push. a hook someone else wrote is left
// alone and said so, since a guard that clobbers is its own accident.
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

// check is the gate: gofmt has nothing to say, vet passes, the tests
// pass, and the exit code is the tests', never a pipe's.
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

// paint shows the repository's own art, once, when something finished.
//
// a build that worked is worth a second of pleasure, and the art belongs
// to the repository: art/<project>.ansi, else art/header.ansi.
//
// it goes only to a terminal, so a pipe, a log and a child game see
// nothing, and NO_COLOR turns it off like anything else that paints.
func paint(r repo.Repo, cfg config.Config) {
	painted(r.Dir, project(r, cfg))
}

// painted shows one directory's art, named for the project or called
// header.ansi, and says whether it found any.
//
// a build that worked is worth a second of pleasure. it goes only to a
// terminal, so a pipe, a log and a child game see nothing, and NO_COLOR
// turns it off like anything else that paints.
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
func buildBinaries(r repo.Repo) error {
	entries, err := os.ReadDir(filepath.Join(r.Dir, "cmd"))
	if err != nil {
		return fmt.Errorf("no cmd/ directory to build")
	}
	commit, _ := r.Git("rev-parse", "--short", "HEAD")
	if clean, _ := r.Clean(); !clean {
		commit += "-dirty"
	}
	stamp := fmt.Sprintf(
		"-X main.build=%s -X main.built=%s",
		commit,
		time.Now().UTC().Format(time.RFC3339),
	)
	os.MkdirAll(filepath.Join(r.Dir, "bin"), 0o755)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		_, err := run(
			r.Dir, "go", "build", "-ldflags", stamp,
			"-o", filepath.Join("bin", e.Name()),
			"./cmd/"+e.Name(),
		)
		if err != nil {
			return err
		}
		fmt.Printf("build: bin/%s %s\n", e.Name(), commit)
	}
	return nil
}

// run is one command in a directory, output and go's own words on error.
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
// took, asking git first.
//
// a member may keep real scripts in bin/ (flipr's deploy and gen,
// kingfisher's whole set), and a clean that deletes those is a clean
// that eats the source. the directory goes only if it was untracked
// and is empty afterwards.
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

// cleanable is the directories go itself can be told to clean.
//
// a stack checkout has no module at its root, only a go.work naming
// the members that are modules, so "go clean ./..." there fails by
// design: the root is not listed in the work file.
//
// the members are the unit instead, and a tree with no go in it at
// all has nothing for go to do.
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

// buildTarget runs a described target's steps in order, in its
// directory, under nice so prod and the person keep their share of the
// machine, with everything it printed kept in bin/NAME.log and the last
// lines shown. the first step that fails stops it, and says which.
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
		// a program named by a relative path is meant against the
		// step's directory, not the shell's; nice would look from
		// its own.
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

// runNamed runs a described run's steps in the foreground: your
// terminal is its terminal, nothing is nice'd or logged, and tmux's
// variables are dropped from the environment.
//
// a window opened from inside tmux is not inside it, and a program that
// checks would be misled.
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

// verify is what a stack's child runs in a member's clone: check, the
// test targets, the binaries and the lint, each told to the parent as an
// event on stdout. the exit code says whether it passed; the lint count
// is told and does not fail the build, since the table shows it.
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
		if err := buildBinaries(r); err != nil {
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

// buildStack is game build stack: every member of the stack .game
// describes, from the road or from dist, each built by a child game of
// its own, and a table at the end. clean is built with nothing for the
// lint to say.
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
			return u, nil
		}
		return "", fmt.Errorf(
			"no %s for %s in ~/.config/game/config",
			*from,
			name,
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
	// each member's build was a child with a pipe for a terminal, so its
	// own art went nowhere. the parent has the terminal, and shows the
	// art of everything that built.
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

// buildDocker is game build docker: an image of this repository, named
// for the project and tagged with the commit, and nothing of the builder
// in it.
//
// the settings the repository needs go beside it, in a ConfigMap under
// bin/, for the cluster, since an image travels and a dotfile does not.
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

// remote says whether a dist lives somewhere other than this machine.
//
// a local bare repository is its own safety: being wrong writes to a
// directory. a url is not, so pushing to one is the step that wants to be
// typed on purpose rather than reached by running the next command in a
// sequence.
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

// gameHome is whose config this run reads: GAME_HOME when it is set, and
// the shell's HOME otherwise.
//
// an agent has an anchor of its own and wants its own roads, dists and
// settings; a person in a shell wants theirs. faking HOME would do it and
// would take ssh keys, gh credentials and everything else along with it,
// so the build tool asks for the one thing it needs instead.
func gameHome() string {
	if at := os.Getenv("GAME_HOME"); at != "" {
		return at
	}
	return os.Getenv("HOME")
}
