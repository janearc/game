// Package release cuts the published history: one commit per release,
// each holding the whole tree and none of the road to it. The next one
// is built from the branch you are on, parented on the public root's
// current main so a checkout only ever fast-forwards, proven to have
// exactly that branch's tree, swept, and pushed.
package release

import (
	"fmt"
	"strings"

	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// Options are what a release needs to know.
type Options struct {
	Public  string   // the public root, a path or url
	Message string   // the commit message, sign-off line included
	Words   []string // the sweep's word list
	Exclude []string // path prefixes the sweep leaves alone, kept verbatim
}

// Result says what happened.
type Result struct {
	Commit  string
	Tree    string
	Skipped bool // the public main already had this tree
	Hits    []sweep.Hit
}

// Run builds, proves, sweeps and pushes. A dirty tree, a tree mismatch or
// a sweep hit stops it before the push, and the built commit is left on
// the branch `flat` for looking at.
func Run(r repo.Repo, o Options) (Result, error) {
	var res Result
	clean, err := r.Clean()
	if err != nil {
		return res, err
	}
	if !clean {
		return res, fmt.Errorf("the tree is dirty; commit or stash first")
	}
	tree, err := r.Tree("HEAD")
	if err != nil {
		return res, err
	}
	res.Tree = tree
	current, err := r.RemoteMain(o.Public)
	if err != nil {
		return res, err
	}
	args := []string{"commit-tree", tree}
	if current != "" {
		if _, err := r.Git("fetch", "-q", o.Public, "refs/heads/main"); err != nil {
			return res, err
		}
		if t, _ := r.Tree(current); t == tree {
			res.Skipped = true
			res.Commit = current
			return res, nil
		}
		args = append(args, "-p", current)
	}
	commit, err := r.GitIn(o.Message+"\n", args...)
	if err != nil {
		return res, err
	}
	res.Commit = commit
	if t, err := r.Tree(commit); err != nil || t != tree {
		return res, fmt.Errorf("the flat tree does not match HEAD: %s vs %s", t, tree)
	}
	if _, err := r.Git("update-ref", "refs/heads/flat", commit); err != nil {
		return res, err
	}
	hits, err := sweep.History(r, commit)
	if err != nil {
		return res, err
	}
	th, err := sweep.Tree(r, commit, o.Words, o.Exclude...)
	if err != nil {
		return res, err
	}
	res.Hits = append(hits, th...)
	if len(res.Hits) > 0 {
		return res, fmt.Errorf("refused: %s", Describe(res.Hits))
	}
	if _, err := r.Git("push", o.Public, commit+":refs/heads/main"); err != nil {
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
