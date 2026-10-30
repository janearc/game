package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/count"
)

// floorLine is a coverage floor and the file and line that said it.
type floorLine struct {
	percent int
	at      string
}

// wholePercent is all of a thing as a percentage, defined as a hundred.
const wholePercent = 100

// CoverFloor is the coverage every Go package must reach under game
// check, as a percentage: the builder's `cover N`, or the project's own
// `cover M`, whichever is higher. zero is no floor.
func (cfg Config) CoverFloor() int {
	return max(cfg.floor.percent, cfg.own.percent)
}

// CoverAllowed is the files the floor leaves out, each with the reason
// the project gave, from `cover allow FILE REASON` in .game.
func (cfg Config) CoverAllowed() map[string]string { return cfg.coverAllow }

// setCover is "cover N", a floor, or "cover allow file reason", a file
// left out of it. the floor is the builder's from the dotfile, and a
// project's own from .game, which may raise it and never lower it.
func setCover(cfg *Config, at line, _, val string) error {
	word, rest, _ := strings.Cut(val, " ")
	if word == "allow" {
		return cfg.setCoverAllow(at, strings.TrimSpace(rest))
	}
	percent, err := strconv.Atoi(val)
	if err != nil || !count.Positive(percent) || percent > wholePercent {
		return at.errorf(
			"cover wants N, a percentage from 1 to 100: " +
				"cover 80; or allow FILE REASON",
		)
	}
	where := fmt.Sprintf("%s:%d", at.path, at.at)
	here := floorLine{percent: percent, at: where}
	if at.words {
		cfg.floor = here
		return nil
	}
	if percent < cfg.floor.percent {
		return at.errorf(
			"cover %d is below the floor, cover %d at %s; "+
				"a project can raise it, never lower it",
			percent, cfg.floor.percent, cfg.floor.at,
		)
	}
	cfg.own = here
	return nil
}

// setCoverAllow is the file and reason of a cover allow. it is a project's:
// the builder's config has no files of its own to leave out.
func (cfg *Config) setCoverAllow(at line, rest string) error {
	if at.words {
		return at.errorf(
			"cover allow is a project's, in its .game; " +
				"your config has no files to leave out",
		)
	}
	file, reason, _ := strings.Cut(rest, " ")
	reason = strings.TrimSpace(reason)
	if file == "" || reason == "" {
		return at.errorf(
			"cover allow wants FILE REASON; the reason is " +
				"required, so an exemption says why",
		)
	}
	if cfg.coverAllow == nil {
		cfg.coverAllow = map[string]string{}
	}
	cfg.coverAllow[file] = reason
	return nil
}
