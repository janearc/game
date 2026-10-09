// Package release cuts what leaves: one flat commit per release, holding
// the whole tree and none of the road to it.
//
// Prepare builds a commit from the current branch, parented on dist's
// main so clones fast-forward, proves it has that branch's tree, sweeps
// it, and stores it under refs/game/release.
//
// Push sends a prepared release to dist.
package release

import (
	"fmt"
	"os"
	"strings"

	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// errNoNeverWords is why a game built without never-words will not release.
var errNoNeverWords = fmt.Errorf("this game was built without " +
	"never-words, so it cannot sweep for them: run bootstrap.sh where " +
	"the list is, and release again")

// Options are what a release needs to know.
type Options struct {
	Name string // the project, which names the release commit
	Dist string // the clean remote, a path or url
	Tag  string // the release's tag, v0.3.0 say
	// Never is the never-words game was built with.
	Never   sweep.Never
	Exclude []string // path prefixes the sweep leaves alone, kept verbatim
	// Allow is, per file, the kinds of sweep hit this repository accepts.
	Allow map[string][]string
	// Omit is files that stay on the road and are left out of every
	// release. exclude does not do this; it only skips scanning.
	Omit   []string
	Author string // the author's name, refused in the released tree
	// License is a path to the one license every release carries. empty
	// means the release keeps whatever license the repository has.
	License string
	Values  []string // your setting values, refused in the tree
}

// Ref is where a prepared release waits for its push.
func Ref(tag string) string { return "refs/game/release/" + tag }

// Message is a release commit's whole message: the project and its tag.
func Message(name, tag string) string { return name + " " + tag }

// Result says what happened.
type Result struct {
	Commit  string
	Tree    string
	Parent  string // dist's main when the release was prepared, or empty
	Skipped bool   // dist's main already has this tree
	Hits    []sweep.Hit
}

// Plan is the two ends of a release: dist's main and the branch's tree.
//
// Prepare and Diff both start here, so a diff shows what a push would send.
// it fetches dist's main first.
func Plan(r repo.Repo, o Options) (tree, parent string, err error) {
	if o.License != "" {
		tree, err = r.TreeWithLicense(
			"HEAD", o.Omit, "LICENSE.txt", o.License,
		)
	} else {
		tree, err = r.TreeWithout("HEAD", o.Omit)
	}
	if err != nil {
		return "", "", err
	}
	if parent, err = r.RemoteMain(o.Dist); err != nil {
		return "", "", err
	}
	if parent != "" {
		if _, err = r.Git(
			"fetch", "-q", o.Dist, "refs/heads/main",
		); err != nil {
			return "", "", err
		}
	}
	return tree, parent, nil
}

// Difference is what a release would change on dist.
type Difference struct {
	Tree   string // the flat tree this branch would send
	Parent string // dist's main, empty when dist has nothing yet
	Stat   string // one line a file: the shape of the change
	Patch  string // the whole unified diff
	Same   bool   // dist already has this tree, so a release is a no-op
	Hits   []sweep.Hit
}

// Diff shows what a push would change on dist. it reports sweep hits
// rather than refusing, because a refused release is worth looking at.
func Diff(r repo.Repo, o Options) (Difference, error) {
	var d Difference
	tree, parent, err := Plan(r, o)
	if err != nil {
		return d, err
	}
	d.Tree, d.Parent = tree, parent
	from := parent
	if from == "" {
		// the empty tree is asked of git rather than typed from memory.
		if from, err = r.Git(
			"hash-object", "-t", "tree", os.DevNull,
		); err != nil {
			return d, err
		}
	} else if t, _ := r.Tree(parent); t == tree {
		d.Same = true
		return d, nil
	}
	if d.Stat, err = r.Git("diff", "--stat", from, tree); err != nil {
		return d, err
	}
	if d.Patch, err = r.Git("diff", from, tree); err != nil {
		return d, err
	}
	if d.Hits, err = sweep.Tree(
		r, tree, o.Never, o.Author, o.Allow, o.Exclude...,
	); err != nil {
		return d, err
	}
	return d, nil
}

// Prepare builds, proves and sweeps a release, and keeps it at Ref(tag). a
// missing tag or never-words, a dirty tree, a tree mismatch, a sweep hit or a
// setting's value in the tree stops it with nothing kept.
func Prepare(r repo.Repo, o Options) (Result, error) {
	var res Result
	if o.Tag == "" {
		return res, fmt.Errorf(
			"a release needs a tag: game release v0.3.0",
		)
	}
	if o.Never.Empty() {
		return res, errNoNeverWords
	}
	clean, err := r.Clean()
	if err != nil {
		return res, err
	}
	if !clean {
		return res, fmt.Errorf(
			"the tree is dirty; commit first, to the road",
		)
	}
	tree, current, err := Plan(r, o)
	if err != nil {
		return res, err
	}
	res.Tree = tree
	res.Parent = current
	args := []string{"commit-tree", tree}
	if current != "" {
		if t, _ := r.Tree(current); t == tree {
			res.Skipped = true
			res.Commit = current
		} else {
			args = append(args, "-p", current)
		}
	}
	if !res.Skipped {
		commit, err := r.GitIn(Message(o.Name, o.Tag)+"\n", args...)
		if err != nil {
			return res, err
		}
		res.Commit = commit
	}
	if t, err := r.Tree(res.Commit); err != nil || t != tree {
		return res, fmt.Errorf(
			"the flat tree does not match HEAD: %s vs %s",
			t,
			tree,
		)
	}
	hits, err := sweep.History(r, res.Commit)
	if err != nil {
		return res, err
	}
	th, err := sweep.Tree(
		r, res.Commit, o.Never, o.Author, o.Allow,
		o.Exclude...)
	if err != nil {
		return res, err
	}
	vh, err := values(r, res.Commit, o.Values)
	if err != nil {
		return res, err
	}
	res.Hits = append(append(hits, th...), vh...)
	if len(res.Hits) > 0 {
		return res, fmt.Errorf("refused: %s", Describe(res.Hits))
	}
	_, err = r.Git("update-ref", Ref(o.Tag), res.Commit)
	return res, err
}

// values finds files at a revision that hold your setting values.
func values(r repo.Repo, rev string, vals []string) ([]sweep.Hit, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	files, err := r.Files(rev)
	if err != nil {
		return nil, err
	}
	var hits []sweep.Hit
	for _, f := range files {
		body, err := r.Show(rev, f)
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			if v != "" && strings.Contains(body, v) {
				hits = append(
					hits,
					sweep.Hit{
						Kind: "a setting's value",
						File: f,
					},
				)
				break
			}
		}
	}
	return hits, nil
}

