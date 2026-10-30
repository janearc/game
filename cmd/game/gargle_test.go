package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// game gargle-balls-dario says its one line.
func TestGargleSaysItsLine(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := gargle(repoAt(""), blankConfig(), &out); err != nil || out.String() != gargled+"\n" {
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
	if err := gargle(repoAt(""), blankConfig(), shut{}); !errors.Is(err, errShut) {
		t.Errorf("a refused write: %v", err)
	}
}

// The verb is declared to pzm, and its --help is pzm's page for it.
func TestGargleIsAVerb(t *testing.T) {
	t.Parallel()
	if !declaredVerb("gargle-balls-dario") {
		t.Fatal("gargle-balls-dario is no verb")
	}
	var out, errs bytes.Buffer
	code := runPzm([]string{"gargle-balls-dario", "--help"}, t.TempDir(), "", "",
		strings.NewReader(""), &out, &errs)
	if code != exitOK || !strings.Contains(out.String(), "gargle-balls-dario") {
		t.Errorf("--help: exit %d, out %q, errs %q", code, out.String(),
			errs.String())
	}
}

// anti-test: it takes no words.
func TestGargleTakesNoWords(t *testing.T) {
	t.Parallel()
	work := repoAt(t.TempDir())
	err := viaPzm(t, "gargle-balls-dario", bareVerb("gargle-balls-dario",
		aboutGargle, gargle), work, blankConfig(), []string{"twice"})
	if err == nil || !strings.Contains(err.Error(), `"twice" is one`) {
		t.Errorf("a word: %v", err)
	}
}

// Run bare, it speaks on the terminal and succeeds.
func TestGargleRunsBare(t *testing.T) {
	work := repoAt(t.TempDir())
	if err := gargle(work, blankConfig(), quiet); err != nil {
		t.Errorf("doGargle: %v", err)
	}
}
