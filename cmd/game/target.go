package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/puzzlemouth/pzm"
)

// runVerb is game run NAME.
func runVerb(here repo.Repo, cfg config.Config, call pzm.Call) error {
	name, _, said := verbWord(call.Args())
	if !said {
		fmt.Fprintln(os.Stderr, "game: run wants a name: game run NAME")
		os.Exit(exitUsage)
	}
	return runNamed(here, cfg, name)
}

// ownerOnly is the mode a settings file is written with: the owner
// alone reads and writes it, defined as 0600 in sys/stat.h's bits.
const ownerOnly = 0o600

// nicest is the nice level a target's steps run at: the lowest
// priority nice defines, as its own docs give it.
const nicest = "19"

// resolved is where a step runs and what it runs. Relative directories
// are taken from root. Programs written with a slash and not absolute
// are taken from the directory.
func resolved(root string, step config.Step) (string, string, []string) {
	dir := step.Dir()
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	prog, rest, _ := verbWord(step.Args())
	if strings.Contains(prog, "/") && !filepath.IsAbs(prog) {
		prog = filepath.Join(dir, prog)
	}
	return dir, prog, rest
}

// runStep runs one step in its directory, under nice. output is in the log.
// a relative path is joined to the step's directory first.
func runStep(root string, step config.Step, logf io.Writer) error {
	dir, prog, rest := resolved(root, step)
	args := append([]string{"-n", nicest, prog}, rest...)
	cmd := exec.Command("nice", args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = logf, logf
	return cmd.Run()
}

// targetNamed is the target of that name in the config, or an error
// naming the ones described.
func targetNamed(cfg config.Config, name string) (*config.Target, error) {
	found, err := named(cfg.Targets(), name)
	if err != nil {
		return nil, fmt.Errorf("no target called %q; described: %s",
			name, err)
	}
	return found, nil
}

// named is the target of that name among those described, or an error
// naming the ones that are.
func named(targets []config.Target, name string) (*config.Target, error) {
	for i := range targets {
		if targets[i].Name() == name {
			return &targets[i], nil
		}
	}
	names := make([]string, count.Nothing, len(targets))
	for _, other := range targets {
		names = append(names, other.Name())
	}
	return nil, errors.New(strings.Join(names, ", "))
}

// buildTarget is game build NAME. it runs the target's steps in order, each
// in its own directory, keeps the output in bin/NAME.log and shows its
// steps and the log's tail on out.
func buildTarget(here repo.Repo, cfg config.Config, name string,
	out io.Writer) error {
	target, err := targetNamed(cfg, name)
	if err != nil {
		return err
	}
	os.MkdirAll(here.Path("bin"), folderMode)
	logPath := here.Path("bin", name+".log")
	logf, err := os.Create(logPath)
	if err != nil {
		return err
	}
	defer logf.Close()
	start := time.Now()
	steps := target.Steps()
	for i, step := range steps {
		said := strings.Join(step.Args(), " ")
		at := count.Next(i)
		fmt.Fprintf(out, "build %s: step %d of %d: %s\n", name, at,
			len(steps), said)
		fmt.Fprintf(logf, "== step %d, in %s: %s\n", at, step.Dir(),
			said)
		if err := runStep(here.Dir(), step, logf); err != nil {
			tail(out, logPath, onFailure)
			return fmt.Errorf(
				"build %s: step %d failed after %s; "+
					"the log is %s",
				name,
				at,
				time.Since(start).Round(time.Second),
				logPath,
			)
		}
	}
	tail(out, logPath, onSuccess)
	fmt.Fprintf(
		out,
		"build %s: done in %s; the log is %s\n",
		name,
		time.Since(start).Round(time.Second),
		logPath,
	)
	return nil
}

// tail writes the last n lines of a file to out, after a build.
func tail(out io.Writer, path string, last int) {
	body, err := os.ReadFile(path)
	if err != nil {
		return
	}
	text := strings.TrimRight(string(body), "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > last {
		lines = lines[len(lines)-last:]
	}
	for _, line := range lines {
		fmt.Fprintln(out, "  "+line)
	}
}

// how much of a target's log is shown: twelve lines after a failure
// and four after a success, as picked here.
const (
	onFailure = 12 // defined here: enough to see why
	onSuccess = 4  // defined here: enough to see it ended
)

// runNamed runs a named run in your terminal, without tmux's variables.
func runNamed(here repo.Repo, cfg config.Config, name string) error {
	run, err := named(cfg.Runs(), name)
	if err != nil {
		return fmt.Errorf("no run called %q; described: %s", name, err)
	}
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMUX=") ||
			strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		env = append(env, kv)
	}
	for _, step := range run.Steps() {
		dir, prog, rest := resolved(here.Dir(), step)
		cmd := exec.Command(prog, rest...)
		cmd.Dir, cmd.Env = dir, env
		cmd.Stdin, cmd.Stdout = os.Stdin, os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			said := strings.Join(step.Args(), " ")
			return fmt.Errorf("run %s: %s: %w", name, said, err)
		}
	}
	return nil
}