// Guard is the environment a push to dist carries. the pre-push hook lets it
// through.
const Guard = "GAME_RELEASE_PUSH=1"

// Push sends a release's commit and tag to dist. it refuses if dist's
// main has changed since the release was prepared.
func Push(r repo.Repo, o Options) (Result, error) {
	var res Result
	commit, err := r.Git("rev-parse", "--verify", "-q", Ref(o.Tag))
	if err != nil || commit == "" {
		return res, fmt.Errorf(
			"no prepared release %s; run game release %s first",
			o.Tag,
			o.Tag,
		)
	}
	res.Commit = commit
	current, err := r.RemoteMain(o.Dist)
	if err != nil {
		return res, err
	}
	parent, _ := r.Git("rev-parse", "--verify", "-q", commit+"^")
	if current != "" && current != commit && current != parent {
		return res, fmt.Errorf(
			"dist's main moved since %s was prepared; "+
				"run game release %s again",
			o.Tag,
			o.Tag,
		)
	}
	if current != commit {
		_, err := r.GitEnv(
			[]string{Guard}, "push", o.Dist,
			commit+":refs/heads/main")
		if err != nil {
			return res, err
		}
	} else {
		res.Skipped = true
	}
	note := Message(o.Name, o.Tag) + "\n"
	if _, err := r.GitIn(
		note, "tag", "-a", "-f", o.Tag, commit, "-F", "-"); err != nil {
		return res, err
	}
	defer r.Git("tag", "-d", o.Tag)
	if _, err := r.GitEnv(
		[]string{Guard}, "push", o.Dist,
		"refs/tags/"+o.Tag); err != nil {
		return res, err
	}
	return res, nil
}

// Describe is hits as one line a person can act on.
func Describe(hits []sweep.Hit) string {
	parts := make([]string, 0, len(hits))
	for _, h := range hits {
		parts = append(parts, h.Kind+" in "+h.File)
	}
	return strings.Join(parts, "; ")
}
