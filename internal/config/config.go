// Package config reads game's settings.
//
// there are two files. the dotfile in your home directory is read first, then
// .game in the repository, which is read second and wins.
//
// each line is "key value" or "key = value". # starts a comment and ~ is
// home. a "project NAME" line opens a block of indented lines.
//
// pzm reads the grammar into the file type declared in file.go: keys are
// lowercase, a block's lines are indented with tabs, spaces or both,
// and a command follows " :: ". this package settles what lines mean.
//
// the sweep's word list comes from the dotfile only. lint lines in the
// repository replace the dotfile's. a line that cannot stand is an error
// naming the file, line and key, as file:line: key: why; a file with
// several says them all.
package config

import (
	"cmp"
	"errors"
	"fmt"
	"github.com/janearc/game/internal/count"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Config is every setting game knows. Load reads it from your dotfile
// and the repository's .game; each setting is asked for by a method.
type Config struct {
	// the name the bounce refuses in the third person
	author   string
	words    string             // the sweep's word list, one per line
	name     string             // the project, from .game's name line
	projects map[string]Project // yours, one block per project
	// named is the project's name. Load sets it, and namedBy says where it
	// came from.
	named   string
	namedBy string
	// dotfile is the path of the config that was read.
	dotfile string
	exclude []string // path prefixes the sweep leaves alone
	// omit is files and directories left out of every release. exclude
	// only limits what the sweep scans.
	omit []string
	// sweepAllow is, per file, the kinds of sweep hit the repository means
	// to carry. other kinds in the same file are still found.
	sweepAllow map[string][]string
	// license is a path to the one license every release carries. it
	// lives in your config, not in a repository.
	license string
	// the lint, as described; none described is no lint
	lint    []Rule
	targets []Target // builds described beyond go's own, by name
	// things to run, described the same way: run NAME DIR :: CMD
	runs []Target
	// settings the repository needs, declared in .game
	needs []Need
	stack []Member // a stack's members, name and ref, in order
	// patches is the directories, in the repository, where the stack keeps
	// members' patches beside patches/: NAME under each, from .game only
	patches []string
	// theirs is the members marked theirs: someone else's code, whose
	// addresses, urls, uuids and paths a release does not hold against it
	theirs []string
	// your values, from the dotfile only
	set map[string]string
	// rapidChecks is how many checks a property test runs under game
	// check. zero keeps the library's own default.
	rapidChecks int
	// floor is the builder's coverage floor, from the dotfile; own is the
	// project's, from .game, never below floor. coverAllow is the files
	// the project leaves out of the floor, with why.
	floor, own floorLine
	coverAllow map[string]string
}

// Author is the name the bounce refuses in the third person.
func (cfg Config) Author() string { return cfg.author }

// Words is the path of the sweep's word list, one word per line.
func (cfg Config) Words() string { return cfg.words }

// Named is the project's name as Load settled it.
func (cfg Config) Named() string { return cfg.named }

// Dotfile is the path of the config that was read.
func (cfg Config) Dotfile() string { return cfg.dotfile }

// Exclude is the path prefixes the sweep leaves alone.
func (cfg Config) Exclude() []string { return cfg.exclude }

// Omit is the files and directories left out of every release.
func (cfg Config) Omit() []string { return cfg.omit }

// SweepAllow is, per file, the kinds of sweep hit the repository means
// to carry.
func (cfg Config) SweepAllow() map[string][]string { return cfg.sweepAllow }

// License is the path of the one license every release carries.
func (cfg Config) License() string { return cfg.license }

// Lint is the lint as described; none described is no lint.
func (cfg Config) Lint() []Rule { return cfg.lint }

// Targets is the builds described beyond go's own, by name.
func (cfg Config) Targets() []Target { return cfg.targets }

// Runs is the things to run, described as targets are.
func (cfg Config) Runs() []Target { return cfg.runs }

// Needs is the settings the repository declares it needs.
func (cfg Config) Needs() []Need { return cfg.needs }

// Stack is a stack's members, name and ref, in order.
func (cfg Config) Stack() []Member { return cfg.stack }

// Member is one member of a stack as .game names it: the repository
// and the tag or branch to take.
type Member struct {
	name string
	ref  string
}

// Name is the member's name, which is the directory it is laid in.
func (member Member) Name() string { return member.name }

// Ref is the tag or branch to take.
func (member Member) Ref() string { return member.ref }

// Patches is the directories where the stack keeps members' patches
// beside patches/.
func (cfg Config) Patches() []string { return cfg.patches }

// Theirs is the members marked theirs.
func (cfg Config) Theirs() []string { return cfg.theirs }

// Set is your values, from the dotfile only.
func (cfg Config) Set() map[string]string { return cfg.set }

// RapidChecks is how many checks a property test runs under game
// check; zero keeps the library's own default.
func (cfg Config) RapidChecks() int { return cfg.rapidChecks }

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
	// road is the dirty remote, which takes every commit: game push.
	road string

	// dist is the clean remote, which only game release push sends to.
	dist string

	// set is the values this project is given, over the dotfile's.
	set map[string]string
}

