package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/puzzlemouth/pzm"
)

// cleanVerb is game clean [--cache].
func cleanVerb(here repo.Repo, cfg config.Config, call pzm.Call) error {
	return clean(here, cfg, call.On("cache"), run, call.Out())
}

// clean is go clean, which knows what go build left behind, plus bin/,
// which go does not know about because it is ours. it runs go with by and
// says what went on out.
func clean(here repo.Repo, cfg config.Config, cache bool, by runner,
	out io.Writer) error {
	for _, dir := range cleanable(here) {
		if _, err := by(dir, "go", "clean", "./..."); err != nil {
			return err
		}
		if !cache {
			continue
		}
		if _, err := by(
			dir, "go", "clean", "-cache", "-testcache",
		); err != nil {
			return err
		}
	}
	var gone int
	dirs := append([]string{here.Dir()}, members(here, cfg)...)
	for _, dir := range dirs {
		swept, err := sweepBin(here, filepath.Join(dir, "bin"))
		if err != nil {
			return err
		}
		gone += swept
	}
	if err := sweepSum(here, out); err != nil {
		return err
	}
	fmt.Fprintf(
		out,
		"clean: %d files of build output away%s\n",
		gone,
		map[bool]string{
			true:  ", caches forgotten",
			false: "",
		}[cache],
	)
	return nil
}

// sweepSum removes the go.work.sum the build wrote. a tracked one remains.
func sweepSum(here repo.Repo, out io.Writer) error {
	sum := here.Path("go.work.sum")
	if _, err := os.Stat(sum); err != nil {
		return nil
	}
	tracked, err := here.Git("ls-files", "--", "go.work.sum")
	if err != nil {
		return err
	}
	if tracked != "" {
		fmt.Fprintln(out, "clean: go.work.sum is tracked; left")
		return nil
	}
	if err := os.Remove(sum); err != nil {
		return err
	}
	fmt.Fprintln(
		out, "clean: go.work.sum away; the build writes it again",
	)
	return nil
}

// sweepBin removes build files from one bin/ and returns the count.
// it checks git first to avoid deleting source or tracked files.
// bin/ is removed only if untracked and empty after.
func sweepBin(here repo.Repo, bin string) (int, error) {
	if _, err := os.Stat(bin); err != nil {
		return count.Nothing, nil
	}
	entries, err := os.ReadDir(bin)
	if err != nil {
		return count.Nothing, err
	}
	var gone int
	for _, entry := range entries {
		at := filepath.Join(bin, entry.Name())
		tracked, err := here.Git("ls-files", "--", at)
		if err != nil {
			return gone, err
		}
		if strings.TrimSpace(tracked) != "" {
			continue
		}
		if err := os.RemoveAll(at); err != nil {
			return gone, err
		}
		gone++
	}
	left, err := os.ReadDir(bin)
	if err != nil {
		return gone, fmt.Errorf("%s, swept: %w", bin, err)
	}
	if count.None(len(left)) {
		if err := os.Remove(bin); err != nil {
			return gone, err
		}
	}
	return gone, nil
}

// cleanable returns directories go clean can work in.
// a stack checkout has no module at its root, only a go.work.
// go clean ./... fails there, so game cleans each member instead.
// a tree with no go has nothing for go to clean.
func cleanable(here repo.Repo) []string {
	if hasGoMod(here) {
		return []string{here.Dir()}
	}
	work, err := os.ReadFile(here.Path("go.work"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(work), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "use ")
		line = strings.Trim(line, "()")
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "./") {
			continue
		}
		out = append(out, here.Path(line))
	}
	return out
}

// members is the stack members this repository declares, as directories,
// so their build output is cleaned with the root's.
func members(here repo.Repo, cfg config.Config) []string {
	var out []string
	for _, member := range cfg.Stack() {
		dir := here.Path(member.Name())
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		out = append(out, dir)
	}
	return out
}
