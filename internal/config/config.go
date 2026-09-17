// Package config is game's settings: one per line, "key value" or
// "key = value", # comments, ~ for home. A project's own settings are a
// block, opened by "project NAME" and held by the indent under it.
//
// Two files, the dotfile in the home directory and .game in the
// repository, folded in that order so the repository's wins, with two
// exceptions.
//
// The sweep's word list is the dotfile's alone, so a repository cannot
// shorten what it may not say. A repository's lint lines replace the
// dotfile's rather than add to them, so a repository that describes any
// lint describes all of it.
//
// An unknown key is an error that names the file and line, because a
// misspelt key that is silently ignored is a setting that silently does
// nothing.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is every setting game knows.
type Config struct {
	// the name the bounce refuses in the third person
	Author   string
	Words    string             // the sweep's word list, one per line
	Name     string             // the project, from .game; names its block
	Projects map[string]Project // the builder's, one block per project
	Exclude  []string           // path prefixes the sweep leaves alone
	// Omit is files that belong on the road and in no release. the
	// sweep's exclude only scopes what is scanned; a file nobody scans
	// still ships, so keeping it out is a separate word.
	Omit []string
	// SweepAllow is, per file, the kinds of sweep hit the repository
	// means to carry: a forensics tool's marks are its product. per
	// file and per kind, so a real leak in the same file is still found.
	SweepAllow map[string][]string
	// the lint, as described; none described is no lint
	Lint    []Rule
	Targets []Target // builds described beyond go's own, by name
	// things to run, described the same way: run NAME DIR :: CMD
	Runs []Target
	// settings the repository needs, declared in .game
	Needs []Need
	Stack [][2]string // a stack's members, name and ref, in order
	// the builder's values, from the dotfile only
	Set map[string]string
}

// Project is one project's part of the builder's dotfile: its two
// remotes and the values it is given.
//
// The project is the unit because one machine builds several, and a key
// that has to name its project on every line is a key that will
// eventually disagree with itself. A block is opened by its name and
// holds the lines indented under it:
//
//	project game
//		road ~/roots/game-road.git
//		dist git@github.com:ada/game.git
//		set CONTACT ada@example.com
//
// The repository says which block is its own, with name in .game, and
// nothing in the repository ever holds a remote or a value.
type Project struct {
	// Road is the dirty remote, which takes every commit: game push.
	Road string

	// Dist is the clean remote, which only game release push sends to.
	Dist string

	// Set is the values this project is given, over the dotfile's.
	Set map[string]string
}

// Need is a setting a repository declares it needs, and why, so a
// builder without it is told what to set rather than handed a program
// that fails later. The value is never in the repository: it is the
// builder's, in their own dotfile, and a release carries only this.
//
//	needs KINGFISHER_CONTACT a name and email the data providers can reach
type Need struct {
	Name string
	Why  string
}

// Target is a build, or a run, described in the config: a name, the
// directory each step runs in, and the steps in order, one per line:
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
//	lint paragraph 4       no paragraph of prose longer, docs or comments
//	lint comments          every function commented on the line above
//	lint print             no fmt.Print outside package main
//	lint exclaim           no exclamation marks in docs
//	lint shout             capitals of five or more in comments, counted
//	lint words a b c       words banned from code and docs
//	lint allow FILE name   that one file is not held to that one rule
type Rule struct {
	Name string
	Args []string
}

// numeric is the lint rules whose argument is a number, where the
// strictest of two is the smaller: a narrower column, a shorter readme, a
// shorter paragraph.
var numeric = map[string]bool{"width": true, "rows": true, "paragraph": true}

// Strictest folds several descriptions of lint into the one that refuses
// the most.
//
// A stack is published as one thing, so it holds every member to every
// member's rule: a number is the smallest any of them asked for, a rule
// with no argument is on if any of them has it, and a word list is the
// union, because a word one member will not say is not said here.
//
// An allowance is per file, so it cannot collide: every one of them is
// kept, and two for the same file are merged.
func Strictest(sets ...[]Rule) []Rule {
	var order []string
	held := map[string]Rule{}
	for _, set := range sets {
		for _, r := range set {
			key := r.Name
			if r.Name == "allow" && len(r.Args) > 0 {
				key = "allow " + r.Args[0]
			}
			was, seen := held[key]
			if !seen {
				order = append(order, key)
				held[key] = r
				continue
			}
			switch {
			case numeric[r.Name]:
				if less(r.Args, was.Args) {
					held[key] = r
				}
			case r.Name == "words":
				held[key] = Rule{
					Name: r.Name,
					Args: union(was.Args, r.Args),
				}
			case r.Name == "allow":
				held[key] = Rule{
					Name: r.Name,
					Args: append(
						[]string{r.Args[0]},
						union(
							was.Args[1:],
							r.Args[1:],
						)...,
					),
				}
			}
		}
	}
	out := make([]Rule, 0, len(order))
	for _, name := range order {
		out = append(out, held[name])
	}
	return out
}

