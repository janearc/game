package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/patch"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/stack"
	"github.com/janearc/puzzlemouth/pzm"
)

// patchVerb is game patch. each stack member with patches is cloned fresh
// into bin/patch and its patches are laid, or with --dry checked and taken
// back. it says whether the patches still land at the refs the stack
// names, without building anything. NAME... limits it to those members.
func patchVerb(here repo.Repo, cfg config.Config, call pzm.Call) error {
	words, from := call.Args(), valueOr(call, "from", "dist")
	dry, out := call.On("dry"), call.Out()
	if err := needStack(cfg); err != nil {
		return err
	}
	remotes, err := remotesFrom(cfg, from)
	if err != nil {
		return err
	}
	url := memberURL(here, cfg, remotes, from)
	asked := map[string]bool{}
	for _, name := range words {
		asked[name] = true
	}
	var failed int
	for _, member := range membersOf(cfg) {
		if count.Positive(len(asked)) && !asked[member.Name()] {
			continue
		}
		delete(asked, member.Name())
		err := patchMember(here, cfg.Patches(), member, url, dry, out)
		if err != nil {
			fmt.Fprintln(out, err)
			failed++
		}
	}
	for name := range asked {
		return fmt.Errorf("patch: %s is not a member of this stack",
			name)
	}
	if count.Positive(failed) {
		return fmt.Errorf("patch: %d member's patches did not land",
			failed)
	}
	return nil
}

// patchMember clones one member fresh into bin/patch at its ref and lays
// its patches there, from wherever the stack keeps them, saying of each
// whether it landed.
func patchMember(here repo.Repo, dirs []string, member stack.Member,
	url func(string) (string, error), dry bool, out io.Writer) error {
	series, err := patch.Series(here.Dir(), dirs, member.Name())
	if err != nil {
		return err
	}
	if count.None(len(series)) {
		fmt.Fprintf(out, "patch %s: no patches\n", member.Name())
		return nil
	}
	from, err := url(member.Name())
	if err != nil {
		return err
	}
	dir := here.Path("bin", "patch", member.Name())
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if why, err := stack.Clone(from, member.Ref(), dir); err != nil {
		return fmt.Errorf("patch %s: clone at %s: %s", member.Name(),
			member.Ref(), why)
	}
	results, err := patch.Lay(dir, series, dry)
	landing := landed(dry, err == nil)
	for _, each := range results {
		if each.Landed() {
			file := filepath.Base(each.Patch())
			fmt.Fprintf(out, "patch %s: %s %s\n", member.Name(),
				file, landing)
		}
	}
	return err
}

// landed is what game patch says of a patch that applied: whether it
// stays, and whether the series it was in stayed with it.
func landed(dry, whole bool) string {
	switch {
	case dry && whole:
		return "would land"
	case dry:
		return "would land, but the series would not"
	case whole:
		return "lands"
	}
	return "landed, then taken back with the series"
}
