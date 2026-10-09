// Stack assembles many projects into one release: a repository holding every
// member at a ref, each under its own name, in a single flat commit.
//
// it is a distribution, not a place to work. each member keeps its own
// repository, tags and module path. a go.work beside the members lets the
// stack build from one clone with no network.
package release

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// Member is one project in a stack. Name is the directory it lands in, Ref is
// what to take and URL is the remote to take it from.
type Member struct {
	Name string
	Ref  string
	URL  string
}

// Stacked says what an assembly produced, and what it refused.
type Stacked struct {
	Result

	// Members are the names in the order they were laid out.
	Members []string

	// Work is the go.work that was written, or empty if no member is a go
	// module.
	Work string

	// Games is each member's own .game by member name, so the caller can
	// hold the assembly to every member's rules.
	Games map[string]string
}

// Stack builds a stack from members. it fetches each member, lays them
// beside this repo, writes a go.work, and keeps the commit at Ref(tag).
// any error stops the whole thing: a stack is published whole or not at all.
func Stack(r repo.Repo, o Options, members []Member) (Stacked, error) {
	var out Stacked
	if o.Tag == "" {
		return out, fmt.Errorf(
			"a stack release needs a tag: " +
				"game release stack v0.3.0",
		)
	}
	if o.Never.Empty() {
		return out, errNoNeverWords
	}
	if len(members) == 0 {
		return out, fmt.Errorf(
			"no members; a stack names them in .game as " +
				"\"stack NAME REF\"",
		)
	}
	index, err := os.CreateTemp("", "game-stack-index")
	if err != nil {
		return out, err
	}
	defer os.Remove(index.Name())
	index.Close()
	os.Remove(index.Name())
	env := []string{"GIT_INDEX_FILE=" + index.Name()}
	// the stack's own tree is at the root. members go beside it.
	own, err := r.Tree("HEAD")
	if err != nil {
		return out, err
	}
	if _, err := r.GitEnv(env, "read-tree", own); err != nil {
		return out, err
	}
	var work []string
	// where each module path lives in this clone, and every version any
	// member asks for it at, so the workspace can point one at the other.
	lives := map[string]string{}
	wanted := map[string]map[string]bool{}
	version := ""
	exclude := append([]string(nil), o.Exclude...)
	// allow collects the sweep allowances per path, from each member's
	// .game moved under its name.
	var allow map[string][]string
	for _, m := range members {
		if m.URL == "" {
			return out, fmt.Errorf(
				"%s has no remote: open a \"project %s\" "+
					"block in the config and give it a "+
					"road or a dist",
				m.Name,
				m.Name,
			)
		}
		at := "refs/game/stack/" + m.Name
		if _, err := r.Git(
			"fetch", "-q", "--no-tags", m.URL,
			"+"+m.Ref+":"+at,
		); err != nil {
			return out, fmt.Errorf(
				"%s at %s from %s: %v",
				m.Name, m.Ref, m.URL, err,
			)
		}
		tree, err := r.Tree(at)
		if err != nil {
			return out, err
		}
		if _, err := r.GitEnv(
			env, "read-tree", "--prefix="+m.Name+"/", tree,
		); err != nil {
			return out, fmt.Errorf("%s: %v", m.Name, err)
		}
		// a member's sweep skips paths it already marked.
		if game, err := r.Show(tree, ".game"); err == nil {
			for _, path := range excluded(game) {
				exclude = append(exclude, m.Name+"/"+path)
			}
			for at, kinds := range allowedIn(m.Name, game) {
				if allow == nil {
					allow = map[string][]string{}
				}
				allow[at] = append(allow[at], kinds...)
			}
			if out.Games == nil {
				out.Games = map[string]string{}
			}
			out.Games[m.Name] = game
		}
		// every module in the member is checked, not just the root. a
		// subdirectory module needs to be found to avoid proxy use.
		for _, at := range modules(r, tree) {
			mod, err := r.Show(tree, at+"go.mod")
			if err != nil {
				continue
			}
			where := strings.TrimSuffix(at, "/")
			dir := strings.TrimSuffix(m.Name+"/"+where, "/")
			work = append(work, dir)
			if path := modulePath(mod); path != "" {
				lives[path] = dir
			}
			for path, v := range required(mod) {
				if wanted[path] == nil {
					wanted[path] = map[string]bool{}
				}
				wanted[path][v] = true
			}
			if v := directive(mod); v > version {
				version = v
			}
		}
		out.Members = append(out.Members, m.Name)
	}
	if len(work) > 0 {
		out.Work = Work(work, version, swaps(lives, wanted))
		blob, err := r.GitIn(out.Work, "hash-object", "-w", "--stdin")
		if err != nil {
			return out, err
		}
		if _, err := r.GitEnv(
			env, "update-index", "--add",
			"--cacheinfo", "100644,"+blob+",go.work",
		); err != nil {
			return out, err
		}
	}
	tree, err := r.GitEnv(env, "write-tree")
	if err != nil {
		return out, err
	}
	out.Tree = tree
	if err := commit(r, o, &out); err != nil {
		return out, err
	}
	for at, kinds := range o.Allow {
		if allow == nil {
			allow = map[string][]string{}
		}
		allow[at] = append(allow[at], kinds...)
	}
	hits, err := sweep.Tree(
		r, out.Commit, o.Never, o.Author, allow, exclude...)
	if err != nil {
		return out, err
	}
	vh, err := values(r, out.Commit, o.Values)
	if err != nil {
		return out, err
	}
	out.Hits = append(hits, vh...)
	if len(out.Hits) > 0 {
		return out, fmt.Errorf("refused: %s", Describe(out.Hits))
	}
	_, err = r.Git("update-ref", Ref(o.Tag), out.Commit)
	return out, err
}

