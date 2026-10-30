package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/puzzlemouth/pzm"
	pv "github.com/janearc/puzzlemouth/verb"
)

// these helpers read a verb's words and flags through pzm, as game does,
// and hand them to the code under test. a test that gives words as game
// is given them goes through the same parser the binary does.

// errPzmRefused is a call pzm would not take. it wraps what pzm said.
var errPzmRefused = errors.New("pzm refused the call")

// capturing keeps the call a verb is run with.
type capturing struct {
	got *pzm.Call
	ran *bool
}

// run keeps the call.
func (keep capturing) run(call pzm.Call) error {
	*keep.got, *keep.ran = call, true
	return nil
}

// quiet is where a test sends what a verb says when it does not read it.
var quiet io.Writer = io.Discard

// parsedCall is the call pzm makes of the verb's words, or the refusal.
func parsedCall(verbName string, args []string) (pzm.Call, error) {
	return parsedCallTo(verbName, args, quiet)
}

// parsedCallTo is parsedCall with the call's output going to to.
func parsedCallTo(verbName string, args []string,
	to io.Writer) (pzm.Call, error) {
	var got pzm.Call
	var ran bool
	tool := pzm.New("game", "test", "", "", "", "")
	found := false
	for _, described := range pzmVerbs() {
		if described.name != verbName {
			continue
		}
		found = true
		flags := make([]pzm.Flag, count.Nothing, len(described.flags))
		for _, flag := range described.flags {
			flags = append(flags, flag.declare())
		}
		tool.Verb(pv.Name(described.name), pv.NoShort, "captures the call",
			"page", "Example_capture", described.facts,
			capturing{got: &got, ran: &ran}.run, flags...)
	}
	if !found {
		return got, fmt.Errorf("no verb %s", verbName)
	}
	var out, errs bytes.Buffer
	words := normalizePzmArgs(append([]string{verbName}, args...))
	if code := tool.RunWith(words, strings.NewReader(""),
		io.MultiWriter(&out, to), &errs); code != exitOK {
		said := strings.TrimSpace(errs.String())
		return got, fmt.Errorf("%w: %s", errPzmRefused, said)
	}
	if !ran {
		said := strings.TrimSpace(out.String())
		return got, fmt.Errorf("%w: it ran nothing: %s", errPzmRefused, said)
	}
	return got, nil
}

// mustCall is parsedCall, failing the test on a refusal.
func mustCall(t *testing.T, verbName string, args []string) pzm.Call {
	t.Helper()
	call, err := parsedCall(verbName, args)
	if err != nil {
		t.Fatal(err)
	}
	return call
}

// viaPzm runs a verb on the call pzm makes of the words.
func viaPzm(t *testing.T, verbName string, do verb, here repo.Repo,
	cfg config.Config, args []string) error {
	t.Helper()
	call, err := parsedCall(verbName, args)
	if err != nil {
		return err
	}
	return do(here, cfg, call)
}

// the adapters below keep the shape of the code under test as it was
// before the words were read at the edge: words and flags in one list.

func argsAslrBy(t *testing.T, dir string, args []string, by runner,
	out io.Writer) error {
	call, err := parsedCall("aslr", args)
	if err != nil {
		return err
	}
	return aslr(dir, call.Args(), by, out)
}

func argsHardenBy(t *testing.T, dir string, args []string, by runner,
	out io.Writer) error {
	call, err := parsedCall("harden", args)
	if err != nil {
		return err
	}
	return harden(dir, call.Args(), by, out)
}

func argsCoverBy(t *testing.T, dir string, args []string, by runner,
	out io.Writer) error {
	call, err := parsedCall("cover", args)
	if err != nil {
		return err
	}
	return cover(dir, call.Args(), call.On("html"), by, out)
}

func argsLeaksBy(t *testing.T, dir string, args []string, by runner,
	out io.Writer) error {
	call, err := parsedCall("leaks", args)
	if err != nil {
		return err
	}
	return leaks(dir, call.Args(), by, out)
}

func argsMutate(t *testing.T, here repo.Repo, args []string,
	out io.Writer) error {
	call, err := parsedCallTo("mutants", args, out)
	if err != nil {
		return err
	}
	return mutantsVerb(here, config.Config{}, call)
}

// verbTo runs the verb on the call pzm makes of the words, with what it
// says going to out.
func verbTo(t *testing.T, verbName string, do verb, here repo.Repo,
	cfg config.Config, args []string, out io.Writer) error {
	t.Helper()
	call, err := parsedCallTo(verbName, args, out)
	if err != nil {
		return err
	}
	return do(here, cfg, call)
}

// stackFor is the flags build reads from the words.
func stackFor(args []string) (*stackAsk, error) {
	call, err := parsedCall("build", args)
	if err != nil {
		return nil, err
	}
	ask, err := stackAsked(call)
	if err != nil {
		return nil, err
	}
	return &ask, nil
}

func argsBuildStack(t *testing.T, here repo.Repo, cfg config.Config,
	args []string, out io.Writer, child func(string) *exec.Cmd) error {
	flags, err := stackFor(append([]string{"stack"}, args...))
	if err != nil {
		return err
	}
	return buildStack(here, cfg, *flags, out, child)
}

// declaredVerb says whether pzm is given the verb.
func declaredVerb(name string) bool {
	for _, each := range pzmVerbs() {
		if each.name == name {
			return true
		}
	}
	return false
}
