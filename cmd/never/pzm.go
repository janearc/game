package main

import (
	"fmt"
	"io"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/puzzlemouth/pzm"
	"github.com/janearc/puzzlemouth/verb"
)

type neverInvocation struct {
	whose, home, dir string
}

// bake hashes the configured words, or says what it would do.
func (invocation neverInvocation) bake(call pzm.Call) error {
	if call.DryRun() {
		_, err := fmt.Fprintln(call.Out(), "would bake never-words")
		return err
	}
	args := append([]string{"never"}, call.Args()...)
	code := run(args, invocation.whose, invocation.home, invocation.dir,
		call.Out(), io.Discard)
	if code != exitOK {
		return fmt.Errorf("the never-words could not be baked")
	}
	return nil
}

// neverTool declares never and its bake command to pzm.
func neverTool(invocation neverInvocation) *pzm.Tool {
	tool := pzm.New("never", "hashes never-words into game",
		pzm.Build(build), pzm.Built(built), pzm.Who(stampedWho),
		pzm.Sessions(stampedSids))
	facts := verb.NewFacts(verb.WritesTree, verb.Changes,
		verb.ThisMachine, verb.NoOne, verb.Light)
	tool.Verb("bake", verb.NoShort, "hashes the configured never-words",
		"# bake\n\nhashes the configured never-words",
		"Example_neverTool_bake", facts, invocation.bake)
	return tool
}

// runNeverPzm runs never through pzm with its input and output given.
func runNeverPzm(
	args []string,
	whose, home, dir string,
	in io.Reader,
	out, errs io.Writer,
) int {
	if len(args) == count.Nothing {
		args = []string{"bake"}
	}
	invocation := neverInvocation{whose: whose, home: home, dir: dir}
	return neverTool(invocation).RunWith(args, in, out, errs)
}