// Road is the dirty remote, which takes every commit.
func (project Project) Road() string { return project.road }

// Dist is the clean remote, which only a release push sends to.
func (project Project) Dist() string { return project.dist }

// Need is a setting a repository declares it needs, and why. if you
// haven't set it, game tells you what to set before anything runs. the
// value lives in your dotfile, never in the repository.
//
//	needs CONTACT a name and email the data providers can reach
type Need struct {
	name string
	why  string
}

// Name is the setting needed.
func (need Need) Name() string { return need.name }

// Why is why the repository needs it.
func (need Need) Why() string { return need.why }

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
	name  string
	steps []Step
}

// Name is the target's name.
func (target Target) Name() string { return target.name }

// Steps is the target's steps, in order.
func (target Target) Steps() []Step { return target.steps }

// Step is one line of a target: where it runs, and what.
type Step struct {
	dir  string
	args []string
}

// Dir is where the step runs.
func (step Step) Dir() string { return step.dir }

// Args is what the step runs: the program and its arguments.
func (step Step) Args() []string { return step.args }

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
//	lint harden            privilege and unsafe file handling reported;
//	                       release and verify refuse a harden finding
//	lint words a b c       words banned from code and docs
//	lint allow FILE name   that one file is not held to that one rule
type Rule struct {
	name string
	args []string
}

// Ruled is one rule of lint by its name and arguments, as a lint line
// describes it.
func Ruled(name string, args ...string) Rule {
	return Rule{name: name, args: args}
}

// Name is the rule's name.
func (rule Rule) Name() string { return rule.name }

// Args is the rule's arguments.
func (rule Rule) Args() []string { return rule.args }

// the places a line's words are read at, defined by the lines that
// take them: the first word, the second and the third.
const (
	first  = 0 // defined by go: a list begins at nothing
	second = 1 // defined: one past the first
	third  = 2 // defined: one past the second
)

// numeric is the lint rules that take a number. the smaller is stricter.
var numeric = map[string]bool{
	"width": true, "rows": true, "paragraph": true, "commas": true,
}

// Strictest combines lint rules into the strictest set. numbers are
// the smallest from any member. word lists are merged. rules without
// arguments are on if any member has them. allowances are merged per
// file.
func Strictest(sets ...[]Rule) []Rule {
	var order []string
	held := map[string]Rule{}
	for _, set := range sets {
		for _, rule := range set {
			key := rule.name
			allowing := rule.name == "allow" &&
				count.Positive(len(rule.args))
			if allowing {
				key = "allow " + rule.args[first]
			}
			was, seen := held[key]
			if !seen {
				order = append(order, key)
				held[key] = rule
				continue
			}
			switch {
			case numeric[rule.name]:
				if less(rule.args, was.args) {
					held[key] = rule
				}
			case rule.name == "words":
				held[key] = Rule{
					name: rule.name,
					args: union(was.args, rule.args),
				}
			case rule.name == "allow":
				file := rule.args[first]
				lints := union(was.args[second:],
					rule.args[second:])
				held[key] = Rule{
					name: rule.name,
					args: append([]string{file}, lints...),
				}
			}
		}
	}
	out := make([]Rule, count.Nothing, len(order))
	for _, name := range order {
		out = append(out, held[name])
	}
	return out
}

// less checks if one rule's number is smaller than another's. it is false
// when either has no number.
func less(one, other []string) bool {
	if count.None(len(one)) || count.None(len(other)) {
		return false
	}
	x, err := strconv.Atoi(one[first])
	if err != nil {
		return false
	}
	y, err := strconv.Atoi(other[first])
	if err != nil {
		return false
	}
	return x < y
}