// less is whether one numeric rule's argument is smaller than another's.
// A rule with no number, or one that is not a number, never wins: a rule
// that cannot be compared cannot be the stricter of the two.
func less(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	x, err := strconv.Atoi(a[0])
	if err != nil {
		return false
	}
	y, err := strconv.Atoi(b[0])
	if err != nil {
		return false
	}
	return x < y
}

// allowance is one "lint allow FILE name..." line.
//
// an exemption names a file and a rule, never a rule alone: a page that
// has to run long says so in a line a reader can grep, and every other
// page stays held to it.
func allowance(path string, n int, f []string) (Rule, error) {
	if len(f) < 3 {
		return Rule{}, fmt.Errorf(
			"%s:%d: lint allow wants a file and a lint, as in "+
				"\"lint allow OPERATION.md paragraph\"",
			path,
			n,
		)
	}
	for _, name := range f[2:] {
		known := false
		for _, r := range Rules {
			known = known || r == name
		}
		if !known {
			return Rule{}, fmt.Errorf(
				"%s:%d: unknown lint %q; the lints are %s",
				path,
				n,
				name,
				strings.Join(Rules, ", "),
			)
		}
	}
	return Rule{Name: "allow", Args: f[1:]}, nil
}

// union is every word in either list, once, in the order first seen.
func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{a, b} {
		for _, w := range list {
			if !seen[w] {
				seen[w] = true
				out = append(out, w)
			}
		}
	}
	return out
}

// Keys is what a file may say, and what an unknown key is measured
// against.
// SweepKinds are the kinds of sweep hit a .game may declare it carries.
var SweepKinds = []string{
	"paths", "sessions", "uuids", "emails", "author", "word",
}

var Keys = []string{
	"author",
	"words",
	"name",
	"road",
	"dist",
	"exclude",
	"omit",
	"sweep",
	"lint",
	"target",
	"run",
	"needs",
	"set",
	"stack",
}

// Rules is the lint names game knows.
var Rules = []string{
	"width",
	"rows",
	"paragraph",
	"comments",
	"print",
	"exclaim",
	"shout",
	"words",
}

// Defaults is a config for a repository with no files at all.
func Defaults(home, repo string) Config {
	return Config{
		Words: filepath.Join(home, ".config", "game", "sweep.words"),
	}
}

// Load is the defaults, then the dotfile, then the repository's .game.
// A missing file is skipped; a malformed one is an error.
//
// A repository that describes any lint describes all of it: its lint
// lines replace the dotfile's, so a repository can say exactly which
// rules it holds to today, and the dotfile is what a repository that
// says nothing gets.
//
// It reads the config at whose and expands ~ against home. They are one
// directory for a person in a shell.
//
// For an agent they are not: the config is the agent's, in its anchor,
// and a ~ in it still means this machine's user, since the roads it
// names belong to the estate rather than to the agent.
func Load(whose, home, repo string) (Config, error) {
	c := Defaults(home, repo)
	dotfile := filepath.Join(whose, ".config", "game", "config")
	if err := c.fold(dotfile, home, true); err != nil {
		return c, err
	}
	mine := c.Lint
	c.Lint = nil
	if err := c.fold(
		filepath.Join(repo, ".game"), home, false,
	); err != nil {
		return c, err
	}
	if len(c.Lint) == 0 {
		c.Lint = mine
	}
	for name, value := range c.Projects[c.Name].Set {
		if c.Set == nil {
			c.Set = map[string]string{}
		}
		c.Set[name] = value
	}
	return c, nil
}

// Mine is the block for the project the repository says it is, and
// whether there is one.
//
// A tree whose project the dotfile does not describe is a local build by
// definition: there is nowhere for a release to go, and game says so
// rather than inventing a remote. That is also the cheapest way to test
// game on somebody else's repository.
func (c Config) Mine() (Project, bool) {
	p, held := c.Projects[c.Name]
	return p, held
}

// Remotes is every project's clean remote, or every project's dirty one,
// which is what a stack needs to clone its members from.
func (c Config) Remotes(clean bool) map[string]string {
	out := map[string]string{}
	for name, p := range c.Projects {
		if url := p.Road; !clean && url != "" {
			out[name] = url
		}
		if url := p.Dist; clean && url != "" {
			out[name] = url
		}
	}
	return out
}

