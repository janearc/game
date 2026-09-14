// Package config is game's settings: one per line, "key value" or
// "key = value", # comments, ~ for home. Two files, the dotfile in the
// home directory and .game in the repository, folded in that order so
// the repository's wins, with two exceptions: the sweep's word list is
// the dotfile's alone, so a repository cannot shorten what it may not
// say; and a repository's lint lines replace the dotfile's rather than
// add to them, so a repository that describes any lint describes all
// of it. An unknown key is an error that names the file and line, because
// a misspelt key that is silently ignored is a setting that silently
// does nothing.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is every setting game knows.
type Config struct {
	Author  string   // the name the bounce refuses in the third person
	Words   string   // the sweep's word list, one per line
	Public  string   // the public root a release goes to
	Exclude []string // path prefixes the sweep leaves alone
	Lint    []Rule   // the lint, as described; none described is no lint
	Targets []Target // builds described beyond go's own, by name
}

// Target is a build described in the config: a name, the directory it
// runs in, and its steps in order, one per line:
//
//	target vt ~/src/ghostty :: ~/.local/zig/0.15.2/zig build -Demit-lib-vt
//	target vt ~/src/ghostty :: cp zig-out/lib/libghostty-vt.a ../bin/
//
// game build NAME runs the steps in order, each in its own directory,
// under nice so the rest of the machine keeps its share, with the
// output kept in bin/NAME.log.
type Target struct {
	Name  string
	Steps []Step
}

// Step is one line of a target: where it runs, and what.
type Step struct {
	Dir  string
	Args []string
}

// Rule is one line of lint: a name and its arguments. game knows a
// fixed set of names; what they are set to is whoever's lint this is.
//
//	lint width 80          prose (docs, comments) no wider; code counted
//	lint rows 25           a readme no taller
//	lint comments          every function commented on the line above
//	lint print             no fmt.Print outside package main
//	lint exclaim           no exclamation marks in docs
//	lint shout             capitals of five or more in comments, counted
//	lint words a b c       words banned from code and docs
type Rule struct {
	Name string
	Args []string
}

// Keys is what a file may say, and what an unknown key is measured
// against.
var Keys = []string{"author", "words", "public", "exclude", "lint", "target"}

// Rules is the lint names game knows.
var Rules = []string{"width", "rows", "comments", "print", "exclaim", "shout", "words"}

// Defaults is a config for a repository with no files at all.
func Defaults(home, repo string) Config {
	return Config{
		Words:  filepath.Join(home, ".config", "game", "sweep.words"),
		Public: "../" + filepath.Base(repo) + "-root.git",
	}
}

// Load is the defaults, then the dotfile, then the repository's .game.
// A missing file is skipped; a malformed one is an error. A repository
// that describes any lint describes all of it: its lint lines replace
// the dotfile's, so a repository can say exactly which rules it holds
// to today, and the dotfile is what a repository that says nothing
// gets.
func Load(home, repo string) (Config, error) {
	c := Defaults(home, repo)
	if err := c.fold(filepath.Join(home, ".config", "game", "config"), home, true); err != nil {
		return c, err
	}
	mine := c.Lint
	c.Lint = nil
	if err := c.fold(filepath.Join(repo, ".game"), home, false); err != nil {
		return c, err
	}
	if len(c.Lint) == 0 {
		c.Lint = mine
	}
	return c, nil
}

// fold reads one file into the config. words is whether this file may
// set the word list.
func (c *Config) fold(path, home string, words bool) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	return c.Parse(f, path, home, words)
}

// Parse reads settings from a reader; path names it in errors.
func (c *Config) Parse(r interface{ Read([]byte) (int, error) }, path, home string, words bool) error {
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, err := split(line)
		if err != nil {
			return fmt.Errorf("%s:%d: %v", path, n, err)
		}
		val = expand(val, home)
		switch key {
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
		case "target":
			name, rest, ok := strings.Cut(val, " ")
			dir, cmd, ok2 := strings.Cut(strings.TrimSpace(rest), "::")
			if !ok || !ok2 || strings.TrimSpace(cmd) == "" {
				return fmt.Errorf("%s:%d: target wants \"target NAME DIR :: COMMAND ...\"", path, n)
			}
			dir = expand(strings.TrimSpace(dir), home)
			step := strings.Fields(strings.TrimSpace(cmd))
			for i := range step {
				step[i] = expand(step[i], home)
			}
			found := false
			for i := range c.Targets {
				if c.Targets[i].Name == name {
					c.Targets[i].Steps = append(c.Targets[i].Steps, Step{Dir: dir, Args: step})
					found = true
				}
			}
			if !found {
				c.Targets = append(c.Targets, Target{Name: name, Steps: []Step{{Dir: dir, Args: step}}})
			}
		case "lint":
			f := strings.Fields(val)
			known := false
			for _, r := range Rules {
				known = known || r == f[0]
			}
			if !known {
				return fmt.Errorf("%s:%d: unknown lint %q; the lints are %s", path, n, f[0], strings.Join(Rules, ", "))
			}
			c.Lint = append(c.Lint, Rule{Name: f[0], Args: f[1:]})
		default:
			return fmt.Errorf("%s:%d: unknown key %q; the keys are %s", path, n, key, strings.Join(Keys, ", "))
		}
	}
	return sc.Err()
}

// split is "key value" or "key = value", the key lowercased, the value
// trimmed and unquoted if it was quoted; a line with no value is an
// error, since every key takes one.
func split(line string) (string, string, error) {
	var key, val string
	if i := strings.IndexAny(line, " \t="); i >= 0 {
		key, val = line[:i], strings.TrimSpace(strings.TrimLeft(line[i:], " \t="))
	} else {
		key = line
	}
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" || val == "" {
		return "", "", fmt.Errorf("want \"key value\", got %q", line)
	}
	if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
		val = val[1 : len(val)-1]
	}
	return key, val, nil
}

// expand turns a leading ~ into the home directory.
func expand(val, home string) string {
	if val == "~" {
		return home
	}
	if strings.HasPrefix(val, "~/") {
		return filepath.Join(home, val[2:])
	}
	return val
}
