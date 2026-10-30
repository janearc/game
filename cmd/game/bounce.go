package main

import (
	"fmt"
	"os"

	"github.com/janearc/game/internal/bounce"
	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/puzzlemouth/pzm"
)

// bounceVerb is game bounce, over what is staged or a range given. added
// lines that name the author in the third person are held until signed off
// in the environment, and what it held is said on the call's output.
func bounceVerb(here repo.Repo, cfg config.Config, call pzm.Call) error {
	args, out := call.Args(), call.Out()
	signed := os.Getenv("BOUNCE_OK") != ""
	if cfg.Author() == "" {
		return fmt.Errorf(
			"bounce needs an author: \"author " +
				"NAME\" in ~/.config/game/config",
		)
	}
	var hits []bounce.Hit
	var err error
	if rng, _, said := verbWord(args); said {
		hits, err = bounce.Range(here, rng, cfg.Author())
	} else {
		hits, err = bounce.Staged(here, cfg.Author())
	}
	if err != nil || count.None(len(hits)) {
		return err
	}
	fmt.Fprintln(
		out,
		"bounce: the author in the third "+
			"person, in added lines:",
	)
	for _, hit := range hits {
		fmt.Fprintln(out, "  "+hit.Line())
	}
	if signed {
		fmt.Fprintln(
			out,
			"bounce: signed off "+
				"(BOUNCE_OK); passing",
		)
		return nil
	}
	return fmt.Errorf("refused. reword to the " +
		"first person, or " +
		"BOUNCE_OK=1 to sign it off")
}
