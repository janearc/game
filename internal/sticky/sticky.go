// Package sticky is a baton for two people pairing on one repository. the
// claim is a committed file at the root.
//
// Stick claims it, Sync follows the holder's branch, and Unstick releases it
// and commits.
package sticky

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/repo"
)

// Path is the claim's name, committed at the repository's root.
const Path = ".sticky"

// Claim is who holds the baton, and where they hold it.
type Claim struct {
	Holder string // whoever's name the claim carries
	Branch string // the branch it was stuck on
	Commit string // the commit, on the road, that carries it
}

// Stuck returns who holds the claim in this working tree, and whether there
// is one. it reads the file on disk, not a revision.
func Stuck(dir string) (string, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, Path))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(b)), true, nil
}

// Holder finds the claimant for a road, checking all branches. the baton
// is per repo, not per branch. any branch's claim answers the question.
func Holder(r repo.Repo, road string) (Claim, bool, error) {
	heads, err := r.Git("ls-remote", "--heads", road)
	if err != nil {
		return Claim{}, false, err
	}
	for _, line := range strings.Split(heads, "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		sha, ref := f[0], f[1]
		if _, err := r.Git("fetch", "-q", road, ref); err != nil {
			continue
		}
		holder, err := r.Show("FETCH_HEAD", Path)
		if err != nil {
			continue // no claim on this branch
		}
		return Claim{
			Holder: strings.TrimSpace(holder),
			Branch: strings.TrimPrefix(ref, "refs/heads/"),
			Commit: sha,
		}, true, nil
	}
	return Claim{}, false, nil
}

// Stick claims the repo for a holder. it refuses on main, on a detached
// head, and if another holder already claims it.
func Stick(r repo.Repo, road, holder string) (Claim, error) {
	branch, err := r.Branch()
	if err != nil {
		return Claim{}, err
	}
	// a claim committed on a detached head lives on no branch: it
	// cannot be pushed, and it blocks the next stick with "already
	// stuck here". refused before anything is written.
	if branch == "HEAD" {
		return Claim{}, fmt.Errorf(
			"stick refuses a detached head; check out a branch " +
				"first",
		)
	}
	if branch == "main" {
		return Claim{}, fmt.Errorf(
			"stick refuses main; check out a branch first",
		)
	}
	if h, ok, err := Stuck(r.Dir); err != nil {
		return Claim{}, err
	} else if ok {
		return Claim{}, fmt.Errorf("already stuck here, by %s", h)
	}
	claim, held, err := Holder(r, road)
	if err != nil {
		return Claim{}, err
	}
	if held {
		return Claim{}, fmt.Errorf(
			"%s already holds this repository, stuck on %s at %s",
			claim.Holder,
			claim.Branch,
			claim.Commit[:7],
		)
	}
	if err := os.WriteFile(
		filepath.Join(r.Dir, Path), []byte(holder+"\n"), 0o644,
	); err != nil {
		return Claim{}, err
	}
	if _, err := r.Git("add", "--", Path); err != nil {
		return Claim{}, err
	}
	// the claim alone: whatever else is staged is theirs, mid-work,
	// and stays staged rather than riding into a commit called stick.
	if _, err := r.GitIn(
		"", "commit", "-q", "-m", "stick", "--", Path,
	); err != nil {
		return Claim{}, err
	}
	commit, err := r.Git("rev-parse", "HEAD")
	if err != nil {
		return Claim{}, err
	}
	return Claim{Holder: holder, Branch: branch, Commit: commit}, nil
}

// Sync fetches branch from road and fast-forwards to its tip. it returns the
// tip and whether it moved. it refuses diverged histories, since that means
// two people held the claim at once. a tree that is only dirty gets git's own
// words, naming the file to commit or stash.
func Sync(
	r repo.Repo, road, branch string,
) (tip string, moved bool, err error) {
	if _, err = r.Git("fetch", "-q", road, branch); err != nil {
		return "", false, err
	}
	before, err := r.Git("rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	if _, err = r.Git(
		"merge-base", "--is-ancestor", "HEAD", "FETCH_HEAD",
	); err != nil {
		return "", false, fmt.Errorf(
			"%s has diverged from the road; fast-forward only, "+
				"reconcile with whoever else is stuck here",
			branch,
		)
	}
	_, err = r.Git("merge", "--ff-only", "-q", "FETCH_HEAD")
	if err != nil {
		return "", false, fmt.Errorf(
			"%s is behind the road and cannot fast-forward over "+
				"what is uncommitted here: %v",
			branch,
			err,
		)
	}
	after, err := r.Git("rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	return after, after != before, nil
}

// Behind is how many commits the road has on branch that this tree does not.
// a road without the branch gives 0.
func Behind(r repo.Repo, road, branch string) (int, error) {
	heads, err := r.Git("ls-remote", "--heads", road, "refs/heads/"+branch)
	if err != nil {
		return 0, err
	}
	if heads == "" {
		return 0, nil
	}
	if _, err := r.Git("fetch", "-q", road, branch); err != nil {
		return 0, err
	}
	n, err := r.Git("rev-list", "--count", "HEAD..FETCH_HEAD")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(n)
}

// Unstick removes the claim file, commits, and returns the new commit. it is
// an error if the tree is not stuck.
func Unstick(r repo.Repo) (string, error) {
	if _, ok, err := Stuck(r.Dir); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("not stuck; nothing to unstick")
	}
	if err := os.Remove(filepath.Join(r.Dir, Path)); err != nil {
		return "", err
	}
	if _, err := r.Git("add", "--", Path); err != nil {
		return "", err
	}
	// the removal alone, for the same reason stick commits the claim
	// alone: what else is staged is theirs.
	if _, err := r.GitIn(
		"", "commit", "-q", "-m", "unstick", "--", Path,
	); err != nil {
		return "", err
	}
	return r.Git("rev-parse", "HEAD")
}
