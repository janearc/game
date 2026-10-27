package main

import (
	"bytes"
	"errors"
	"flag"
	"regexp"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// the page fits in what a hook delivers whole, names only verbs that
// exist, and keeps to the width the lint asks of everything else.
func TestHelpClaudeFitsAndNamesRealVerbs(t *testing.T) {
	t.Parallel()
	budget := int(held(t, "hook-budget-chars"))
	if size := len(helpClaude); size > budget {
		t.Errorf("help-claude is %d characters; a hook delivers %d", size,
			budget)
	}
	named := regexp.MustCompile(`(?m)^  game (?P<verb>[a-z-]+)`)
	verb := named.SubexpIndex("verb")
	for _, match := range named.FindAllStringSubmatch(helpClaude, every) {
		if verbs[match[verb]] == nil {
			t.Errorf("help-claude names game %s, which is no verb",
				match[verb])
		}
	}
	width := int(held(t, "lint-width"))
	for place, line := range strings.Split(helpClaude, "\n") {
		if len(line) > width {
			t.Errorf("line %d is %d wide: %q", count.Next(place),
				len(line), line)
		}
	}
	if strings.ContainsAny(helpClaude, "—–") {
		t.Error("help-claude has a dash the house does not write")
	}
}

// --help on a verb that takes no words prints what it does and stops
// it; a word it does not take is refused the same way.
func TestBareVerbsTakeHelpAndNothingElse(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := bare(&out, "stick", aboutStick, []string{"--help"})
	if !errors.Is(err, flag.ErrHelp) ||
		!strings.Contains(out.String(), "game stick: claims this") {
		t.Errorf("--help: err %v, out %q", err, out.String())
	}
	if err := bare(&out, "sticky", aboutSticky, nil); err != nil {
		t.Errorf("no words: %v", err)
	}
	err = bare(&out, "unstick", aboutUnstick, []string{"please"})
	if err == nil || !strings.Contains(err.Error(), `"please" is one`) {
		t.Errorf("a word: %v", err)
	}
}