// allowWords is the fewest words a lint allow line has: allow, the
// file and one lint, as the line is defined.
const allowWords = 3

// allowance is one "lint allow FILE name..." line. it needs a file and at
// least one lint.
func allowance(at line, words []string) (*Rule, error) {
	if len(words) < allowWords {
		return nil, at.errorf(
			"lint allow wants a file and a lint, as in " +
				"\"lint allow OPERATION.md paragraph\"",
		)
	}
	for _, name := range words[third:] {
		if !slices.Contains(rules, name) {
			return nil, at.errorf(
				"unknown lint %q; the lints are %s",
				name,
				strings.Join(rules, ", "),
			)
		}
	}
	return &Rule{name: "allow", args: words[second:]}, nil
}

// union is every word from both lists, once, in first seen order.
func union(one, other []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]string{one, other} {
		for _, word := range list {
			if !seen[word] {
				seen[word] = true
				out = append(out, word)
			}
		}
	}
	return out
}

// sweepKinds are the kinds of sweep hit a .game may declare it carries.
var sweepKinds = []string{
	"paths", "sessions", "uuids", "emails", "author", "word",
}

// rules is the lint names game knows.
var rules = []string{
	"width",
	"rows",
	"paragraph",
	"comments",
	"print",
	"exclaim",
	"shout",
	"words",
	"chain",
	"commas",
	"opener",
	"history",
	"harden",
}

// defaults is a config for a repository with no files at all.
func defaults(home, repo string) Config {
	return Config{
		words: filepath.Join(home, ".config", "game", "sweep.words"),
	}
}

// Load reads the defaults, then the dotfile under whose, then the
// repository's .game. a missing file is skipped and a malformed one is an
// error.
//
// ~ expands against home, which is not always whose.
//
// lint lines in the repository replace the dotfile's. the dotfile's lint is
// used when the repository has none.
func Load(whose, home, repo string) (Config, error) {
	cfg := defaults(home, repo)
	dotfile := filepath.Join(whose, ".config", "game", "config")
	if err := cfg.fold(dotfile, home, true); err != nil {
		return cfg, err
	}
	mine := cfg.lint
	cfg.lint = nil
	if err := cfg.fold(
		filepath.Join(repo, ".game"), home, false,
	); err != nil {
		return cfg, err
	}
	if err := cfg.patchesBesideMembers(); err != nil {
		return cfg, err
	}
	if count.None(len(cfg.lint)) {
		cfg.lint = mine
	}
	cfg.dotfile = dotfile
	cfg.named, cfg.namedBy = named(cfg.name, repo)
	for name, value := range cfg.projects[cfg.named].set {
		if cfg.set == nil {
			cfg.set = map[string]string{}
		}
		cfg.set[name] = value
	}
	return cfg, nil
}

// Mine is the block for the project the repository says it is, and whether
// there is one. a project with no block is a local build.
func (cfg Config) Mine() (Project, bool) {
	project, held := cfg.projects[cfg.named]
	return project, held
}

// named returns the tree's name and where it came from. the name line in
// .game wins, then the last element of go.mod's module path, then the
// directory name.
func named(fromGame, repo string) (name, by string) {
	if fromGame != "" {
		return fromGame, "the name line in .game"
	}
	if mod := Module(repo); mod != "" {
		return last(mod), "go.mod's module " + mod
	}
	return filepath.Base(repo), "the directory's name"
}

// Module is the path a tree's go.mod declares, or empty without one.
func Module(repo string) string {
	body, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(
				strings.TrimPrefix(line, "module "),
			)
		}
	}
	return ""
}

// last is the project name from a module path. example/v2 is example.
func last(mod string) string {
	parts := strings.Split(strings.Trim(mod, "/"), "/")
	name := parts[count.Last(len(parts))]
	if digits, versioned := strings.CutPrefix(name, "v"); versioned &&
		len(parts) > count.One && digits != "" {
		if _, err := strconv.Atoi(digits); err == nil {
			last := count.Last(len(parts))
			name = parts[count.Last(last)]
		}
	}
	return name
}

