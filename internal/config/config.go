// Package config reads game's settings.
//
// there are two files: the dotfile in your home directory, then .game in the
// repository, which is read second and wins.
//
// each line is "key value" or "key = value". # starts a comment and ~ is
// home. a "project NAME" line opens a block of indented lines.
//
// the sweep's word list comes from the dotfile only. lint lines in the
// repository replace the dotfile's. an unknown key is an error naming the
// file and line.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Config is every setting game knows.
type Config struct {
	// the name the bounce refuses in the third person
	Author   string
	Words    string             // the sweep's word list, one per line
	Name     string             // the project, from .game's name line
	Projects map[string]Project // yours, one block per project
	// Named is the project's name as game uses it. Load sets it, and
	// NamedBy says where it came from, in words a message can carry.
	Named   string
	NamedBy string
	// Dotfile is the path of the config that was read, so a message can
	// name it.
	Dotfile string
	Exclude []string // path prefixes the sweep leaves alone
	// Omit is files that stay on the road and are left out of every
	// release. exclude only limits what the sweep scans.
	Omit []string
	// SweepAllow is, per file, the kinds of sweep hit the repository
	// means to carry. a real leak of another kind in the same file is
	// still found.
	SweepAllow map[string][]string
	// License is a path to the one license every release carries. it
	// lives in your config, not in a repository.
	License string
	// the lint, as described; none described is no lint
	Lint    []Rule
	Targets []Target // builds described beyond go's own, by name
	// things to run, described the same way: run NAME DIR :: CMD
	Runs []Target
	// settings the repository needs, declared in .game
	Needs []Need
	Stack [][2]string // a stack's members, name and ref, in order
	// your values, from the dotfile only
	Set map[string]string
}

// Project is one project's block in your dotfile: its two remotes and the
// values it is given.
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

// Need is a setting a repository declares it needs, and why. if you
// haven't set it, game tells you what to set before anything runs. the
// value lives in your dotfile, never in the repository.
//
//	needs CONTACT a name and email the data providers can reach
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

// numeric is the lint rules that take a number. for these the smaller number
// is the stricter.
var numeric = map[string]bool{"width": true, "rows": true, "paragraph": true}

// Strictest combines every member's lint rules into the strictest set. a
// number is the smallest any member asks for, word lists are merged, and a
// rule without arguments is on if any member has it. allowances are kept
// per file and merged.
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

// less checks if one rule's number is smaller than another's.
// a rule with no number cannot be stricter.
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
// an exemption names a file and a rule. a rule alone is not allowed.
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

// SweepKinds are the kinds of sweep hit a .game may declare it carries.
var SweepKinds = []string{
	"paths", "sessions", "uuids", "emails", "author", "word",
}

// Keys is what a file may say. an unknown key is an error.
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

// Load reads the defaults, then the dotfile under whose, then the
// repository's .game. a missing file is skipped and a malformed one is an
// error.
//
// ~ expands against home, which is not always whose: an agent's config is in
// its anchor, and ~ in it is still this machine's user.
//
// lint lines in the repository replace the dotfile's. the dotfile's lint is
// used when the repository has none.
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
	c.Dotfile = dotfile
	c.Named, c.NamedBy = named(c.Name, repo)
	for name, value := range c.Projects[c.Named].Set {
		if c.Set == nil {
			c.Set = map[string]string{}
		}
		c.Set[name] = value
	}
	return c, nil
}

// Mine is the block for the project the repository says it is, and whether
// there is one. a project with no block is a local build.
func (c Config) Mine() (Project, bool) {
	p, held := c.Projects[c.Named]
	return p, held
}

// named returns the tree's name and where it came from. the name line in
// .game wins, then the last element of go.mod's module path, then the
// directory name.
func named(fromGame, repo string) (name, by string) {
	if fromGame != "" {
		return fromGame, "the name line in .game"
	}
	if mod := module(repo); mod != "" {
		return last(mod), "go.mod's module " + mod
	}
	return filepath.Base(repo), "the directory's name"
}

// module is the path a tree's go.mod declares, or empty without one.
func module(repo string) string {
	b, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(
				strings.TrimPrefix(line, "module "),
			)
		}
	}
	return ""
}

// last is the project name from a module path. a major version suffix
// is go's, not the project's: example/v2 is example.
func last(mod string) string {
	parts := strings.Split(strings.Trim(mod, "/"), "/")
	name := parts[len(parts)-1]
	if len(parts) > 1 && len(name) > 1 && name[0] == 'v' {
		if _, err := strconv.Atoi(name[1:]); err == nil {
			name = parts[len(parts)-2]
		}
	}
	return name
}

// NoBlock is the refusal when a tree has no block. it shows the name
// looked for, where it came from, which file was read, and the blocks
// that file does have.
func (c Config) NoBlock() error {
	blocks := make([]string, 0, len(c.Projects))
	for name := range c.Projects {
		blocks = append(blocks, name)
	}
	sort.Strings(blocks)
	has := "it has no project blocks at all"
	if len(blocks) > 0 {
		has = "its project blocks are " + strings.Join(blocks, ", ")
	}
	return fmt.Errorf(
		"no project block for %s in %s; the name is from %s, and %s. "+
			"open a \"project %s\" block there to give it a road "+
			"and a dist",
		c.Named, c.Dotfile, c.NamedBy, has, c.Named,
	)
}

// Remotes maps each project's name to its dist when clean is true, and to its
// road otherwise. projects with none are left out.
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

// fold reads a file into the config. words means this is your dotfile,
// which sets the word list and values. only a repository declares needs.
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
		// a project's lines are indented. the first unindented line
		// ends the project block.
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
		case "license":
			c.License = val
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

// split splits "key value" or "key = value". the key is lowercased and the
// value is unquoted. a line with no value is an error.
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

// Missing lists every need not set, with reason. build can refuse early.
func (c Config) Missing() []Need {
	var out []Need
	for _, n := range c.Needs {
		if _, ok := c.Set[n.Name]; !ok {
			out = append(out, n)
		}
	}
	return out
}

// Env is the declared settings as NAME=value. only what the repo declared.
func (c Config) Env() []string {
	var out []string
	for _, n := range c.Needs {
		if v, ok := c.Set[n.Name]; ok {
			out = append(out, n.Name+"="+v)
		}
	}
	return out
}

// Whose returns GAME_HOME if set, else the shell's HOME.
func Whose() string {
	if at := os.Getenv("GAME_HOME"); at != "" {
		return at
	}
	return os.Getenv("HOME")
}
