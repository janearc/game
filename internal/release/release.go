// Package release cuts what leaves: one flat commit per release, holding
// the whole tree and none of the road to it.
//
// Prepare builds that commit from the branch you are on, parented on
// dist's current main so a clone only ever fast-forwards, proves it has
// exactly that branch's tree, sweeps it, and keeps it under
// refs/game/release.
//
// Push sends a prepared release to dist, and nothing else ever does.
package release

import (
	"fmt"
	"strings"

	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// Options are what a release needs to know.
type Options struct {
	Name    string   // the project, which names the release commit
	Dist    string   // the clean remote, a path or url
	Tag     string   // the release's tag, v0.3.0 say
	Words   []string // the sweep's word list
	Exclude []string // path prefixes the sweep leaves alone, kept verbatim
	// Allow is, per file, the kinds of sweep hit this repository has
	// declared it means to carry.
	Allow map[string][]string
	// Omit is files that belong on the road and in no release. the
	// sweep's exclude only scopes what is scanned; a file nobody scans
	// still ships, so keeping it out is a separate word.
	Omit   []string
	Author string   // the author's name, refused in the released tree
	Values []string // the builder's setting values, refused in the tree
}

// Ref is where a prepared release waits for its push.
func Ref(tag string) string { return "refs/game/release/" + tag }

// Message is a release commit's whole message: the project and its tag,
// and no notes, since the notes are the road and the road stays home.
func Message(name, tag string) string { return name + " " + tag }

// Result says what happened.
type Result struct {
	Commit  string
	Tree    string
	Parent  string // dist's main when the release was prepared, or empty
	Skipped bool   // dist's main already has this tree
	Hits    []sweep.Hit
}

// Prepare builds, proves and sweeps a release and keeps it at Ref(tag).
// A dirty tree, a tree mismatch, a sweep hit or a setting's value in the
// tree stops it, and nothing is kept.
func Prepare(r repo.Repo, o Options) (Result, error) {
	var res Result
	if o.Tag == "" {
		return res, fmt.Errorf(
			"a release needs a tag: game release v0.3.0",
		)
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
	tree, err := r.TreeWithout("HEAD", o.Omit)
	if err != nil {
		return res, err
	}
	res.Tree = tree
	current, err := r.RemoteMain(o.Dist)
	if err != nil {
		return res, err
	}
	res.Parent = current
	args := []string{"commit-tree", tree}
	if current != "" {
		_, err := r.Git("fetch", "-q", o.Dist, "refs/heads/main")
		if err != nil {
			return res, err
		}
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
		r, res.Commit, o.Words, o.Author, o.Allow,
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

// values is every file at a revision that holds one of the builder's
// setting values: a value belongs in a dotfile, and a release that
// carries one has carried the builder with it.
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

// Guard is the environment a push to dist carries, which the pre-push
// hook game installs lets through and nothing else sets.
const Guard = "GAME_RELEASE_PUSH=1"

// Push sends a prepared release to dist: its commit as main and its tag.
// It refuses when dist's main has moved since the release was prepared,
// because the flat commit's parent would no longer be what a clone has.
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
