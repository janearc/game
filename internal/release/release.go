// Package release cuts the published history: one commit per release,
// each holding the whole tree and none of the road to it. The next one
// is built from the branch you are on, parented on the public root's
// current main so a checkout only ever fast-forwards, proven to have
// exactly that branch's tree, swept, and pushed.
package release

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/janearc/game/internal/bounce"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
)

// Options are what a release needs to know.
type Options struct {
	Public  string   // the public root, a path or url
	Message string   // the commit message, sign-off line included
	Words   []string // the sweep's word list
	Exclude []string // path prefixes the sweep leaves alone, kept verbatim
	Tag     string   // a tag to put on the public root at the release, or none
}

// Mark is the private commit the last release to a public root was cut
// from, kept as a ref in the private repository, one per root, so
// "since the last release" means since the last release *there*: a
// beta root and a stable root each remember their own. It never appears
// in a public message.
func Mark(public string) string {
	name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(public), ".git"), "-root")
	return "refs/game/released/" + name
}

// retire removes the one mark the first releases kept, refs/game/released,
// which stands where the per-root marks now live; a repository that has
// it starts its notes fresh.
func retire(r repo.Repo) {
	if _, err := r.Git("rev-parse", "--verify", "-q", "refs/game/released"); err == nil {
		r.Git("update-ref", "-d", "refs/game/released")
	}
}

// Notes are the private commit subjects since the mark for a public
// root, for a release message; each is swept and bounced before it may
// leave, since a subject can name a person as easily as a file can. No
// mark, no notes.
func Notes(r repo.Repo, public string, words []string, author string) ([]string, []sweep.Hit, error) {
	retire(r)
	since, err := r.Git("rev-parse", "--verify", "-q", Mark(public))
	if err != nil || since == "" {
		return nil, nil, nil
	}
	out, err := r.Git("log", "--format=%s", since+"..HEAD")
	if err != nil {
		return nil, nil, err
	}
	var notes []string
	var hits []sweep.Hit
	for _, subject := range strings.Split(out, "\n") {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			continue
		}
		hits = append(hits, sweep.Text("a commit subject", subject, words)...)
		if author != "" && len(bounce.Check("+"+subject, author)) > 0 {
			hits = append(hits, sweep.Hit{Kind: "the author in the third person", File: "a commit subject"})
		}
		notes = append(notes, subject)
	}
	return notes, hits, nil
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
	// the mark moves only once the release has landed.
	retire(r)
	if _, err := r.Git("update-ref", Mark(o.Public), "HEAD"); err != nil {
		return res, err
	}
	if o.Tag != "" {
		if _, err := r.GitIn(o.Message+"\n", "tag", "-a", o.Tag, commit, "-F", "-"); err != nil {
			return res, err
		}
		if _, err := r.Git("push", o.Public, "refs/tags/"+o.Tag); err != nil {
			return res, err
		}
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