// NoBlock is the refusal when a tree has no block. it shows the name
// looked for, where it came from, which file was read, and the blocks
// that file does have.
func (cfg Config) NoBlock() error {
	blocks := make([]string, count.Nothing, len(cfg.projects))
	for name := range cfg.projects {
		blocks = append(blocks, name)
	}
	sort.Strings(blocks)
	has := "it has no project blocks at all"
	if count.Positive(len(blocks)) {
		has = "its project blocks are " + strings.Join(blocks, ", ")
	}
	return fmt.Errorf(
		"no project block for %s in %s; the name is from %s, and %s. "+
			"open a \"project %s\" block there to give it a road "+
			"and a dist",
		cfg.named, cfg.dotfile, cfg.namedBy, has, cfg.named,
	)
}

// Remotes maps each project's name to its dist when clean is true, and to its
// road otherwise. projects with none are left out.
func (cfg Config) Remotes(clean bool) map[string]string {
	out := map[string]string{}
	for name, project := range cfg.projects {
		if url := project.road; !clean && url != "" {
			out[name] = url
		}
		if url := project.dist; clean && url != "" {
			out[name] = url
		}
	}
	return out
}

// fold reads a file into the config. words means this is your dotfile,
// which sets the word list and values. only a repository declares needs.
func (cfg *Config) fold(path, home string, words bool) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()
	return cfg.Parse(file, path, home, words)
}

// a line is one setting as it was read, with its file and number, its
// key, the project block it is in, whether this is the dotfile, the home
// for ~, and the tail after " :: " for the keys that take one.
type line struct {
	path  string
	at    int
	key   string
	open  string
	words bool
	home  string
	tail  string
}

// errorf is an error at this line, named file:line: key: first, as pzm
// names the lines it refuses.
func (at line) errorf(format string, args ...any) error {
	return fmt.Errorf(
		"%s:%d: %s: "+format,
		append([]any{at.path, at.at, at.key}, args...)...,
	)
}

// a setter reads one key's value into the config.
type setter func(cfg *Config, at line, key, val string) error

// Parse reads settings from a reader; path names it in errors. pzm reads
// the file's grammar; what the lines mean is settled here, and every line
// that cannot stand is named with the rest.
func (cfg *Config) Parse(
	from io.Reader,
	path, home string,
	words bool,
) error {
	raw, err := io.ReadAll(from)
	if err != nil {
		return err
	}
	var got file
	if err := schema.Read(path, raw, &got); err != nil {
		return err
	}
	read := reading{
		cfg: cfg, path: path, home: home, words: words,
	}
	read.fold(got)
	slices.SortStableFunc(read.refused, func(one, other refusal) int {
		return cmp.Compare(one.at, other.at)
	})
	var all []error
	for _, one := range read.refused {
		all = append(all, one.err)
	}
	return errors.Join(all...)
}

// reading is one file being folded into a config, and the lines it
// refused.
type reading struct {
	cfg     *Config
	path    string
	home    string
	words   bool
	refused []refusal
}

// refusal is why one line was refused, and where it was.
type refusal struct {
	at  int
	err error
}

// fold reads every line of the file into the config.
func (read *reading) fold(got file) {
	read.one("author", got.Author, "", setAuthor)
	read.one("license", got.License, "", setLicense)
	read.one("words", got.Words, "", setWords)
	read.one("name", got.Name, "", setName)
	read.each("road", got.Road, "", setRemote)
	read.each("dist", got.Dist, "", setRemote)
	read.each("public", got.Public, "", setPublic)
	read.each("exclude", got.Exclude, "", setExclude)
	read.each("omit", got.Omit, "", setOmit)
	read.each("stack", got.Stack, "", setStack)
	read.each("patches", got.Patches, "", setPatches)
	read.each("needs", got.Needs, "", setNeeds)
	read.each("set", got.Set, "", setValue)
	read.each("target", got.Target, "", setTarget)
	read.each("run", got.Run, "", setTarget)
	read.each("sweep", got.Sweep, "", setSweep)
	read.each("lint", got.Lint, "", setLint)
	read.one("rapid", got.Rapid, "", setRapid)
	read.each("cover", got.Cover, "", setCover)
	for _, block := range got.Project {
		read.project(block)
	}
}

// each reads every line of a key that may be said more than once.
func (read *reading) each(
	key string, lines []spoken, open string, set setter,
) {
	for _, line := range lines {
		read.one(key, line, open, set)
	}
}

