package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/lint"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/stack"
	"github.com/janearc/puzzlemouth/pzm"
)

// verifyVerb is game verify, the child a stack member runs.
func verifyVerb(here repo.Repo, cfg config.Config, call pzm.Call) error {
	return verify(here, cfg, call.Out())
}

// told writes events as JSON lines for the parent to read.
type told struct{ enc *json.Encoder }

// step is one event: a step starting, passing or failing.
func (say told) step(step, state, kind, text string, counted int) {
	say.enc.Encode(stack.Happened(step, state, kind, text, counted))
}

// failed reports a step's failure and returns the error. a check that
// failed in go test is reported as the test step, so the parent can tell
// flaky from failing.
func (say told) failed(step string, err error) error {
	text := err.Error()
	if step == "check" && strings.Contains(text, "go test") {
		step = "test"
	}
	say.step(step, "fail", stack.Kind(text), text, noCount)
	return err
}

// verify is game verify, the child a stack member runs. it runs the
// check where there is a module, the test targets, the build where
// there are commands, and the lint count, and tells each as it happens.
//
// with lint harden in the .game, the harden layers beyond the source
// rules run over what the build left, and a finding fails the member.
func verify(here repo.Repo, cfg config.Config, out io.Writer) error {
	say := told{json.NewEncoder(out)}
	if hasGoMod(here) {
		say.step("check", "start", "", "", noCount)
		if err := check(here, cfg, run, out); err != nil {
			return say.failed("check", err)
		}
		say.step("check", "ok", "", "", noCount)
	}
	for _, target := range cfg.Targets() {
		if !testTarget(target.Name()) {
			continue
		}
		say.step("test", "start", "", target.Name(), noCount)
		err := buildTarget(here, cfg, target.Name(), out)
		if err != nil {
			return say.failed("test", err)
		}
		say.step("test", "ok", "", target.Name(), noCount)
	}
	if st, err := os.Stat(here.Path("cmd")); err == nil &&
		st.IsDir() {
		say.step("build", "start", "", "", noCount)
		err := buildBinaries(here, cfg, madeBy(here), run, out,
			stdoutColour())
		if err != nil {
			return say.failed("build", err)
		}
		say.step("build", "ok", "", "", noCount)
	}
	rules, err := lint.Describe(cfg.Lint())
	if err != nil {
		return say.failed("lint", err)
	}
	found, err := rules.Run(here.Dir())
	if err != nil {
		return say.failed("lint", err)
	}
	say.step("lint", "ok", "", "", len(found))
	if !rules.Harden() {
		return nil
	}
	say.step("harden", "start", "", "", noCount)
	beyond, err := hardenBeyondSource(here.Dir(), run)
	if err != nil {
		return say.failed("harden", err)
	}
	if hard := append(hardenOf(found), beyond...); !none(hard) {
		return say.failed("harden", firstToFix(hard))
	}
	say.step("harden", "ok", "", "", noCount)
	return nil
}

// firstToFix is the error a harden step fails with: how many there are
// to fix, and the first of them, so the stack's table has something to
// read.
func firstToFix(hard []lint.Finding) error {
	first := 0 // defined by go: a list begins at nothing
	return fmt.Errorf("harden: %d to fix; %s", len(hard), hard[first])
}

// selfVerify is the child a stack member gets: this same game, found with
// executable, running verify in the member's directory.
func selfVerify(
	executable func() (string, error),
) (func(string) *exec.Cmd, error) {
	self, err := executable()
	if err != nil {
		return nil, err
	}
	return verifier(self).child, nil
}

// verifier is game itself, at its own path, to run verify in a member.
type verifier string

// child is the verify a member gets, which runs in its directory.
func (program verifier) child(string) *exec.Cmd {
	return exec.Command(string(program), "verify")
}
