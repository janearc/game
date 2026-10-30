package main

import (
	"fmt"
	"io"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/repo"
)

// trees is game trees. it shows the block this repository matched, its
// road and dist, and whether the dist is guarded, on out.
func trees(here repo.Repo, cfg config.Config, out io.Writer) error {
	mine, held := cfg.Mine()
	if !held {
		fmt.Fprintf(out, "a local build: %v\n", cfg.NoBlock())
	}
	road := orNone(mine.Road(), cfg.Dotfile())
	dist := orNone(mine.Dist(), cfg.Dotfile())
	fmt.Fprintf(out, "road: %s\n", road)
	fmt.Fprintf(out, "dist: %s\n", dist)
	return guard(here, mine.Dist(), out)
}
