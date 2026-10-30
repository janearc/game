// Package release cuts what leaves. a release is one flat commit of the
// whole tree, with none of the road to it.
//
// Prepare builds the commit from the current branch, on top of dist's main.
// it checks the tree, sweeps it, and stores it under refs/game/release.
//
// Push sends a prepared release to dist.
package release

import (
	"fmt"
	"os"
	"strings"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// errNoNeverWords is why a game built without never-words will not release.
var errNoNeverWords = fmt.Errorf("this game was built without " +
	"never-words, so it cannot sweep for them: run bootstrap.sh where " +
	"the list is, and release again")

// Options are what a release needs to know. For makes them with what
// every release has: the project, the dist and the tag. The rest is
// said by a method each.
type Options struct {
	name string // the project, which names the release commit
	dist string // the clean remote, a path or url
	tag  string // the release's tag, v0.3.0 say
	// never is the never-words game was built with.
	never   sweep.Never
	exclude []string // path prefixes the sweep leaves alone, kept verbatim
	// allow is the kinds of sweep hit this repository accepts, per file.
	allow map[string][]string
	// omit is files and directories left out of every release. exclude
	// only skips scanning.
	omit   []string
	author string // the author's name, refused in the released tree
	// license is a path to the license every release carries. empty means
	// the release keeps the repository's license.
	license string
	values  []string // your setting values, refused in the tree
	// patches is the directories .game names where a stack keeps its
	// members' patches beside patches/.
	patches []string
	// qualifier is asked about a stack before its ref is written. Nil
	// asks no one.
	qualifier func(Stacked) error
}

// For is a release of the project name to dist at tag, with nothing
// else asked yet.
func For(name, dist, tag string) Options {
	return Options{name: name, dist: dist, tag: tag}
}

// Swept is the options with what the sweep is held to: the never-words
// game was built with, the path prefixes it leaves alone, and the
// kinds of hit each file may carry.
func (asked Options) Swept(never sweep.Never, exclude []string,
	allow map[string][]string) Options {
	asked.never, asked.exclude, asked.allow = never, exclude, allow
	return asked
}

// Omitting is the options with the files and directories every
// release leaves out.
func (asked Options) Omitting(omit []string) Options {
	asked.omit = omit
	return asked
}

// By is the options with the author's name, refused in the released
// tree.
func (asked Options) By(author string) Options {
	asked.author = author
	return asked
}

// Licensed is the options with the path of the license every release
// carries.
func (asked Options) Licensed(path string) Options {
	asked.license = path
	return asked
}

// Refusing is the options with the setting values refused in the tree.
func (asked Options) Refusing(values []string) Options {
	asked.values = values
	return asked
}

// PatchedFrom is the options with the directories a stack keeps its
// members' patches in, beside patches/.
func (asked Options) PatchedFrom(dirs []string) Options {
	asked.patches = dirs
	return asked
}

// ref is where a prepared release waits for its push.
func ref(tag string) string { return "refs/game/release/" + tag }

// message is a release commit's whole message: the project and its tag.
func message(name, tag string) string { return name + " " + tag }

// Result says what happened.
type Result struct {
	commit  string
	tree    string
	parent  string // dist's main when the release was prepared, or empty
	skipped bool   // dist's main already has this tree
	hits    []sweep.Hit
}

// Commit is the release commit prepared, or the one dist already had.
func (made Result) Commit() string { return made.commit }

// Tree is the flat tree the release carries.
func (made Result) Tree() string { return made.tree }

// plan gives the branch's tree and dist's main, fetching main when dist has
// one. Prepare and Diff both start here.
func plan(here repo.Repo, asked Options) (tree, parent string, err error) {
	if asked.license != "" {
		tree, err = here.TreeWithLicense(
			"HEAD", asked.omit, "LICENSE.txt", asked.license,
		)
	} else {
		tree, err = here.TreeWithout("HEAD", asked.omit)
	}
	if err != nil {
		return "", "", err
	}
	if parent, err = here.RemoteMain(asked.dist); err != nil {
		return "", "", err
	}
	if parent != "" {
		if _, err = here.Git(
			"fetch", "-q", asked.dist, "refs/heads/main",
		); err != nil {
			return "", "", err
		}
	}
	return tree, parent, nil
}

// Difference is what a release would change on dist.
type Difference struct {
	tree   string // the flat tree this branch would send
	parent string // dist's main, empty when dist has nothing yet
	stat   string // one line a file: the shape of the change
	patch  string // the whole unified diff
	same   bool   // dist already has this tree, so a release is a no-op
	hits   []sweep.Hit
}

// Tree is the flat tree this branch would send.
func (diff Difference) Tree() string { return diff.tree }

// Parent is dist's main, empty when dist has nothing yet.
func (diff Difference) Parent() string { return diff.parent }

// Stat is one line a file: the shape of the change.
func (diff Difference) Stat() string { return diff.stat }

// Patch is the whole unified diff.
func (diff Difference) Patch() string { return diff.patch }

// Same is whether dist already has this tree, so a release is a no-op.
func (diff Difference) Same() bool { return diff.same }

// Hits is what the sweep found, which a diff reports instead of
// refusing.
func (diff Difference) Hits() []sweep.Hit { return diff.hits }

// Diff shows what a push would change on dist. it reports sweep hits
// instead of refusing.
func Diff(here repo.Repo, asked Options) (*Difference, error) {
	tree, parent, err := plan(here, asked)
	if err != nil {
		return nil, err
	}
	from := parent
	if from == "" {
		// the empty tree is asked of git rather than typed from memory.
		if from, err = here.Git(
			"hash-object", "-t", "tree", os.DevNull,
		); err != nil {
			return nil, err
		}
	} else if had, _ := here.Tree(parent); had == tree {
		same := NewDifference(tree, parent, "", "", true, nil)
		return &same, nil
	}
	stat, err := here.Git("diff", "--stat", from, tree)
	if err != nil {
		return nil, err
	}
	patch, err := here.Git("diff", from, tree)
	if err != nil {
		return nil, err
	}
	hits, err := sweep.Tree(
		here, tree, asked.never, asked.author, asked.allow,
		asked.exclude...,
	)
	if err != nil {
		return nil, err
	}
	diff := NewDifference(tree, parent, stat, patch, false, hits)
	return &diff, nil
}

// NewDifference is the difference between dist, whose main is parent
// (empty when dist has nothing yet), and tree: its stat, its patch and the
// sweep's hits. same is that dist already has the tree.
func NewDifference(tree, parent, stat, patch string, same bool,
	hits []sweep.Hit) Difference {
	return Difference{tree: tree, parent: parent, stat: stat, patch: patch,
		same: same, hits: hits}
}

// Prepare makes the release commit and keeps it at ref(tag) for its push.
// it refuses if the sweep finds anything.
func Prepare(here repo.Repo, asked Options) (*Result, error) {
	var res Result
	if err := preparable(here, asked); err != nil {
		return nil, err
	}
	tree, current, err := plan(here, asked)
	if err != nil {
		return nil, err
	}
	res.tree, res.parent = tree, current
	if err := flatCommit(here, asked, &res); err != nil {
		return nil, err
	}
	if err := refuseRelease(here, asked, &res); err != nil {
		return nil, err
	}
	_, err = here.Git("update-ref", ref(asked.tag), res.commit)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// preparable checks for a tag, never-words and a clean tree.
func preparable(here repo.Repo, asked Options) error {
	if asked.tag == "" {
		return fmt.Errorf("a release needs a tag: game release v0.3.0")
	}
	if asked.never.Empty() {
		return errNoNeverWords
	}
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

// flatCommit makes the release commit. it reuses dist's main if that holds
// the tree, else commits the tree on top of it, and checks the result.
func flatCommit(here repo.Repo, asked Options, res *Result) error {
	commit, skipped, err := flat(here, asked, res.tree, res.parent)
	if err != nil {
		return err
	}
	res.commit, res.skipped = commit, skipped
	if have, err := here.Tree(res.commit); err != nil || have != res.tree {
		return fmt.Errorf(
			"the flat tree does not match HEAD: %s vs %s",
			have,
			res.tree,
		)
	}
	return nil
}

// flat is the commit of a tree on top of parent, which is dist's main. It
// also says whether the commit is dist's own, which is reused when it
// holds the tree. Release and stack cut their commits this way. The
// message is the project and the tag.
func flat(
	here repo.Repo, asked Options, tree, parent string,
) (string, bool, error) {
	args := []string{"commit-tree", tree}
	if parent != "" {
		if have, _ := here.Tree(parent); have == tree {
			return parent, true, nil
		}
		args = append(args, "-p", parent)
	}
	made, err := here.GitIn(message(asked.name, asked.tag)+"\n", args...)
	return made, false, err
}

// refuseRelease sweeps the commit's history and tree and looks for the
// setting values. any hit refuses the release.
func refuseRelease(here repo.Repo, asked Options, res *Result) error {
	hits, err := sweep.History(here, res.commit)
	if err != nil {
		return fmt.Errorf("the history of %s: %w", res.commit, err)
	}
	treeHits, err := sweep.Tree(
		here, res.commit, asked.never, asked.author, asked.allow,
		asked.exclude...)
	if err != nil {
		return err
	}
	found, err := values(here, res.commit, asked.values)
	if err != nil {
		return err
	}
	res.hits = append(append(hits, treeHits...), found...)
	if count.Positive(len(res.hits)) {
		return fmt.Errorf("refused: %s", describe(res.hits))
	}
	return nil
}

// values finds files at a revision that hold your setting values.
func values(here repo.Repo, rev string, vals []string) ([]sweep.Hit, error) {
	if count.None(len(vals)) {
		return nil, nil
	}
	held := &holding{values: vals}
	if err := here.Blobs(rev, nil, held.file); err != nil {
		return nil, err
	}
	return held.hits, nil
}

// holding is a search of a tree for the setting values, and what it
// has found so far.
type holding struct {
	values []string
	hits   []sweep.Hit
}

// file reads one file's body for any of the values.
func (held *holding) file(file, body string) error {
	for _, value := range held.values {
		if value != "" && strings.Contains(body, value) {
			held.hits = append(held.hits, sweep.Found(
				"a setting's value", file))
			return nil
		}
	}
	return nil
}

// Guard is the environment a push to dist carries. the pre-push hook lets it
// through.
const Guard = "GAME_RELEASE_PUSH=1"

// Push sends a release's commit and tag to dist in one atomic push. It
// refuses if dist's main has changed since the release was prepared. It
// refuses a dist that cannot take both refs at once. It refuses when this
// repository has a tag of that name and leaves that tag as it is.
func Push(here repo.Repo, asked Options) (*Result, error) {
	var res Result
	commit, err := here.Git("rev-parse", "--verify", "-q", ref(asked.tag))
	if err != nil || commit == "" {
		return nil, fmt.Errorf(
			"no prepared release %s; run game release %s first",
			asked.tag,
			asked.tag,
		)
	}
	res.commit = commit
	current, err := here.RemoteMain(asked.dist)
	if err != nil {
		return nil, err
	}
	parent, _ := here.Git("rev-parse", "--verify", "-q", commit+"^")
	if current != "" && current != commit && current != parent {
		return nil, fmt.Errorf(
			"dist's main moved since %s was prepared; "+
				"run game release %s again",
			asked.tag,
			asked.tag,
		)
	}
	if err := tagFree(here, asked.tag); err != nil {
		return nil, err
	}
	note := message(asked.name, asked.tag) + "\n"
	if _, err := here.GitIn(
		note, "tag", "-a", asked.tag, commit, "-F", "-",
	); err != nil {
		return nil, err
	}
	defer here.Git("tag", "-d", asked.tag)
	refs := []string{"refs/tags/" + asked.tag}
	if current == commit {
		res.skipped = true
	} else {
		refs = append([]string{commit + ":refs/heads/main"}, refs...)
	}
	args := append([]string{"push", "--atomic", asked.dist}, refs...)
	if _, err = here.GitEnv([]string{Guard}, args...); err != nil {
		return nil, err
	}
	return &res, nil
}

// tagFree refuses a tag name this repository already has. Push makes a
// tag for the push and deletes it after. The one that was there first is
// not Push's to replace or to delete.
func tagFree(here repo.Repo, tag string) error {
	if _, err := here.Git("rev-parse", "--verify", "-q",
		"refs/tags/"+tag); err != nil {
		return nil
	}
	return fmt.Errorf(
		"tag %s is already in this repository, and is not game's to "+
			"replace; move or delete it, then push again", tag)
}

// describe is hits as one line a person can act on.
func describe(hits []sweep.Hit) string {
	parts := make([]string, count.Nothing, len(hits))
	for _, hit := range hits {
		parts = append(parts, hit.String())
	}
	return strings.Join(parts, "; ")
}

// Qualifying is the options with who a stack is shown to: a function
// asked about the stack once it is assembled and swept, and before its
// ref is written. Any error from it refuses the stack. The ref an earlier
// stack left at the same tag stays as it was.
func (asked Options) Qualifying(qualifier func(Stacked) error) Options {
	asked.qualifier = qualifier
	return asked
}