// commit writes the assembled tree as one commit parented on dist's main. if
// the tree is the same, it reuses dist's commit.
func commit(r repo.Repo, o Options, out *Stacked) error {
	current, err := r.RemoteMain(o.Dist)
	if err != nil {
		return err
	}
	out.Parent = current
	args := []string{"commit-tree", out.Tree}
	if current != "" {
		if _, err := r.Git(
			"fetch", "-q", o.Dist, "refs/heads/main",
		); err != nil {
			return err
		}
		if t, _ := r.Tree(current); t == out.Tree {
			out.Skipped, out.Commit = true, current
			return nil
		}
		args = append(args, "-p", current)
	}
	commit, err := r.GitIn(Message(o.Name, o.Tag)+"\n", args...)
	if err != nil {
		return err
	}
	out.Commit = commit
	return nil
}

// Work is the go.work for the member directories. version is the highest go
// version any member asks for, 1.25 when none does. each swap adds a replace
// line.
func Work(members []string, version string, swap []Swap) string {
	var b strings.Builder
	b.WriteString("// the stack, as one workspace: every member built\n")
	b.WriteString("// from this clone rather than from the proxy.\n")
	b.WriteString("//\n")
	b.WriteString("// written by game; each member keeps its own go.mod.\n")
	if version == "" {
		version = "1.25"
	}
	b.WriteString("go " + version + "\n\nuse (\n")
	sorted := append([]string(nil), members...)
	sort.Strings(sorted)
	for _, m := range sorted {
		b.WriteString("\t./" + strings.TrimSuffix(m, "/") + "\n")
	}
	b.WriteString(")\n")
	if len(swap) > 0 {
		for _, line := range []string{
			"",
			"// a member that asks for a sibling by version gets",
			"// the copy in this clone. without these the reader",
			"// goes to the proxy for a tag that may not be",
			"// published, and the stack then builds only on a",
			"// machine whose module cache already has it.",
		} {
			b.WriteString(line + "\n")
		}
		for _, sw := range swap {
			b.WriteString(
				"replace " + sw.Path + " " + sw.Version +
					" => ./" + sw.Dir + "\n",
			)
		}
	}
	return b.String()
}

// Swap is one replace line pointing a module at the directory it lives in
// here, for one version a member requires.
type Swap struct {
	Path    string
	Version string
	Dir     string
}

// swaps makes a replace for each version any member asks for of a module that
// is in the workspace, sorted by path and version.
func swaps(lives map[string]string, wanted map[string]map[string]bool) []Swap {
	var out []Swap
	for path, dir := range lives {
		for v := range wanted[path] {
			out = append(out, Swap{
				Path:    path,
				Version: v,
				Dir:     dir,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Version < out[j].Version
	})
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
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
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
		f := strings.Fields(line)
		if len(f) != 2 || !strings.HasPrefix(f[1], "v") {
			continue
		}
		out[f[0]] = f[1]
	}
	return out
}

// modules lists the directories in a tree that hold a go.mod, as prefixes
// ending in a slash. the root is the empty string.
func modules(r repo.Repo, tree string) []string {
	out, err := r.Git("ls-tree", "-r", "--name-only", tree)
	if err != nil {
		return nil
	}
	var at []string
	for _, path := range strings.Split(out, "\n") {
		path = strings.TrimSpace(path)
		if path != "go.mod" && !strings.HasSuffix(path, "/go.mod") {
			continue
		}
		at = append(at, strings.TrimSuffix(path, "go.mod"))
	}
	return at
}

// allowedIn reads a member's own .game and moves each path under the
// member's name, so the allowances travel with the member.
func allowedIn(member, game string) map[string][]string {
	out := map[string][]string{}
	for _, line := range strings.Split(game, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "sweep allow ") {
			continue
		}
		f := strings.Fields(strings.TrimPrefix(line, "sweep allow "))
		if len(f) < 2 {
			continue
		}
		at := member + "/" + strings.Trim(f[0], "\"'")
		out[at] = append(out[at], f[1:]...)
	}
	return out
}

// excluded reads paths from a .game file to skip in sweep.
func excluded(game string) []string {
	var out []string
	for _, line := range strings.Split(game, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "exclude ") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, "exclude "))
		path = strings.Trim(path, "\"'")
		if path != "" {
			out = append(out, path)
		}
	}
	return out
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