// fold reads one file into the config. words is whether this file is
// the builder's dotfile: only it may set the word list and give values,
// and only a repository may declare what it needs.
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
func (c *Config) Parse(
	r interface{ Read([]byte) (int, error) },
	path, home string,
	words bool,
) error {
	sc := bufio.NewScanner(r)
	n, open := 0, ""
	for sc.Scan() {
		n++
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// a project's lines are the indented ones under it, so the
		// first line back at the margin closes the block.
		if raw[0] != '\t' && raw[0] != ' ' {
			open = ""
		}
		key, val, err := split(line)
		if err != nil {
			return fmt.Errorf("%s:%d: %v", path, n, err)
		}
		val = expand(val, home)
		if open != "" && key != "road" && key != "dist" &&
			key != "set" && key != "project" {
			return fmt.Errorf(
				"%s:%d: %s is not a project's; a block holds "+
					"road, dist and set, and %s belongs "+
					"at the margin",
				path,
				n,
				key,
				key,
			)
		}
		switch key {
		case "author":
			c.Author = val
		case "words":
			if words {
				c.Words = val
			}
		case "name":
			if words {
				return fmt.Errorf(
					"%s:%d: name is a "+
						"repository's; the dotfile "+
						"names remotes with road "+
						"and dist",
					path,
					n,
				)
			}
			c.Name = val
		case "project":
			if !words {
				return fmt.Errorf(
					"%s:%d: a project block is the "+
						"builder's; a repository "+
						"says which one is its own "+
						"with name",
					path,
					n,
				)
			}
			if val == "" {
				return fmt.Errorf(
					"%s:%d: project wants \"project NAME\"",
					path,
					n,
				)
			}
			open = val
			if c.Projects == nil {
				c.Projects = map[string]Project{}
			}
			if _, held := c.Projects[open]; !held {
				c.Projects[open] = Project{}
			}
		case "road", "dist":
			if !words {
				return fmt.Errorf(
					"%s:%d: %s is the builder's, in "+
						"~/.config/game/config "+
						"under its project; a "+
						"repository never holds a "+
						"remote",
					path,
					n,
					key,
				)
			}
			if open == "" {
				return fmt.Errorf(
					"%s:%d: %s sits inside a project "+
						"block: \"project NAME\", "+
						"then %s indented under it",
					path,
					n,
					key,
					key,
				)
			}
			if val == "" {
				return fmt.Errorf(
					"%s:%d: %s wants a url",
					path,
					n,
					key,
				)
			}
			if first, _, cut := strings.Cut(val, " "); cut &&
				first == open {
				return fmt.Errorf(
					"%s:%d: the block already names "+
						"%s; %s takes the url alone",
					path,
					n,
					open,
					key,
				)
			}
			held := c.Projects[open]
			if key == "road" {
				held.Road = val
			} else {
				held.Dist = val
			}
			c.Projects[open] = held
		case "public":
			return fmt.Errorf(
				"%s:%d: \"public\" is now \"dist\": "+
					"the clean remote only game release "+
					"pushes to; the dirty one is "+
					"\"road\"",
				path,
				n,
			)
		case "exclude":
			c.Exclude = append(c.Exclude, val)
		case "stack":
			if words {
				return fmt.Errorf(
					"%s:%d: a stack is a "+
						"repository's; the dotfile "+
						"says where each member "+
						"lives",
					path,
					n,
				)
			}
			name, ref, _ := strings.Cut(val, " ")
			if strings.TrimSpace(ref) == "" {
				return fmt.Errorf(
					"%s:%d: stack wants \"stack NAME REF\"",
					path,
					n,
				)
			}
			c.Stack = append(
				c.Stack,
				[2]string{name, strings.TrimSpace(ref)},
			)
		case "needs":
			if words {
				return fmt.Errorf(
					"%s:%d: needs is a "+
						"repository's declaration; "+
						"the dotfile gives values "+
						"with set",
					path,
					n,
				)
			}
			name, why, _ := strings.Cut(val, " ")
			if strings.TrimSpace(why) == "" {
				return fmt.Errorf(
					"%s:%d: needs wants \"needs "+
						"NAME why it is needed\"",
					path,
					n,
				)
			}
			c.Needs = append(
				c.Needs,
				Need{Name: name, Why: strings.TrimSpace(why)},
			)
		case "set":
			if !words {
				return fmt.Errorf(
					"%s:%d: a value never lives "+
						"in a repository; set it in "+
						"~/.config/game/config, and "+
						"declare it here with needs",
					path,
					n,
				)
			}
			name, value, _ := strings.Cut(val, " ")
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf(
					"%s:%d: set wants \"set NAME value\"",
					path,
					n,
				)
			}
			if open != "" {
				held := c.Projects[open]
				if held.Set == nil {
					held.Set = map[string]string{}
				}
				held.Set[name] = strings.TrimSpace(value)
				c.Projects[open] = held
				break
			}
			if c.Set == nil {
				c.Set = map[string]string{}
			}
			c.Set[name] = strings.TrimSpace(value)
		case "target", "run":
			name, rest, ok := strings.Cut(val, " ")
			dir, cmd, ok2 := strings.Cut(
				strings.TrimSpace(rest),
				"::",
			)
			if !ok || !ok2 || strings.TrimSpace(cmd) == "" {
				return fmt.Errorf(
					"%s:%d: %s wants \"%s NAME "+
						"DIR :: COMMAND ...\"",
					path,
					n,
					key,
					key,
				)
			}
			dir = expand(strings.TrimSpace(dir), home)
			step := strings.Fields(strings.TrimSpace(cmd))
			for i := range step {
				step[i] = expand(step[i], home)
			}
			list := &c.Targets
			if key == "run" {
				list = &c.Runs
			}
			found := false
			for i := range *list {
				if (*list)[i].Name == name {
					(*list)[i].Steps = append(
						(*list)[i].Steps,
						Step{Dir: dir, Args: step},
					)
					found = true
				}
			}
			if !found {
				*list = append(
					*list,
					Target{
						Name: name,
						Steps: []Step{
							{Dir: dir, Args: step},
						},
					},
				)
			}
		case "omit":
			c.Omit = append(c.Omit, val)
		case "sweep":
			f := strings.Fields(val)
			if len(f) < 3 || f[0] != "allow" {
				return fmt.Errorf(
					"%s:%d: sweep wants "+
						"allow FILE KIND...; the "+
						"kinds are %s",
					path, n,
					strings.Join(SweepKinds, ", "),
				)
			}
			for _, k := range f[2:] {
				known := false
				for _, x := range SweepKinds {
					known = known || x == k
				}
				if !known {
					return fmt.Errorf(
						"%s:%d: unknown sweep kind "+
							"%q; the kinds are %s",
						path, n, k,
						strings.Join(
							SweepKinds, ", "),
					)
				}
			}
			if c.SweepAllow == nil {
				c.SweepAllow = map[string][]string{}
			}
			c.SweepAllow[f[1]] = append(
				c.SweepAllow[f[1]], f[2:]...)
		case "lint":
			f := strings.Fields(val)
			if len(f) == 0 {
				return fmt.Errorf(
					"%s:%d: lint wants a rule; "+
						"the lints are %s",
					path,
					n,
					strings.Join(Rules, ", "),
				)
			}
			if f[0] == "allow" {
				r, e := allowance(path, n, f)
				if e != nil {
					return e
				}
				c.Lint = append(c.Lint, r)
				break
			}
			known := false
			for _, r := range Rules {
				known = known || r == f[0]
			}
			if !known {
				return fmt.Errorf(
					"%s:%d: unknown lint %q; "+
						"the lints are %s",
					path,
					n,
					f[0],
					strings.Join(Rules, ", "),
				)
			}
			c.Lint = append(c.Lint, Rule{Name: f[0], Args: f[1:]})
		default:
			return fmt.Errorf(
				"%s:%d: unknown key %q; the keys are %s",
				path,
				n,
				key,
				strings.Join(Keys, ", "),
			)
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
		key, val = line[:i], strings.TrimSpace(
			strings.TrimLeft(line[i:], " \t="),
		)
	} else {
		key = line
	}
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" || val == "" {
		return "", "", fmt.Errorf("want \"key value\", got %q", line)
	}
	if len(val) >= 2 &&
		(val[0] == '"' && val[len(val)-1] == '"' ||
			val[0] == '\'' && val[len(val)-1] == '\'') {
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

// Missing is every need the builder has not set, each with its reason,
// so a build can refuse with what to do rather than start and fail.
func (c Config) Missing() []Need {
	var out []Need
	for _, n := range c.Needs {
		if _, ok := c.Set[n.Name]; !ok {
			out = append(out, n)
		}
	}
	return out
}

// Env is the needed settings as NAME=value, for a build or a run: only
// what the repository declared, so a builder's other values never reach
// a program that did not ask for them.
func (c Config) Env() []string {
	var out []string
	for _, n := range c.Needs {
		if v, ok := c.Set[n.Name]; ok {
			out = append(out, n.Name+"="+v)
		}
	}
	return out
}
