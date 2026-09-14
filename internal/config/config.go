// Package config is game's settings: one per line, "key value" or
// "key = value", # comments, ~ for home. Two files, the dotfile in the
// home directory and .game in the repository, folded in that order so
// the repository's wins, with one exception: the sweep's word list is
// the dotfile's alone, so a repository cannot shorten what it may not
// say. An unknown key is an error that names the file and line, because
// a misspelt key that is silently ignored is a setting that silently
// does nothing.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is every setting game knows.
type Config struct {
	Author  string   // the name the bounce refuses in the third person
	Words   string   // the sweep's word list, one per line
	Public  string   // the public root a release goes to
	Exclude []string // path prefixes the sweep leaves alone
}

// Keys is what a file may say, and what an unknown key is measured
// against.
var Keys = []string{"author", "words", "public", "exclude"}

// Defaults is a config for a repository with no files at all.
func Defaults(home, repo string) Config {
	return Config{
		Words:  filepath.Join(home, ".config", "game", "sweep.words"),
		Public: "../" + filepath.Base(repo) + "-root.git",
	}
}

// Load is the defaults, then the dotfile, then the repository's .game.
// A missing file is skipped; a malformed one is an error.
func Load(home, repo string) (Config, error) {
	c := Defaults(home, repo)
	if err := c.fold(filepath.Join(home, ".config", "game", "config"), home, true); err != nil {
		return c, err
	}
	if err := c.fold(filepath.Join(repo, ".game"), home, false); err != nil {
		return c, err
	}
	return c, nil
}

// fold reads one file into the config. words is whether this file may
// set the word list.
func (c *Config) fold(path, home string, words bool) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	return c.Parse(f, path, home, words)
}

// Parse reads settings from a reader; path names it in errors.
func (c *Config) Parse(r interface{ Read([]byte) (int, error) }, path, home string, words bool) error {
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, err := split(line)
		if err != nil {
			return fmt.Errorf("%s:%d: %v", path, n, err)
		}
		val = expand(val, home)
		switch key {
		case "author":
			c.Author = val
		case "words":
			if words {
				c.Words = val
			}
		case "public":
			c.Public = val
		case "exclude":
			c.Exclude = append(c.Exclude, val)
		default:
			return fmt.Errorf("%s:%d: unknown key %q; the keys are %s", path, n, key, strings.Join(Keys, ", "))
		}
	}
	return sc.Err()
}

// split is "key value" or "key = value", the key lowercased, the value
// trimmed and unquoted if it was quoted; a line with no value is an
// error, since every key takes one.
func split(line string) (string, string, error) {
	var key, val string
	if i := strings.IndexAny(line, " \t="); i >= 0 {
		key, val = line[:i], strings.TrimSpace(strings.TrimLeft(line[i:], " \t="))
	} else {
		key = line
	}
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" || val == "" {
		return "", "", fmt.Errorf("want \"key value\", got %q", line)
	}
	if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
		val = val[1 : len(val)-1]
	}
	return key, val, nil
}

// expand turns a leading ~ into the home directory.
func expand(val, home string) string {
	if val == "~" {
		return home
	}
	if strings.HasPrefix(val, "~/") {
		return filepath.Join(home, val[2:])
	}
	return val
}
