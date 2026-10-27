package main

import (
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/lint"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
)

// blankConfig is a config with nothing set.
func blankConfig() config.Config {
	var cfg config.Config
	return cfg
}

// configOf is the config a dotfile and a .game say, in lines, the way
// Load reads them, without writing either. the dotfile may hold project
// blocks and a .game may hold a stack, needs and lint.
func configOf(t *testing.T, dotfile, game []string) config.Config {
	t.Helper()
	var cfg config.Config
	for _, one := range []struct {
		lines  []string
		file   string
		dotted bool
	}{{dotfile, "(dotfile)", true}, {game, "(.game)", false}} {
		text := strings.Join(one.lines, "\n") + "\n"
		err := cfg.Parse(strings.NewReader(text), one.file, "/h", one.dotted)
		if err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

// dotted is the config a dotfile of these lines says.
func dotted(t *testing.T, lines ...string) config.Config {
	t.Helper()
	return configOf(t, lines, nil)
}

// gamed is the config a .game of these lines says.
func gamed(t *testing.T, lines ...string) config.Config {
	t.Helper()
	return configOf(t, nil, lines)
}

// loadedAs is the config Load makes for a tree whose .game names it
// name and says the lines, with a dotfile of the other lines.
func loadedAs(t *testing.T, name string, dotfile, game []string) config.Config {
	t.Helper()
	whose, tree := t.TempDir(), t.TempDir()
	at := filepath.Join(whose, ".config", "game")
	if err := os.MkdirAll(at, 0o755); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(dotfile, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(at, "config"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := game
	if name != "" {
		lines = append([]string{"name " + name}, game...)
	}
	text = strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(tree, ".game"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(whose, whose, tree)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// scripted is a script that runs the body with sh, for a target line
// whose command has words with spaces in them, which a line cannot hold.
func scripted(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "step.sh")
	text := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// checking is the config that says how many checks a property runs; none
// is a config that says nothing of it.
func checking(t *testing.T, checks int) config.Config {
	t.Helper()
	if !count.Positive(checks) {
		return blankConfig()
	}
	return dotted(t, "rapid checks "+strconv.Itoa(checks))
}

// block is the lines of one project block of a dotfile: the road and the
// dist when it has them.
func block(name, road, dist string) []string {
	lines := []string{"project " + name}
	if road != "" {
		lines = append(lines, "\troad "+road)
	}
	if dist != "" {
		lines = append(lines, "\tdist "+dist)
	}
	return lines
}

// lineOf is the line a lint finding is at, as its one line says it.
func lineOf(found lint.Finding) int {
	_, after, _ := strings.Cut(found.String(), ": ")
	place, _, _ := strings.Cut(after, ": ")
	at := strings.LastIndex(place, ":")
	line, err := strconv.Atoi(place[count.Next(at):])
	if err != nil {
		return count.Nothing
	}
	return line
}
