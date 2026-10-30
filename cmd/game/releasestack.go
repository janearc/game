package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
)

// releaseStack is game release stack TAG. it assembles the members, from
// their roads or their dists, into one flat commit, holds it to every
// member's lint, and keeps it at the release ref for its push.
func releaseStack(
	here repo.Repo,
	cfg config.Config,
	tag string,
	from string,
	mean bool,
	out io.Writer,
	gameCommit, gameBuilt string,
) error {
	if err := needStamp(gameCommit, gameBuilt); err != nil {
		return err
	}
	name, dist, err := distFor(cfg, "nowhere for a stack to go")
	if err != nil {
		return err
	}
	// no members is refused by release.Stack, in the same words
	remotes, err := remotesFrom(cfg, from)
	if err != nil {
		return err
	}
	if err := refuseStuck(here, "stack"); err != nil {
		return err
	}
	if err := releaseChecks(here, cfg, mean, run, out); err != nil {
		return err
	}
	opts := releaseOptions(name, dist, tag, cfg)
	// a stack keeps each member's licence; none is laid over it
	by := lintOfTheAssembly{here: here, cfg: cfg, mean: mean, out: out}
	opts = opts.Licensed("").Qualifying(by.Qualify)
	members := stackMembers(cfg, remotes)
	res, err := release.Stack(here, opts, members)
	if err != nil {
		return err
	}
	tellStacked(out, name, tag, from, dist, *res)
	return nil
}

// lintOfTheAssembly is what release.Stack asks before it keeps a stack
// (its Qualify). It holds a stack to every member's
// rules and to go's own checks before its ref is written, so a refused
// stack keeps nothing.
type lintOfTheAssembly struct {
	here repo.Repo
	cfg  config.Config
	mean bool
	out  io.Writer
}

// Qualify lints the assembled commit by the rules its members keep. Then
// it vets and tests every go module of the assembly.
func (by lintOfTheAssembly) Qualify(assembled release.Stacked) error {
	err := stackLint(by.here, by.cfg, assembled.Commit(), assembled.Games(),
		by.mean, by.out)
	if err != nil {
		return err
	}
	return stackBuilds(by.here, by.cfg, run, by.out, assembled.Commit(),
		assembled.Modules())
}

// remotesFrom is the remote of each project, from its road or from its
// dist. Any other word for where members come from is refused.
func remotesFrom(
	cfg config.Config, from string,
) (map[string]string, error) {
	switch from {
	case "road":
		return cfg.Remotes(false), nil
	case "dist":
		return cfg.Remotes(true), nil
	}
	return nil, fmt.Errorf("--from is road or dist, not %q", from)
}

// stackMembers is the stack's members with the remote each comes from.
func stackMembers(
	cfg config.Config,
	remotes map[string]string,
) []release.Member {
	stacked := cfg.Stack()
	members := make([]release.Member, count.Nothing, len(stacked))
	for _, member := range stacked {
		name := member.Name()
		taken := release.Taken(name, member.Ref(), remotes[name],
			cfg.IsTheirs(name))
		members = append(members, taken)
	}
	return members
}

// tellStacked says what the assembly produced: dist already holding
// the tree, or the commit prepared, its workspace and how to send it.
func tellStacked(
	out io.Writer,
	name, tag, from, dist string,
	res release.Stacked,
) {
	commit := repo.Short(res.Commit())
	if res.Skipped() {
		fmt.Fprintf(out,
			"stack %s %s: dist already has this tree at %s\n",
			name, tag, commit)
		return
	}
	members := len(res.Members())
	fmt.Fprintf(out,
		"stack %s %s: %d members from their %ss, prepared %s\n",
		name, tag, members, from, commit)
	if res.Work() != "" {
		members := strings.Join(res.Members(), " ")
		fmt.Fprintln(out, "  go.work: "+members)
	}
	for _, member := range res.Members() {
		if laid := res.Patched(member); count.Positive(laid) {
			word := "patches"
			if laid == count.One {
				word = "patch"
			}
			fmt.Fprintf(out, "  %s: %d %s laid\n", member, laid,
				word)
		}
	}
	fmt.Fprintf(
		out,
		"send it with: game release push %s, to dist (clean): %s\n",
		tag,
		dist,
	)
}

