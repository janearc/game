package config_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
)

// every is the count regexp has defined as all the matches there are.
const every = -1

// known is what config says it knows, read from the error it gives a
// .game holding one unknown line: the words after "the lints are".
func known(t *testing.T, line, kinds string) []string {
	t.Helper()
	home, repo := t.TempDir(), t.TempDir()
	game := filepath.Join(repo, ".game")
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(game, []byte(line+"\n"), openFile); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(home, home, repo)
	if err == nil {
		t.Fatalf("config took %q", line)
	}
	_, list, found := strings.Cut(err.Error(), "the "+kinds+" are ")
	if !found {
		t.Fatalf("config did not name the %s: %v", kinds, err)
	}
	return strings.Split(strings.TrimSpace(list), ", ")
}

// keysDeclared is every key the declaration of a file has, and the keys of
// its blocks. public is left out: it is read only to be refused, and
// the vim syntax marks it red as it does any word it does not know.
func keysDeclared() []string {
	var keys []string
	for _, setting := range config.Settings() {
		if setting.Key != "public" {
			keys = append(keys, setting.Key)
		}
		for _, inside := range setting.In {
			keys = append(keys, inside.Key)
		}
	}
	return keys
}

// the vim syntax in contrib names every key and lint rule config knows,
// so a key added here and not there fails a test instead of showing up
// red. the keys are the declaration's own, from Settings; the lint rules
// are read from config's error, which names them all.
func TestVimSyntaxKnowsEveryKey(t *testing.T) {
	text, err := os.ReadFile("../../contrib/vim/syntax/game.vim")
	if err != nil {
		t.Fatal(err)
	}
	words := map[string]bool{}
	for _, line := range strings.Split(string(text), "\n") {
		if strings.HasPrefix(line, "syn match") {
			found := regexp.MustCompile(`[a-z]+`).FindAllString(line, every)
			for _, word := range found {
				words[word] = true
			}
		}
	}
	keys := keysDeclared()
	lints := known(t, "lint bogus", "lints")
	leastKeys := int(held(t, "known-keys-least"))
	leastLints := int(held(t, "known-lints-least"))
	if len(keys) < leastKeys || len(lints) < leastLints {
		t.Fatalf("config named %d keys and %d lints", len(keys),
			len(lints))
	}
	for _, key := range append(keys, lints...) {
		if !words[key] {
			t.Errorf("contrib/vim/syntax/game.vim does not know %q", key)
		}
	}
}