// one reads a line, if the key was said, to its setter. the value has its
// leading ~ turned into the home.
func (read *reading) one(key string, said spoken, open string, set setter) {
	if said.At == 0 {
		return
	}
	at := line{
		path: read.path, at: said.At, key: key, open: open,
		words: read.words, home: read.home, tail: said.Tail,
	}
	value := expand(said.Value, read.home)
	if err := set(read.cfg, at, key, value); err != nil {
		read.refused = append(read.refused, refusal{said.At, err})
	}
}

// project reads a project's block: the dotfile's only. a repository says
// which block is its own with name.
func (read *reading) project(block project) {
	at := line{
		path: read.path, at: block.Name.At, key: "project",
		words: read.words, home: read.home,
	}
	name := expand(block.Name.Value, read.home)
	if err := read.cfg.openProject(at, name); err != nil {
		read.refused = append(read.refused, refusal{at.at, err})
		return
	}
	read.one("road", block.Road, name, setRemote)
	read.one("dist", block.Dist, name, setRemote)
	read.each("set", block.Set, name, setValue)
}

// openProject starts a project's block, in the dotfile only.
func (cfg *Config) openProject(at line, name string) error {
	if !at.words {
		return at.errorf(
			"a project block is the " +
				"builder's; a repository " +
				"says which one is its own " +
				"with name",
		)
	}
	if name == "" {
		return at.errorf("project wants \"project NAME\"")
	}
	if cfg.projects == nil {
		cfg.projects = map[string]Project{}
	}
	if _, held := cfg.projects[name]; !held {
		cfg.projects[name] = Project{}
	}
	return nil
}

// setAuthor is the author line.
func setAuthor(cfg *Config, _ line, _, val string) error {
	cfg.author = val
	return nil
}

// setLicense is the license line.
func setLicense(cfg *Config, _ line, _, val string) error {
	cfg.license = val
	return nil
}

// setWords is the word list, which only the dotfile sets.
func setWords(cfg *Config, at line, _, val string) error {
	if at.words {
		cfg.words = val
	}
	return nil
}

// setName is the repository's name, which the dotfile may not say.
func setName(cfg *Config, at line, _, val string) error {
	if at.words {
		return at.errorf(
			"name is a " +
				"repository's; the dotfile " +
				"names remotes with road " +
				"and dist",
		)
	}
	cfg.name = val
	return nil
}

// setRemote is a road or a dist, inside a project block in the dotfile.
func setRemote(cfg *Config, at line, key, val string) error {
	if !at.words {
		return at.errorf(
			"%s is the builder's, in "+
				"~/.config/game/config "+
				"under its project; a "+
				"repository never holds a "+
				"remote",
			key,
		)
	}
	if at.open == "" {
		return at.errorf(
			"%s sits inside a project "+
				"block: \"project NAME\", "+
				"then %s indented under it",
			key,
			key,
		)
	}
	if val == "" {
		return at.errorf("%s wants a url", key)
	}
	if first, _, cut := strings.Cut(val, " "); cut && first == at.open {
		return at.errorf(
			"the block already names "+
				"%s; %s takes the url alone",
			at.open,
			key,
		)
	}
	held := cfg.projects[at.open]
	if key == "road" {
		held.road = val
	} else {
		held.dist = val
	}
	cfg.projects[at.open] = held
	return nil
}

// setPublic is the old word for dist, which says so.
func setPublic(_ *Config, at line, _, _ string) error {
	return at.errorf(
		"\"public\" is now \"dist\": " +
			"the clean remote only game release " +
			"pushes to; the dirty one is " +
			"\"road\"",
	)
}

// setExclude is a path prefix the sweep leaves alone.
func setExclude(cfg *Config, _ line, _, val string) error {
	cfg.exclude = append(cfg.exclude, val)
	return nil
}

// setOmit is a file or directory kept on the road and out of every
// release.
func setOmit(cfg *Config, _ line, _, val string) error {
	cfg.omit = append(cfg.omit, val)
	return nil
}