// prefixed adds the member's directory to each allow line.
func prefixed(name string, rules []config.Rule) []config.Rule {
	out := make([]config.Rule, count.Nothing, len(rules))
	for _, rule := range rules {
		if file, lints, said := verbWord(rule.Args()); said &&
			rule.Name() == "allow" {
			under := append([]string{name + "/" + file}, lints...)
			rule = config.Ruled(rule.Name(), under...)
		}
		out = append(out, rule)
	}
	return out
}

// memberRules is the strictest of the stack's own lint and every member's,
// with each member's allowances moved under its name. a .game that does not
// parse adds none.
func memberRules(cfg config.Config,
	games map[string]string) (lint.Lint, error) {
	sets := [][]config.Rule{cfg.Lint()}
	for name, game := range games {
		var member config.Config
		if err := member.Parse(
			strings.NewReader(game), ".game", "", false,
		); err != nil {
			continue
		}
		rules := prefixed(name, member.Lint())
		sets = append(sets, rules)
	}
	return lint.Describe(config.Strictest(sets...))
}

// lintExported exports a commit to a scratch dir and runs rules on it.
func lintExported(
	here repo.Repo,
	commit string,
	rules lint.Lint,
) ([]lint.Finding, error) {
	dir, err := os.MkdirTemp("", "game-stack-lint")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err := export(here, commit, dir, exec.Command); err != nil {
		return nil, err
	}
	return rules.Run(dir)
}

// placed is a path, asked whether it sits under a member's directory.
type placed string

// under is whether the path is under the member's directory.
func (path placed) under(name string) bool {
	return strings.HasPrefix(string(path), name+"/")
}

// notTheirs is the findings less those in a member marked theirs, which
// keeps its own style and is not held to ours, and how many those were.
// a staticcheck finding names its file at the start of its text.
func notTheirs(
	found []lint.Finding, theirs []string,
) ([]lint.Finding, int) {
	var kept []lint.Finding
	var left int
	for _, finding := range found {
		where := finding.File()
		if finding.Rule() == "staticcheck" {
			where = finding.Text()
		}
		if slices.ContainsFunc(theirs, placed(where).under) {
			left++
			continue
		}
		kept = append(kept, finding)
	}
	return kept, left
}

// stackLint holds the assembled commit to every member's rules. it exports
// and lints the tree, says every finding, and refuses the stack on any
// unless meant. a member marked theirs is held to its own rules only.
func stackLint(
	here repo.Repo,
	cfg config.Config,
	commit string,
	games map[string]string,
	mean bool,
	out io.Writer,
) error {
	rules, err := memberRules(cfg, games)
	if err != nil {
		return err
	}
	if rules.Empty() {
		return nil
	}
	found, err := lintExported(here, commit, rules)
	if err != nil {
		return err
	}
	found, left := notTheirs(found, cfg.Theirs())
	if count.Positive(left) {
		fmt.Fprintf(out, "stack lint: %d in members marked theirs, "+
			"left to their own rules\n", left)
	}
	return stackVerdict(found, mean, out)
}

// stackVerdict says each finding and the counts, and refuses the stack
// on any unless meant.
func stackVerdict(found []lint.Finding, mean bool, out io.Writer) error {
	for _, finding := range found {
		fmt.Fprintln(out, finding)
	}
	fix, look, err := lint.Verdict(found)
	if err == nil {
		fmt.Fprintln(out, "stack lint: clean, by every member's rules")
		return nil
	}
	fmt.Fprintf(out, "stack lint: %d to fix, %d to look at, by every "+
		"member's rules\n", fix, look)
	if mean {
		return nil
	}
	return fmt.Errorf(
		"the assembly is not clean by the rules its members keep; " +
			"pay it in the members, or say --i-mean-it",
	)
}
