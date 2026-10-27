// Package sticky is a baton for two people pairing on one repository. the
// Claim is a committed file at the root.
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

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
)

// stickyFile is the Claim's name, committed at the repository's root.
const stickyFile = ".sticky"

// ownerWrites is the mode the Claim file is written with: the owner
// writes and everyone reads, defined as 0644 in sys/stat.h's bits.
const ownerWrites = 0o644

// Claim is who holds the baton, and where they hold it.
type Claim struct {
	holder string // whoever's name the Claim carries
	branch string // the branch it was stuck on
	commit string // the commit, on the road, that carries it
}

// Holder is whoever's name the Claim carries.
func (made Claim) Holder() string { return made.holder }

// Branch is the branch the Claim was stuck on.
func (made Claim) Branch() string { return made.branch }

// short is the commit as git shows it short.
func (made Claim) short() string {
	return repo.Short(made.commit)
}

// Stuck returns who holds the Claim in this working tree, and whether there
// is one. it reads the file on disk, not a revision.
func Stuck(dir string) (string, bool, error) {
	body, err := os.ReadFile(filepath.Join(dir, stickyFile))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(string(body)), true, nil
}

// shaAndRef is the words of one ls-remote line, as git has defined it:
// the commit and the ref.
const shaAndRef = 2

// holder finds the claimant for a road across all branches. the baton
// is per repo, not per branch. any branch's Claim answers the question.
// a branch the road lists but will not give is an error, not an empty
// answer: nobody can say who holds it.
func holder(here repo.Repo, road string) (Claim, bool, error) {
	heads, err := here.Git("ls-remote", "--heads", road)
	if err != nil {
		return Claim{}, false, err
	}
	for _, line := range strings.Split(heads, "\n") {
		fields := strings.Fields(line)
		if len(fields) != shaAndRef {
			continue
		}
		sha, ref := fields[count.First], fields[count.Next(count.First)]
		if _, err := here.Git("fetch", "-q", road, ref); err != nil {
			return Claim{}, false, fmt.Errorf(
				"%s lists %s but will not give it: %w",
				road, ref, err,
			)
		}
		name, err := here.Show("FETCH_HEAD", stickyFile)
		if err != nil {
			continue // no Claim on this branch
		}
		return Claim{
			holder: strings.TrimSpace(name),
			branch: strings.TrimPrefix(ref, "refs/heads/"),
			commit: sha,
		}, true, nil
	}
	return Claim{}, false, nil
}

// stickable gives the branch a Claim may be made on. it refuses a detached
// head, main, a tree already stuck and a road someone else holds, before
// anything is written.
func stickable(here repo.Repo, road string) (string, error) {
	branch, err := here.Branch()
	if err != nil {
		return "", err
	}
	// a Claim on a detached head lives on no branch. it cannot be pushed
	// and it blocks the next stick with "already stuck here".
	if branch == "HEAD" {
		return "", fmt.Errorf(
			"stick refuses a detached head; check out a branch " +
				"first",
		)
	}
	if branch == "main" {
		return "", fmt.Errorf(
			"stick refuses main; check out a branch first",
		)
	}
	if holder, ok, err := Stuck(here.Dir()); err != nil {
		return "", err
	} else if ok {
		return "", fmt.Errorf("already stuck here, by %s", holder)
	}
	taken, held, err := holder(here, road)
	if err != nil {
		return "", err
	}
	if held {
		return "", fmt.Errorf(
			"%s already holds this repository, stuck on %s at %s",
			taken.holder,
			taken.branch,
			taken.short(),
		)
	}
	return branch, nil
}

// Stick claims the repository for holder. it commits the name in .sticky,
// alone, on the checked out branch, which is not main.
func Stick(here repo.Repo, road, holder string) (Claim, error) {
	branch, err := stickable(here, road)
	if err != nil {
		return Claim{}, err
	}
	at := here.Path(stickyFile)
	err = os.WriteFile(at, []byte(holder+"\n"), ownerWrites)
	if err != nil {
		return Claim{}, err
	}
	if _, err := here.Git("add", "--", stickyFile); err != nil {
		return Claim{}, err
	}
	// commit the Claim alone. whatever else is staged stays staged.
	if _, err := here.GitIn(
		"", "commit", "-q", "-m", "stick", "--", stickyFile,
	); err != nil {
		return Claim{}, err
	}
	commit, err := here.Git("rev-parse", "HEAD")
	if err != nil {
		return Claim{}, err
	}
	return Claim{holder: holder, branch: branch, commit: commit}, nil
}

// Sync fetches branch from road and fast-forwards to its tip. it returns the
// tip and whether it moved. a tree already holding the road's tip, with or
// without work of its own on top, is left as it is and its HEAD returned.
//
// it refuses diverged histories, since that means two people held the
// Claim at once. a tree that is only dirty gets git's own words, naming
// the file to commit or stash.
func Sync(
	here repo.Repo, road, branch string,
) (tip string, moved bool, err error) {
	if _, err = here.Git("fetch", "-q", road, branch); err != nil {
		return "", false, err
	}
	// HEAD and the road's tip, read at once: after a fast-forward the
	// tip is HEAD, so nothing need be asked afterwards
	heads, err := here.Git("rev-parse", "HEAD", "FETCH_HEAD")
	if err != nil {
		return "", false, err
	}
	before, tip, _ := strings.Cut(heads, "\n")
	// the road's tip already here: work ahead of it is the holder's,
	// not yet pushed, and there is nothing to take
	if _, err = here.Git(
		"merge-base", "--is-ancestor", "FETCH_HEAD", "HEAD",
	); err == nil {
		return before, false, nil
	}
	if _, err = here.Git(
		"merge-base", "--is-ancestor", "HEAD", "FETCH_HEAD",
	); err != nil {
		return "", false, fmt.Errorf(
			"%s has diverged from the road; fast-forward only, "+
				"reconcile with whoever else is stuck here",
			branch,
		)
	}
	_, err = here.Git("merge", "--ff-only", "-q", "FETCH_HEAD")
	if err != nil {
		return "", false, fmt.Errorf(
			"%s is behind the road and cannot fast-forward over "+
				"what is uncommitted here: %v",
			branch,
			err,
		)
	}
	return tip, tip != before, nil
}

// Behind is how many commits the road has on branch that this tree does not.
// a road without the branch gives nothing.
func Behind(here repo.Repo, road, branch string) (int, error) {
	heads, err := here.Git("ls-remote", "--heads", road,
		"refs/heads/"+branch)
	if err != nil {
		return count.Nothing, err
	}
	if heads == "" {
		return count.Nothing, nil
	}
	if _, err := here.Git("fetch", "-q", road, branch); err != nil {
		return count.Nothing, err
	}
	counted, err := here.Git("rev-list", "--count", "HEAD..FETCH_HEAD")
	if err != nil {
		return count.Nothing, err
	}
	return strconv.Atoi(counted)
}

// Unstick removes the Claim file, commits, and returns the new commit. it
// is an error if the tree is not stuck.
func Unstick(here repo.Repo) (string, error) {
	if _, ok, err := Stuck(here.Dir()); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("not stuck; nothing to unstick")
	}
	at := here.Path(stickyFile)
	if err := os.Remove(at); err != nil {
		return "", err
	}
	if _, err := here.Git("add", "--", stickyFile); err != nil {
		return "", err
	}
	// commit the removal alone. whatever else is staged stays staged.
	if _, err := here.GitIn(
		"", "commit", "-q", "-m", "unstick", "--", stickyFile,
	); err != nil {
		return "", err
	}
	return here.Git("rev-parse", "HEAD")
}
