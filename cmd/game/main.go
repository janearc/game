// game: the verbs a go project needs that go itself does not know are
// things. check, build, release, bounce, sweep, version. it runs in any
// shell and needs git and go and nothing else; there is no makefile
// because there is nothing left for one to say. the window it will one
// day draw in is another program's business.
//
//	game check                    gofmt -l, go vet, go test, exit code kept
//	game build                    every cmd/* into bin/, stamped with the
//	                              commit and the time it was built
//	game release [--public PATH] [--message FILE] [--exclude PREFIX]...
//	game bounce [RANGE]           what is staged, or a commit or range
//	game sweep [REV]              the tree at a revision, default HEAD
//	game version                  which commit this binary is, and its age
//
// settings come from ~/.config/game/config and then .game in the
// repository, one per line, the repository's winning and a flag winning
// over both; the word list is the dotfile's alone:
//
//	author  Jane                        the name the bounce refuses
//	words   ~/.config/game/sweep.words  words the sweep refuses, one per line
//	public  ../daffy-root.git           the public root, relative to the repo
//	exclude spec-docs/                  a prefix the sweep leaves alone; repeatable
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/janearc/game/internal/bounce"
	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// build and built are stamped by the makefile.
var (
	build = "dev"
	built = ""
)

// config is the dotfile, with the defaults a repository gets without one.
type config struct {
	Author  string
	Words   string
	Public  string
	Exclude []string
}

// load reads ~/.config/game/config and then .game in the repository,
// the same keys, the repository's winning; a missing file is skipped.
// the sweep's word list is only ever the dotfile's: a repository must
// not be able to shorten the list of what it may not say.
func load() config {
	home := os.Getenv("HOME")
	c := config{Words: filepath.Join(home, ".config/game/sweep.words"), Public: "../" + filepath.Base(cwd()) + "-root.git"}
	c.read(filepath.Join(home, ".config/game/config"), true)
	c.read(filepath.Join(cwd(), ".game"), false)
	return c
}

// read folds one file into the config; words is honoured only for the
// dotfile.
func (c *config) read(path string, words bool) {
	home := os.Getenv("HOME")
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		val = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(val), "="))
		val = strings.Replace(val, "~", home, 1)
		switch strings.ToLower(key) {
		case "author":
			c.Author = val
		case "words":
			if words {
				c.Words = val
			}
		case "public":
			c.Public = val
		case "exclude":
			c.Exclude = append(c.Exclude, val)
		}
	}
}

func cwd() string {
	d, _ := os.Getwd()
	return d
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfg := load()
	r := repo.Repo{Dir: cwd()}
	var err error
	switch os.Args[1] {
	case "version", "--age":
		fmt.Println(age())
	case "check":
		err = check(r)
	case "build":
		err = buildBinaries(r)
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

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

func usage() {
	fmt.Fprintln(os.Stderr, "game check | build | release [--public PATH] [--message FILE] [--exclude PREFIX]... | bounce [RANGE] | sweep [REV] | version")
}

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

// project is the project's name: the public root's, "daffy" for
// daffy-root.git, since a checkout is named for whoever holds it; else
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
