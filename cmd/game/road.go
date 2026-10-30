package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
)

// orNone returns a remote, or says there is none and where to add it.
func orNone(url, dotfile string) string {
	if url == "" {
		return "(none; add it to " + dotfile + ")"
	}
	return url
}

// pushRoad sends the branch you are on to the road, and says so first,
// so a push is never mistaken for a release. it says the push on out.
func pushRoad(here repo.Repo, cfg config.Config, out io.Writer) error {
	name := cfg.Named()
	road, err := roadFor(here, cfg)
	if err != nil {
		return err
	}
	mine, _ := cfg.Mine()
	dist := mine.Dist()
	if road == dist {
		return fmt.Errorf(
			"the road and dist for %s are the same "+
				"remote; they never are",
			name,
		)
	}
	if err := guard(here, dist, out); err != nil {
		return err
	}
	branch, err := here.Branch()
	if err != nil || branch == "HEAD" {
		return fmt.Errorf(
			"not on a branch; check one out before " +
				"pushing to the road",
		)
	}
	fmt.Fprintf(out, "pushed %s to the road, %s\n", branch, road)
	_, err = here.Git("push", road, branch)
	return err
}

// roadFor returns this project's road or an error explaining why.
func roadFor(here repo.Repo, cfg config.Config) (string, error) {
	name := cfg.Named()
	mine, held := cfg.Mine()
	if !held {
		return "", cfg.NoBlock()
	}
	if mine.Road() == "" {
		return "", fmt.Errorf(
			"no road for %s; its block in %s has none",
			name,
			cfg.Dotfile(),
		)
	}
	err := there(mine.Road(), here.Dir(), name, cfg.Dotfile())
	if err != nil {
		return "", err
	}
	return mine.Road(), nil
}

// there checks that a remote given as a path on this machine exists. if
// it does not, the error names the path, the project and the config file
// it came from. a relative path is resolved from the repository. urls are
// not checked.
func there(remote_, dir, name, dotfile string) error {
	if repo.Elsewhere(remote_) {
		return nil
	}
	at := remote_
	if !filepath.IsAbs(at) {
		at = filepath.Join(dir, at)
	}
	if _, err := os.Stat(at); err == nil {
		return nil
	}
	where := ""
	if at != remote_ {
		where = " (looked at " + at + ")"
	}
	return fmt.Errorf(
		"the road for %s is not there: %s%s; it is named in %s "+
			"under \"project %s\"",
		name, remote_, where, dotfile, name,
	)
}