// setStack is one member of the stack, name and ref, in .game only, and
// theirs after the ref when the member is someone else's code.
func setStack(cfg *Config, at line, _, val string) error {
	if at.words {
		return at.errorf(
			"a stack is a " +
				"repository's; the dotfile " +
				"says where each member " +
				"lives",
		)
	}
	words := strings.Fields(val)
	if len(words) == nameRefAndTheirs && words[third] != "theirs" {
		return at.errorf("stack wants \"stack NAME REF\", or " +
			"\"stack NAME REF theirs\" for someone else's code")
	}
	name, ref := words[first], words[second]
	// a member's name is a directory under bin/stack that game removes
	// and clones again, so it may not climb out or be hidden; and git
	// reads it as a pathspec, so it holds no glob to reach another's.
	if name == "" || strings.ContainsAny(name, `/\*?[`) ||
		strings.HasPrefix(name, ".") {
		return at.errorf("a stack member's name is one plain "+
			"directory, not %q", name)
	}
	cfg.stack = append(cfg.stack, Member{name: name, ref: ref})
	if len(words) == nameRefAndTheirs {
		cfg.theirs = append(cfg.theirs, name)
	}
	return nil
}

// nameRefAndTheirs is the words of a stack line that marks its member
// someone else's, as the line is defined: NAME REF theirs.
const nameRefAndTheirs = 3

// IsTheirs is whether the stack member of that name is someone else's
// code, marked theirs.
func (cfg Config) IsTheirs(name string) bool {
	return slices.Contains(cfg.theirs, name)
}

// setPatches is a directory in the repository where the stack keeps
// members' patches beside patches/, each in NAME under it. stacks that
// can't put patches in a member's own repository keep them there, as
// carrie keeps charm's in charmbracelet/patches. in .game only.
func setPatches(cfg *Config, at line, _, val string) error {
	if at.words {
		return at.errorf("patches are a repository's; " +
			"say where in its .game")
	}
	words := strings.Fields(val)
	if len(words) != count.One || !filepath.IsLocal(words[first]) {
		return at.errorf("patches wants one directory inside "+
			"the repository, as \"patches charm/patches\"; "+
			"not %q", val)
	}
	dir := filepath.Clean(words[first])
	top, _, _ := strings.Cut(filepath.ToSlash(dir), "/")
	switch {
	case dir == ".":
		return at.errorf("patches names a directory inside the " +
			"repository, not the repository itself")
	case strings.EqualFold(dir, "patches"):
		return at.errorf("patches/ is always read; name only " +
			"the directories beside it")
	case strings.EqualFold(top, "bin"):
		return at.errorf("patches %s is in bin, where game puts what "+
			"it builds and clones", dir)
	case slices.ContainsFunc(cfg.patches, sameFold(dir).is):
		return at.errorf("patches %s is named twice", dir)
	}
	cfg.patches = append(cfg.patches, dir)
	return nil
}

// patchesBesideMembers refuses a patches directory inside a stack
// member's. game lays members under their names, so a place there would
// read the member's own files as the stack's patches. stack lines can
// come after a patches line, so this runs once .game is read.
func (cfg Config) patchesBesideMembers() error {
	for _, dir := range cfg.patches {
		top, _, _ := strings.Cut(filepath.ToSlash(dir), "/")
		for _, member := range cfg.stack {
			if !strings.EqualFold(top, member.name) {
				continue
			}
			return fmt.Errorf(".game: patches %s is inside "+
				"stack member %s, whose own files would be "+
				"read as the stack's patches", dir, member.name)
		}
	}
	return nil
}

// sameFold is whether a name is dir, case folded.
type sameFold string

// is says whether named is the directory, case folded.
func (dir sameFold) is(named string) bool {
	return strings.EqualFold(named, string(dir))
}

// setNeeds is a setting the repository declares it needs, and why.
func setNeeds(cfg *Config, at line, _, val string) error {
	if at.words {
		return at.errorf(
			"needs is a " +
				"repository's declaration; " +
				"the dotfile gives values " +
				"with set",
		)
	}
	name, why, _ := strings.Cut(val, " ")
	cfg.needs = append(cfg.needs, Need{name: name, why: why})
	return nil
}

// setValue sets a value from the dotfile for a project or globally.
func setValue(cfg *Config, at line, _, val string) error {
	if !at.words {
		return at.errorf(
			"a value never lives " +
				"in a repository; set it in " +
				"~/.config/game/config, and " +
				"declare it here with needs",
		)
	}
	name, value, _ := strings.Cut(val, " ")
	if at.open != "" {
		held := cfg.projects[at.open]
		if held.set == nil {
			held.set = map[string]string{}
		}
		held.set[name] = value
		cfg.projects[at.open] = held
		return nil
	}
	if cfg.set == nil {
		cfg.set = map[string]string{}
	}
	cfg.set[name] = value
	return nil
}

