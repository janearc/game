// Stack assembles many projects into one release. the release is one flat
// commit holding every member at a ref, each under its own name.
//
// it is a distribution, not a place to work. each member keeps its own
// repository, tags and module path. a go.work beside the members lets the
// stack build from one clone with no network.
package release

import (
	"fmt"
	"go/version"
	"slices"
	"sort"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// Member is one project in a stack. Name is the directory it lands in.
// Ref is what to take. URL is where to take it from. Theirs is whether
// it is someone else's code, whose addresses, urls, uuids and paths are
// its own and not held against the stack.
type Member struct {
	name   string
	ref    string
	url    string
	theirs bool
}

// Taken is a member of a stack: its name, the ref to take, the url to
// take it from, and whether it is someone else's code.
func Taken(name, ref, url string, theirs bool) Member {
	return Member{name: name, ref: ref, url: url, theirs: theirs}
}

// Stacked says what an assembly produced, and what it refused.
type Stacked struct {
	made Result

	// members are the names in the order they were laid out.
	members []string

	// modules are the directories of the go modules the go.work names, from
	// the root of the stack. Empty with no go module.
	modules []string

	// work is the go.work written, or empty if no member is a go module.
	work string

	// games is each member's own .game, by member name.
	games map[string]string

	// patched is how many of the stack's own patches were laid on each
	// member, by member name; a member with none is not in it.
	patched map[string]int
}

// Commit is the stack commit prepared, or the one dist already had.
func (made Stacked) Commit() string { return made.made.commit }

// Skipped is whether dist's main already had this tree.
func (made Stacked) Skipped() bool { return made.made.skipped }

// Members are the names in the order they were laid out.
func (made Stacked) Members() []string { return made.members }

// Modules are the directories of the go modules the go.work names, from
// the root of the stack. Empty with no go module.
func (made Stacked) Modules() []string { return made.modules }

// Work is the go.work written, or empty if no member is a go module.
func (made Stacked) Work() string { return made.work }

// Games is each member's own .game, by member name.
func (made Stacked) Games() map[string]string { return made.games }

// Patched is how many of the stack's own patches were laid on the
// member; none for one with none.
func (made Stacked) Patched(member string) int { return made.patched[member] }

// Stack builds a stack from members. it fetches each member, lays them
// beside this repo, writes a go.work, and keeps the commit at ref(tag).
// any error stops it before the ref is written.
func Stack(here repo.Repo, asked Options, members []Member) (*Stacked, error) {
	var out Stacked
	if err := stackable(asked, members); err != nil {
		return nil, err
	}
	if err := tidy(here); err != nil {
		return nil, err
	}
	scratch, err := here.Scratch("HEAD", asked.omit)
	if err != nil {
		return nil, err
	}
	defer scratch.Close()
	env := scratch.Env()
	lay := newLayout(asked)
	for _, member := range members {
		if err := lay.add(here, env, member); err != nil {
			return nil, err
		}
	}
	out.members, out.games = lay.members, lay.games
	out.modules = lay.work
	out.patched = lay.patched
	if err := written(here, env, lay, &out); err != nil {
		return nil, err
	}
	if err := commit(here, asked, &out); err != nil {
		return nil, err
	}
	if err := refuse(here, asked, lay, &out); err != nil {
		return nil, err
	}
	if err := keep(here, asked, out); err != nil {
		return nil, err
	}
	stacked := NewStacked(out.made.commit, out.made.skipped, out.members,
		out.modules, out.work, out.games, out.patched)
	return &stacked, nil
}

// NewStacked is a stack whose commit is commit, which is the one dist
// already had when skipped.
//
// members are the names in the order they were laid out, modules the
// directories of their go modules and work the go.work written. games is
// each member's own .game, and patched how many of the stack's own
// patches were laid on each member.
func NewStacked(commit string, skipped bool, members, modules []string,
	work string, games map[string]string, patched map[string]int) Stacked {
	return Stacked{
		made:    Result{commit: commit, skipped: skipped},
		members: members, modules: modules, work: work, games: games,
		patched: patched,
	}
}

// written puts the go.work into the index when the members make one. It
// then writes the index as the stack's tree.
func written(
	here repo.Repo, env []string, lay *layout, out *Stacked,
) error {
	if count.Positive(len(lay.twice)) {
		return fmt.Errorf("the go.work would use a module twice, "+
			"which go refuses: %s; mark the member theirs if they "+
			"are its own examples", strings.Join(lay.twice, "; "))
	}
	if count.Positive(len(lay.work)) {
		swapped := swaps(lay.lives, lay.wanted)
		out.work = workspace(lay.work, lay.version, swapped)
		if err := addWork(here, env, out.work); err != nil {
			return err
		}
	}
	tree, err := here.GitEnv(env, "write-tree")
	if err != nil {
		return fmt.Errorf("the stack's tree: %w", err)
	}
	out.made.tree = tree
	return nil
}

// keep asks the qualifier if there is one. Only then does it write the
// ref the stack waits at.
func keep(here repo.Repo, asked Options, out Stacked) error {
	if asked.qualifier != nil {
		if err := asked.qualifier(out); err != nil {
			return err
		}
	}
	_, err := here.Git("update-ref", ref(asked.tag), out.made.commit)
	return err
}

// stackable checks for a tag, never-words and members.
func stackable(asked Options, members []Member) error {
	if asked.tag == "" {
		return fmt.Errorf(
			"a stack release needs a tag: " +
				"game release stack v0.3.0",
		)
	}
	if asked.never.Empty() {
		return errNoNeverWords
	}
	if count.None(len(members)) {
		return fmt.Errorf(
			"no members; a stack names them in .game as " +
				"\"stack NAME REF\"",
		)
	}
	return nil
}

// tidy refuses a root with a change not committed, as a release does.
func tidy(here repo.Repo) error {
	clean, err := here.Clean()
	if err != nil {
		return err
	}
	if !clean {
		return fmt.Errorf(
			"the tree is dirty; commit first, to the road",
		)
	}
	return nil
}

// layout is what laying the members into the index produced.
type layout struct {
	members []string // names, in the order laid
	work    []string // the module directories, for go.work
	// lives maps a module path to its directory here. wanted maps a
	// module path to the versions some member asks for.
	lives  map[string]string
	wanted map[string]map[string]bool
	// version is the highest go directive any member has
	version string
	// exclude is the sweep's prefixes, the caller's and then each
	// member's under its name. allow is the allowances, likewise.
	exclude []string
	allow   map[string][]string
	games   map[string]string // each member's own .game
	patched map[string]int    // the stack's own patches laid, by member
	// patches is the directories .game names where members' patches are
	// kept beside patches/
	patches []string
	twice   []string // module paths two directories give
}

// newLayout starts a layout with the caller's own exclusions.
func newLayout(asked Options) *layout {
	return &layout{
		lives:   map[string]string{},
		wanted:  map[string]map[string]bool{},
		exclude: append([]string(nil), asked.exclude...),
		patches: asked.patches,
	}
}

// add fetches a member at its ref and reads its tree into the index under
// its name. it takes what the member's .game and go.mod files say.
func (lay *layout) add(here repo.Repo, env []string, member Member) error {
	if err := lay.room(here, env, member); err != nil {
		return err
	}
	at := "refs/game/stack/" + member.name
	if _, err := here.Git(
		"fetch", "-q", "--no-tags", member.url, "+"+member.ref+":"+at,
	); err != nil {
		return fmt.Errorf(
			"%s at %s from %s: %v",
			member.name, member.ref, member.url, err,
		)
	}
	tree, err := here.Tree(at)
	if err != nil {
		return err
	}
	if _, err := here.GitEnv(
		env, "read-tree", "--prefix="+member.name+"/", tree,
	); err != nil {
		return fmt.Errorf("%s: %v", member.name, err)
	}
	if err := lay.settle(here, env, member); err != nil {
		return err
	}
	left, err := lay.leave(here, env, member.name)
	if err != nil {
		return err
	}
	err = lay.takeModules(here, left, member.name, member.theirs)
	if err != nil {
		return err
	}
	lay.members = append(lay.members, member.name)
	return nil
}

// leave is the member's tree as the stack leaves it. The .game and the
// modules are read from it, not from the tree fetched. It reads the .game
// from what the patches left. It applies the omit lines the .game says.
func (lay *layout) leave(
	here repo.Repo, env []string, name string,
) (string, error) {
	patched, err := kept(here, env, name)
	if err != nil {
		return "", err
	}
	game, err := here.Show(patched, ".game")
	if err != nil {
		return patched, nil
	}
	if err := lay.takeGame(here, env, name, game); err != nil {
		return "", err
	}
	return kept(here, env, name)
}

// kept is the member's tree as the stack leaves it: what the index holds
// under its name after the stack's patches and omissions.
func kept(here repo.Repo, env []string, name string) (string, error) {
	tree, err := here.GitEnv(env, "write-tree", "--prefix="+name+"/")
	if err != nil {
		return "", fmt.Errorf("%s: %v", name, err)
	}
	return tree, nil
}

// room refuses a member with no remote to take it from, and one whose
// name the stack already holds: git would merge it into a directory of
// that name, the root's or an earlier member's.
func (lay *layout) room(here repo.Repo, env []string, member Member) error {
	if member.url == "" {
		return fmt.Errorf(
			"%s has no remote: open a \"project %s\" block in the "+
				"config and give it a road or a dist",
			member.name,
			member.name,
		)
	}
	held, err := here.GitEnv(env, "ls-files", "--", member.name)
	if err != nil {
		return fmt.Errorf("the stack's index: %w", err)
	}
	if held != "" {
		return fmt.Errorf(
			"%s: the stack already holds %s, so the member would "+
				"be mixed into it; rename the member or that "+
				"directory",
			member.name,
			member.name,
		)
	}
	return nil
}

// takeGame reads a member's .game once, as game reads it. Game files that
// do not parse refuse the stack. The excluded paths and allowances are kept
// under the member's name. The omit lines are applied to the index.
func (lay *layout) takeGame(
	here repo.Repo, env []string, name, game string,
) error {
	var member config.Config
	if err := member.Parse(strings.NewReader(game), ".game", "",
		false); err != nil {
		return fmt.Errorf("%s: its .game: %v", name, err)
	}
	for _, path := range member.Exclude() {
		lay.exclude = append(lay.exclude, name+"/"+path)
	}
	for file, kinds := range member.SweepAllow() {
		if lay.allow == nil {
			lay.allow = map[string][]string{}
		}
		at := name + "/" + file
		lay.allow[at] = append(lay.allow[at], kinds...)
	}
	if lay.games == nil {
		lay.games = map[string]string{}
	}
	lay.games[name] = game
	return omit(here, env, name, member.Omit())
}

// takeModules reads all go.mod files in a member. a subdirectory
// module must be in the workspace or the build uses the proxy.
//
// a member marked theirs gives only its own top module: its examples and
// tutorials are its own, and often share a module path. a path two
// directories give is kept in twice, for the stack to refuse.
func (lay *layout) takeModules(
	here repo.Repo, tree, name string, theirs bool,
) error {
	places, err := modulesIn(here, tree)
	if err != nil {
		return fmt.Errorf("%s: its modules: %v", name, err)
	}
	for _, at := range places {
		if theirs && at != "" {
			continue
		}
		mod, err := here.Show(tree, at+"go.mod")
		if err != nil {
			continue
		}
		where := strings.TrimSuffix(at, "/")
		dir := strings.TrimSuffix(name+"/"+where, "/")
		lay.work = append(lay.work, dir)
		if path := modulePath(mod); path != "" {
			if held, ok := lay.lives[path]; ok {
				lay.twice = append(lay.twice,
					path+" in "+held+" and "+dir)
			}
			lay.lives[path] = dir
		}
		for path, version := range required(mod) {
			if lay.wanted[path] == nil {
				lay.wanted[path] = map[string]bool{}
			}
			lay.wanted[path][version] = true
		}
		if version := directive(mod); higher(version, lay.version) {
			lay.version = version
		}
	}
	return nil
}

// higher is whether go version one is above go version other. It compares
// as go does. 1.26 is above 1.9. 1.25rc1 is above 1.25.
func higher(one, other string) bool {
	return count.Positive(version.Compare("go"+one, "go"+other))
}

// addWork puts the go.work into the index as a blob, beside the members.
func addWork(here repo.Repo, env []string, work string) error {
	blob, err := here.GitIn(work, "hash-object", "-w", "--stdin")
	if err != nil {
		return err
	}
	_, err = here.GitEnv(
		env, "update-index", "--add",
		"--cacheinfo", "100644,"+blob+",go.work",
	)
	return err
}

// refuse sweeps the commit with every member's allowances and the
// caller's, and looks for the setting values. any hit refuses the stack.
func refuse(here repo.Repo, asked Options, lay *layout, out *Stacked) error {
	history, err := sweep.History(here, out.made.commit)
	if err != nil {
		return fmt.Errorf("the history of %s: %w", out.made.commit, err)
	}
	allow := lay.allow
	for at, kinds := range asked.allow {
		if allow == nil {
			allow = map[string][]string{}
		}
		allow[at] = append(allow[at], kinds...)
	}
	hits, err := sweep.Tree(
		here, out.made.commit, asked.never, asked.author, allow,
		lay.exclude...,
	)
	if err != nil {
		return err
	}
	found, err := values(here, out.made.commit, asked.values)
	if err != nil {
		return err
	}
	out.made.hits = append(append(history, hits...), found...)
	if count.Positive(len(out.made.hits)) {
		return fmt.Errorf("refused: %s", describe(out.made.hits))
	}
	return nil
}

// commit writes the assembled tree as one commit parented on dist's main. if
// the tree is the same, it reuses dist's commit.
func commit(here repo.Repo, asked Options, out *Stacked) error {
	current, err := here.RemoteMain(asked.dist)
	if err != nil {
		return err
	}
	out.made.parent = current
	if current != "" {
		if err := fetchMain(here, asked.dist); err != nil {
			return err
		}
	}
	made, skipped, err := flat(here, asked, out.made.tree, current)
	if err != nil {
		return fmt.Errorf("the stack's commit: %w", err)
	}
	out.made.commit, out.made.skipped = made, skipped
	return nil
}

// fetchMain fetches dist's main before the assembled commit is read.
func fetchMain(here repo.Repo, dist string) error {
	_, err := here.Git("fetch", "-q", dist, "refs/heads/main")
	return err
}

// workspace is the go.work for the member directories. version is the
// highest go version any member asks for, 1.25 when none does. each
// swap adds a replace line.
func workspace(members []string, version string, swap []replace) string {
	var out strings.Builder
	out.WriteString("// the stack, as one workspace: every member built\n")
	out.WriteString("// from this clone rather than from the proxy.\n")
	out.WriteString("//\n")
	out.WriteString(
		"// written by game; each member keeps its own go.mod.\n",
	)
	if version == "" {
		version = "1.25"
	}
	out.WriteString("go " + version + "\n\nuse (\n")
	sorted := append([]string(nil), members...)
	sort.Strings(sorted)
	for _, member := range sorted {
		out.WriteString("\t./" + member + "\n")
	}
	out.WriteString(")\n")
	if count.Positive(len(swap)) {
		for _, line := range []string{
			"",
			"// a member that asks for a sibling by version gets",
			"// the copy in this clone. without these the reader",
			"// goes to the proxy for a tag that may not be",
			"// published, and the stack then builds only on a",
			"// machine whose module cache already has it.",
		} {
			out.WriteString(line + "\n")
		}
		for _, one := range swap {
			out.WriteString(
				"replace " + one.path + " " + one.version +
					" => ./" + one.dir + "\n",
			)
		}
	}
	return out.String()
}

// replace points one version of a module at the directory it lives in here.
type replace struct {
	path    string
	version string
	dir     string
}

// ordered compares two replaces by path, then version, as
// slices.SortFunc asks.
func ordered(one, other replace) int {
	const same = 0 // defined by strings.Compare: equal strings
	if by := strings.Compare(one.path, other.path); by != same {
		return by
	}
	return strings.Compare(one.version, other.version)
}

// swaps makes a replace for each version any member asks for of a module that
// is in the workspace, sorted by path and version.
func swaps(
	lives map[string]string, wanted map[string]map[string]bool,
) []replace {
	var out []replace
	for path, dir := range lives {
		for version := range wanted[path] {
			out = append(out, replace{
				path:    path,
				version: version,
				dir:     dir,
			})
		}
	}
	slices.SortFunc(out, ordered)
	return out
}

// modulePath returns the path from a go.mod file.
func modulePath(mod string) string {
	for _, line := range strings.Split(mod, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module ") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "module "))
	}
	return ""
}

// required reads go.mod's direct and indirect dependencies.
func required(mod string) map[string]string {
	out := map[string]string{}
	block := false
	for _, line := range strings.Split(mod, "\n") {
		line, _, _ = strings.Cut(line, "//")
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "require ("):
			block = true
			continue
		case block && line == ")":
			block = false
			continue
		case strings.HasPrefix(line, "require "):
			line = strings.TrimPrefix(line, "require ")
		case !block:
			continue
		}
		path, version, cut := strings.Cut(line, " ")
		version = strings.TrimSpace(version)
		if !cut || !strings.HasPrefix(version, "v") ||
			strings.ContainsAny(version, " \t") {
			continue
		}
		out[path] = version
	}
	return out
}

// modulesIn lists the directories in a tree that hold a go.mod, as prefixes
// ending in a slash. the root is the empty string.
func modulesIn(here repo.Repo, tree string) ([]string, error) {
	out, err := here.Git("ls-tree", "-r", "--name-only", tree)
	if err != nil {
		return nil, err
	}
	var at []string
	for _, path := range strings.Split(out, "\n") {
		path = strings.TrimSpace(path)
		if path != "go.mod" && !strings.HasSuffix(path, "/go.mod") {
			continue
		}
		at = append(at, strings.TrimSuffix(path, "go.mod"))
	}
	return at, nil
}

// directive returns a module's go version line or empty.
func directive(mod string) string {
	for _, line := range strings.Split(mod, "\n") {
		if strings.HasPrefix(line, "go ") {
			said := strings.TrimPrefix(line, "go ")
			return strings.TrimSpace(said)
		}
	}
	return ""
}
