package main

import (
	"fmt"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sweep"
	"github.com/janearc/puzzlemouth/pzm"
)

// sweepVerb is game sweep [REV]. it checks the history for trailers and the
// tree at HEAD or REV for the fixed patterns, the never-words and the
// author. with no never-words it never reports clean. each hit is said on
// the call's output.
func sweepVerb(here repo.Repo, cfg config.Config, call pzm.Call) error {
	args, out, never := call.Args(), call.Out(), neverWords()
	rev := "HEAD"
	if asked, _, said := verbWord(args); said {
		rev = asked
	}
	if strings.HasPrefix(rev, "-") {
		return fmt.Errorf("sweep: %q begins with a hyphen, so git "+
			"would read it as an option, not a revision; "+
			"game sweep --help says what sweep takes", rev)
	}
	hits, err := sweep.History(here, rev)
	if err != nil {
		return fmt.Errorf("the history of %s: %w", rev, err)
	}
	treeHits, err := sweep.Tree(
		here,
		rev,
		never,
		cfg.Author(),
		cfg.SweepAllow(),
		cfg.Exclude()...)
	if err != nil {
		return err
	}
	hits = append(hits, treeHits...)
	for _, hit := range hits {
		fmt.Fprintf(out, "sweep: %s\n", hit)
	}
	switch {
	case count.Positive(len(hits)):
		return fmt.Errorf("%d hit(s)", len(hits))
	case never.Empty():
		return fmt.Errorf("%s has none of the fixed "+
			"patterns, but this game was built without "+
			"never-words, so none were looked for", rev)
	}
	fmt.Fprintf(out, "sweep: %s is clean\n", rev)
	return nil
}

// what the pairing verbs do, said when one is given a word.
const (
	aboutStick = "claims this repository: a committed .sticky with your " +
		"name, pushed to the road. refused on main, or when someone " +
		"else holds it."
	aboutSticky = "follows the claim: fast-forwards this branch to the " +
		"holder's commit, now and on every build. refused over " +
		"anything divergent."
	aboutUnstick = "lets go of the claim: removes .sticky, commits, pushes."
)

// noWords reads the words after a verb that takes none. any word is an
// error that says what the verb does.
func noWords(name, about string, args []string) error {
	if word, _, said := verbWord(args); said {
		return fmt.Errorf(
			"%s takes no words, and %q is one. game %s: %s",
			name,
			word,
			name,
			about,
		)
	}
	return nil
}