// setTarget is one step of a target or a run: the name, the directory
// and, after the double colon, the command. steps of one name gather in
// order.
func setTarget(cfg *Config, at line, key, val string) error {
	// pzm has seen the name, the directory and the command's tail
	name, dir, _ := strings.Cut(val, " ")
	dir = expand(dir, at.home)
	step := strings.Fields(at.tail)
	for i := range step {
		step[i] = expand(step[i], at.home)
	}
	list := &cfg.targets
	if key == "run" {
		list = &cfg.runs
	}
	for i := range *list {
		if (*list)[i].name == name {
			(*list)[i].steps = append(
				(*list)[i].steps,
				Step{dir: dir, args: step},
			)
			return nil
		}
	}
	*list = append(*list, Target{
		name:  name,
		steps: []Step{{dir: dir, args: step}},
	})
	return nil
}

// setSweep is "sweep allow FILE KIND...", the kinds of hit one file is
// meant to carry.
func setSweep(cfg *Config, at line, _, val string) error {
	words := strings.Fields(val)
	if words[first] != "allow" {
		return at.errorf(
			"sweep wants "+
				"allow FILE KIND...; the "+
				"kinds are %s",
			strings.Join(sweepKinds, ", "),
		)
	}
	file, kinds := words[second], words[third:]
	for _, kind := range kinds {
		if !slices.Contains(sweepKinds, kind) {
			return at.errorf(
				"unknown sweep kind "+
					"%q; the kinds are %s",
				kind,
				strings.Join(sweepKinds, ", "),
			)
		}
	}
	if cfg.sweepAllow == nil {
		cfg.sweepAllow = map[string][]string{}
	}
	cfg.sweepAllow[file] = append(cfg.sweepAllow[file], kinds...)
	return nil
}

// setLint is one lint rule, or an allowance.
func setLint(cfg *Config, at line, _, val string) error {
	words := strings.Fields(val)
	if count.None(len(words)) {
		return at.errorf(
			"lint wants a rule; "+
				"the lints are %s",
			strings.Join(rules, ", "),
		)
	}
	name, args := words[first], words[second:]
	if name == "allow" {
		rule, err := allowance(at, words)
		if err != nil {
			return err
		}
		cfg.lint = append(cfg.lint, *rule)
		return nil
	}
	if !slices.Contains(rules, name) {
		return at.errorf(
			"unknown lint %q; "+
				"the lints are %s",
			name,
			strings.Join(rules, ", "),
		)
	}
	cfg.lint = append(cfg.lint, Rule{name: name, args: args})
	return nil
}

// setRapid is "rapid checks N", how many checks a property test runs.
func setRapid(cfg *Config, at line, _, val string) error {
	key, said, _ := strings.Cut(val, " ")
	var checks int
	if key == "checks" {
		checks, _ = strconv.Atoi(said)
	}
	if !count.Positive(checks) {
		return at.errorf(
			"rapid takes checks N, how " +
				"many checks a property test " +
				"runs: rapid checks 1000",
		)
	}
	cfg.rapidChecks = checks
	return nil
}

// unquoted is a value with the one pair of quotes around it taken off,
// double or single, when it has them.
func unquoted(val string) string {
	for _, quote := range []string{`"`, `'`} {
		inner, quoted := strings.CutPrefix(val, quote)
		inner, closed := strings.CutSuffix(inner, quote)
		if quoted && closed && len(val) > len(quote) {
			return inner
		}
	}
	return val
}

// expand turns a leading ~ into the home directory.
func expand(val, home string) string {
	if val == "~" {
		return home
	}
	if under, ok := strings.CutPrefix(val, "~/"); ok {
		return filepath.Join(home, under)
	}
	return val
}

// Missing lists every need not set, with reason. build can refuse early.
func (cfg Config) Missing() []Need {
	var out []Need
	for _, need := range cfg.needs {
		if _, ok := cfg.set[need.name]; !ok {
			out = append(out, need)
		}
	}
	return out
}

// Env is the declared settings as NAME=value. only what the repo declared.
func (cfg Config) Env() []string {
	var out []string
	for _, need := range cfg.needs {
		if value, ok := cfg.set[need.name]; ok {
			out = append(out, need.name+"="+value)
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
