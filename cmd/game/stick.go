package main

import (
	"fmt"
	"io"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sticky"
)

// stick is game stick. it claims this repository for your author name
// by committing .sticky, then pushes, so the other person's game sees
// the claim. it says what it did on out.
func stick(here repo.Repo, cfg config.Config, out io.Writer) error {
	if cfg.Author() == "" {
		return fmt.Errorf(
			"stick needs an author: \"author NAME\" in " +
				"~/.config/game/config",
		)
	}
	at, err := roadFor(here, cfg)
	if err != nil {
		return err
	}
	// the road's tip first, when the road has this branch. a claim
	// committed behind it cannot be pushed.
	if err := catchUp(here, at, out); err != nil {
		return err
	}
	made, err := sticky.Stick(here, at, cfg.Author())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "stick: stuck on %s by %s\n", made.Branch(),
		made.Holder())
	if err := pushRoad(here, cfg, out); err != nil {
		return fmt.Errorf(
			"stuck here, but the claim did not reach the "+
				"road: %v; game push sends it",
			err,
		)
	}
	return nil
}

// catchUp fast-forwards this branch to the road's tip when the road is
// ahead, and says so on out. a detached head is left for Stick to refuse.
func catchUp(here repo.Repo, road string, out io.Writer) error {
	branch, err := here.Branch()
	if err != nil || branch == "HEAD" {
		return err
	}
	behind, err := sticky.Behind(here, road, branch)
	if err != nil || count.None(behind) {
		return err
	}
	tip, _, err := sticky.Sync(here, road, branch)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "stick: caught up to the road at %s first\n",
		repo.Short(tip))
	return nil
}

// follow is game sticky, the pairing verb. it fast-forwards this
// branch to the road's tip, and every build does the same while the
// claim holds. it says where it is stuck on out.
func follow(here repo.Repo, cfg config.Config, out io.Writer) error {
	at, err := roadFor(here, cfg)
	if err != nil {
		return err
	}
	branch, err := onBranch(here)
	if err != nil {
		return err
	}
	tip, _, err := sticky.Sync(here, at, branch)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "sticky: stuck to %s\n", repo.Short(tip))
	return nil
}

// onBranch is the branch checked out, or why there is none to name.
func onBranch(here repo.Repo) (string, error) {
	branch, err := here.Branch()
	if err != nil {
		return "", fmt.Errorf("no branch to name: %w", err)
	}
	return branch, nil
}

// unstick is game unstick. it releases the claim, commits and pushes,
// so the other person sees the claim is gone. it says what it did on out.
func unstick(here repo.Repo, cfg config.Config, out io.Writer) error {
	// check the road first. releasing the claim is a commit, and if it
	// can't be pushed, the other person still sees the claim.
	at, err := roadFor(here, cfg)
	if err != nil {
		return err
	}
	// and take the other person's changes first, for the same reason.
	branch, err := onBranch(here)
	if err != nil {
		return err
	}
	if behind, err := sticky.Behind(here, at, branch); err != nil {
		return err
	} else if count.Positive(behind) {
		return mergeFirst()
	}
	tip, err := sticky.Unstick(here)
	if err != nil {
		return err
	}
	fmt.Fprintf(
		out,
		"unstick: no longer sticky %s; cleared for publish\n",
		repo.Short(tip),
	)
	if err := pushRoad(here, cfg, out); err != nil {
		return fmt.Errorf(
			"unstuck here, but the road still carries the claim: "+
				"%v; game push sends the release",
			err,
		)
	}
	return nil
}
