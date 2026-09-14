// game: the verbs a go project needs that go itself does not know are
// things. check, build, release, bounce, sweep, version. it runs in any
// shell and needs git and go and nothing else; there is no makefile
// because there is nothing left for one to say. the window it will one
// day draw in is another program's business.
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
//	game release [--public PATH] [--message FILE] [--exclude PREFIX]...
//	                              lint must pass first
//	game bounce [RANGE]           what is staged, or a commit or range
//	game sweep [REV]              the tree at a revision, default HEAD
//	game version                  which commit this binary is, and its age
//
// settings come from ~/.config/game/config and then .game in the
// repository, one per line, the repository's winning and a flag winning
// over both; the word list is the dotfile's alone:
//
//	author  Ada                         the name the bounce refuses
//	words   ~/.config/game/sweep.words  the sweep's word list
//	public  ../project-root.git         the public root, from the repo
//	exclude spec-docs/                  left alone by the sweep; repeatable
//	lint    width 80                    a lint rule; repeatable. a .game
//	                                    that has any replaces the dotfile's
//	target  NAME DIR :: COMMAND ...     a build beyond go's; one line per
//	                                    step, in order
package main

import (
	"bytes"
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
	cfg, err := config.Load(os.Getenv("HOME"), r.Dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "game:", err)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version", "--age":
		fmt.Println(age())
	case "check":
		err = check(r)
	case "build":
		if len(os.Args) > 2 {
			err = buildTarget(r, cfg, os.Args[2])
			break
		}
		err = buildBinaries(r)
	case "lint":
		fs := flag.NewFlagSet("lint", flag.ExitOnError)
		look := fs.Bool("look", false, "list the things to look at, not only count them")
		fs.Parse(os.Args[2:])
		l, e := described(cfg)
		if e != nil {
			err = e
			break
		}
		if l.Empty() {
			fmt.Println("lint: nothing described; add lint lines to ~/.config/game/config or .game")
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
				if *look {
					fmt.Println(f)
				}
				continue
			}
			fix++
			fmt.Println(f)
		}
		for rule, n := range looks {
			fmt.Printf("look: %d %s (--look lists them)\n", n, rule)
		}
		if fix > 0 {
			err = fmt.Errorf("lint: %d to fix", fix)
		} else if len(looks) == 0 {
			fmt.Println("lint: clean")
		}
		if _, e := exec.LookPath("staticcheck"); e != nil {
			fmt.Println("lint: staticcheck is not on the path; go install honnef.co/go/tools/cmd/staticcheck@latest")
		}
	case "clean":
		fs := flag.NewFlagSet("clean", flag.ExitOnError)
		cache := fs.Bool("cache", false, "also forget go's build and test caches, for a cold run")
		fs.Parse(os.Args[2:])
		err = clean(r, *cache)
	case "release":
		fs := flag.NewFlagSet("release", flag.ExitOnError)
		public := fs.String("public", cfg.Public, "the public root, a path or url")
		msgFile := fs.String("message", "", "a file holding the release commit message; default is a dated line and the sign-off from the last commit")
		var exclude multi
		fs.Var(&exclude, "exclude", "a path prefix the sweep leaves alone; repeatable")
		fs.Parse(os.Args[2:])
		ex := append(append([]string(nil), cfg.Exclude...), exclude...)
		msg, e := message(r, *msgFile, *public)
		if e != nil {
			err = e
			break
		}
		// lint clean before anything leaves: a release is the one
		// moment the house rules are not a suggestion.
		if l, e := described(cfg); e != nil {
			err = e
			break
		} else if found, e := l.Run(r.Dir); e != nil || lint.Failed(found) {
			if e == nil {
				e = fmt.Errorf("refused: lint has things to fix; run game lint")
			}
			err = e
			break
		}
		res, e := release.Run(r, release.Options{Public: *public, Message: msg, Words: sweep.Words(cfg.Words), Exclude: ex})
		switch {
		case e != nil:
			err = e
		case res.Skipped:
			fmt.Printf("release: nothing to release; the public main already has this tree\n")
		default:
			fmt.Printf("release: %s pushed as main to %s; tree %s\n", res.Commit[:7], *public, res.Tree)
		}
	case "bounce":
		if cfg.Author == "" {
			err = fmt.Errorf("bounce needs an author: \"author NAME\" in ~/.config/game/config")
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
			fmt.Println("bounce: the author in the third person, in added lines:")
			for _, h := range hits {
				fmt.Println("  " + h.Line)
			}
			if os.Getenv("BOUNCE_OK") != "" {
				fmt.Println("bounce: signed off (BOUNCE_OK); passing")
			} else {
				err = fmt.Errorf("refused. reword to the first person, or BOUNCE_OK=1 to sign it off")
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
		th, e := sweep.Tree(r, rev, sweep.Words(cfg.Words), cfg.Exclude...)
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

// message is the release commit's message: a file if given, otherwise a
// dated line, the project's name, and whatever sign-off line the last
// commit on the branch ends with, so the release is signed the way the
// work was.
func message(r repo.Repo, file, public string) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	name := project(r, public)
	last, _ := r.Git("log", "-1", "--format=%B")
	lines := strings.Split(strings.TrimSpace(last), "\n")
	sign := ""
	if len(lines) > 0 && !strings.Contains(lines[len(lines)-1], " ") {
		sign = "\n\n" + lines[len(lines)-1]
	}
	return fmt.Sprintf("%s, as it stands on %s\n\ncut from a private branch whose tree this is exactly, proven by hash.\nwhat the private history holds is the road here.%s", name, time.Now().Format("2006-01-02"), sign), nil
}

// multi is a repeatable flag.
type multi []string

// String is the flag's values, joined.
func (m *multi) String() string { return strings.Join(*m, ",") }

// Set adds one more value.
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

// usage is the one line to type when the verb was wrong.
func usage() {
	fmt.Fprintln(os.Stderr, "game check | build [NAME] | clean [--cache] | lint | release [--public PATH] [--message FILE] [--exclude PREFIX]... | bounce [RANGE] | sweep [REV] | version")
}

// age is which commit this binary is and how long ago it was built.
func age() string {
	if built == "" {
		return fmt.Sprintf("build %s, not stamped", build)
	}
	t, err := time.Parse(time.RFC3339, built)
	if err != nil {
		return fmt.Sprintf("build %s, built %s", build, built)
	}
	return fmt.Sprintf("build %s, %s old", build, time.Since(t).Round(time.Second))
}

// project is the project's name: the public root's, "thing" for
// thing-root.git, since a checkout is named for whoever holds it; else
// the module path's last element; else the directory.
func project(r repo.Repo, public string) string {
	base := filepath.Base(public)
	base = strings.TrimSuffix(base, ".git")
	base = strings.TrimSuffix(base, "-root")
	if base != "" && base != "." && base != "/" {
		return base
	}
	if mod, err := os.ReadFile(filepath.Join(r.Dir, "go.mod")); err == nil {
		for _, line := range strings.Split(string(mod), "\n") {
			if strings.HasPrefix(line, "module ") {
				return filepath.Base(strings.TrimSpace(strings.TrimPrefix(line, "module ")))
			}
		}
	}
	return filepath.Base(r.Dir)
}

// check is the gate: gofmt has nothing to say, vet passes, the tests
// pass, and the exit code is the tests', never a pipe's.
func check(r repo.Repo) error {
	out, err := run(r.Dir, "gofmt", "-l", ".")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("gofmt would change: %s", strings.ReplaceAll(strings.TrimSpace(out), "\n", " "))
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
	stamp := fmt.Sprintf("-X main.build=%s -X main.built=%s", commit, time.Now().UTC().Format(time.RFC3339))
	os.MkdirAll(filepath.Join(r.Dir, "bin"), 0o755)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := run(r.Dir, "go", "build", "-ldflags", stamp, "-o", filepath.Join("bin", e.Name()), "./cmd/"+e.Name()); err != nil {
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
		return out.String(), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

// clean is go clean, which knows what go build left behind, plus bin/,
// which go does not know about because it is ours.
func clean(r repo.Repo, cache bool) error {
	if _, err := run(r.Dir, "go", "clean", "./..."); err != nil {
		return err
	}
	if cache {
		if _, err := run(r.Dir, "go", "clean", "-cache", "-testcache"); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(filepath.Join(r.Dir, "bin")); err != nil {
		return err
	}
	fmt.Println("clean: bin/ away" + map[bool]string{true: ", caches forgotten", false: ""}[cache])
	return nil
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
		return fmt.Errorf("no target called %q; described: %s", name, strings.Join(names, ", "))
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
		fmt.Printf("build %s: step %d of %d: %s\n", name, i+1, len(t.Steps), strings.Join(step, " "))
		fmt.Fprintf(logf, "== step %d: %s\n", i+1, strings.Join(step, " "))
		cmd := exec.Command("nice", append([]string{"-n", "19"}, step...)...)
		cmd.Dir = t.Dir
		cmd.Stdout, cmd.Stderr = logf, logf
		if err := cmd.Run(); err != nil {
			tail(logPath, 12)
			return fmt.Errorf("build %s: step %d failed after %s; the log is %s", name, i+1, time.Since(start).Round(time.Second), logPath)
		}
	}
	tail(logPath, 4)
	fmt.Printf("build %s: done in %s; the log is %s\n", name, time.Since(start).Round(time.Second), logPath)
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
