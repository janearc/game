package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// game gargle-balls-dario says its one line.
func TestGargleSaysItsLine(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := gargle(&out); err != nil || out.String() != gargled+"\n" {
		t.Errorf("gargle: err %v, said %q", err, out.String())
	}
}

// shut is a terminal that takes nothing.
type shut struct{}

// errShut is what shut says to every write.
var errShut = errors.New("the terminal is shut")

// Write refuses every write.
func (shut) Write([]byte) (int, error) { return count.Nothing, errShut }

// anti-test: a terminal that refuses the line is said, not swallowed.
func TestGargleSaysWhenItCouldNotSpeak(t *testing.T) {
	t.Parallel()
	if err := gargle(shut{}); !errors.Is(err, errShut) {
		t.Errorf("a refused write: %v", err)
	}
}

// The verb is in game's table, and its --help is what it does.
func TestGargleIsAVerb(t *testing.T) {
	t.Parallel()
	if verbs["gargle-balls-dario"] == nil {
		t.Fatal("gargle-balls-dario is no verb")
	}
	var out bytes.Buffer
	err := bare(&out, "gargle-balls-dario", aboutGargle, []string{"--help"})
	if !errors.Is(err, flag.ErrHelp) ||
		!strings.Contains(out.String(), "says one line about dario") {
		t.Errorf("--help: err %v, out %q", err, out.String())
	}
}

// anti-test: it takes no words.
func TestGargleTakesNoWords(t *testing.T) {
	t.Parallel()
	work := repoAt(t.TempDir())
	err := verbs["gargle-balls-dario"](work, blankConfig(),
		[]string{"twice"})
	if err == nil || !strings.Contains(err.Error(), `"twice" is one`) {
		t.Errorf("a word: %v", err)
	}
}

// Run bare, it speaks on the terminal and succeeds.
func TestGargleRunsBare(t *testing.T) {
	work := repoAt(t.TempDir())
	if err := doGargle(work, blankConfig()); err != nil {
		t.Errorf("doGargle: %v", err)
	}
}
