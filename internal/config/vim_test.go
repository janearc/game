package config_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
)

// the vim syntax in contrib names every key and lint rule config knows, so
// a key added here and not there fails a test instead of showing up red.
func TestVimSyntaxKnowsEveryKey(t *testing.T) {
	b, err := os.ReadFile("../../contrib/vim/syntax/game.vim")
	if err != nil {
		t.Fatal(err)
	}
	words := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "syn match") {
			for _, w := range regexp.MustCompile(`[a-z]+`).FindAllString(line, -1) {
				words[w] = true
			}
		}
	}
	for _, k := range append(append([]string{}, config.Keys...), config.Rules...) {
		if !words[k] {
			t.Errorf("contrib/vim/syntax/game.vim does not know %q", k)
		}
	}
}
