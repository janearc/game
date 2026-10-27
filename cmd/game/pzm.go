package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/puzzlemouth/pzm"
	pv "github.com/janearc/puzzlemouth/verb"
)

type pzmFlagKind int

const (
	pzmSwitch pzmFlagKind = iota
	pzmValue
	pzmList
)

type pzmFlag struct {
	name  string
	short pv.Short
	help  string
	kind  pzmFlagKind
}

// declare gives pzm the flag's name, kind and help.
func (flag pzmFlag) declare() pzm.Flag {
	name, help := pv.Name(flag.name), pv.Help(flag.help)
	switch flag.kind {
	case pzmSwitch:
		return pzm.Switch(name, flag.short, help)
	case pzmValue:
		return pzm.Value(name, flag.short, help)
	default:
		return pzm.List(name, flag.short, help)
	}
}

// switchFlag describes a flag that takes no value.
func switchFlag(name, help string) pzmFlag {
	return pzmFlag{name: name, help: help, kind: pzmSwitch}
}

// valueFlag describes a flag that takes one word.
func valueFlag(name, help string) pzmFlag {
	return pzmFlag{name: name, help: help, kind: pzmValue}
}

// outputFlag describes the program name flag, -o.
func outputFlag() pzmFlag {
	return pzmFlag{name: "o", short: 'o',
		help: "the program's name", kind: pzmValue}
}

// listFlag describes a flag that takes comma-separated words.
func listFlag(name, help string) pzmFlag {
	return pzmFlag{name: name, help: help, kind: pzmList}
}

var (
	libraryFlags = []pzmFlag{
		outputFlag(),
		listFlag("with", "a C library to fetch, build and link"),
	}
	xcodeFlags = []pzmFlag{
		valueFlag("scheme", "the Xcode scheme"),
		valueFlag("configuration", "the Xcode build configuration"),
		valueFlag("project", "an Xcode project"),
		valueFlag("workspace", "an Xcode workspace"),
		valueFlag("destination", "the Xcode destination"),
	}
	buildXcodePzmFlags = append(append([]pzmFlag{}, xcodeFlags...),
		listFlag("with", "a Swift library to fetch"))
	nativeFlags = append(append([]pzmFlag{}, libraryFlags...),
		xcodeFlags...)
	fuzzFlags = append(append([]pzmFlag{}, libraryFlags...),
		valueFlag("time", "how long to fuzz, in seconds"),
		valueFlag("runtime", "libFuzzer's runtime"))
	mutantPzmFlags = append(append([]pzmFlag{}, fuzzFlags...),
		valueFlag("only", "a regular expression for file:function"),
		valueFlag("again", "an earlier report's surviving mutants"),
		valueFlag("report", "where to write the mutation report"),
		valueFlag("workers", "how many mutants run at once"),
		valueFlag("timeout", "how long one mutant's tests may run"),
		switchFlag("bench", "estimate the run without mutating"))
	releasePzmFlags = []pzmFlag{
		switchFlag("i-mean-it", "release over lint findings"),
		valueFlag("from", "where stack members come from"),
		switchFlag("publish", "confirm that the dist is public"),
	}
	legacyLongFlags = map[string]bool{
		"again": true, "bench": true, "cache": true,
		"configuration": true, "count": true, "destination": true,
		"dry": true, "from": true, "here": true, "i-mean-it": true,
		"look": true, "only": true, "parallel": true, "project": true,
		"publish": true, "report": true, "runtime": true,
		"scheme": true, "time": true, "timeout": true, "with": true,
		"workers":   true,
		"workspace": true,
	}
)

type pzmVerb struct {
	name       string
	help       string
	do         verb
	facts      pv.Facts
	flags      []pzmFlag
	flagsAfter bool
}

// readFacts states that a verb only reads, with the load given.
func readFacts(load pv.Load) pv.Facts {
	return pv.NewFacts(pv.WritesNothing, pv.LeavesAlone,
		pv.ThisMachine, pv.NoOne, load)
}

// readRemoteFacts states that a verb reads outside at full load.
func readRemoteFacts() pv.Facts {
	return pv.NewFacts(pv.WritesNothing, pv.LeavesAlone,
		pv.Outside, pv.NoOne, pv.FullCPU)
}

// writeFacts states that a verb changes the tree at full load.
func writeFacts(reach pv.Reach) pv.Facts {
	return pv.NewFacts(pv.WritesTree, pv.Changes, reach,
		pv.NoOne, pv.FullCPU)
}

// cleanFacts states that clean deletes build output at full load.
func cleanFacts() pv.Facts {
	return pv.NewFacts(pv.WritesTree, pv.Deletes,
		pv.ThisMachine, pv.NoOne, pv.FullCPU)
}

type pzmFactSet struct {
	readLight, readHeavy    pv.Facts
	writeLocal, writeRemote pv.Facts
}

// pzmVerbs is every game command declared once for the parser.
func pzmVerbs() []pzmVerb {
	facts := pzmFactSet{
		readLight:   readFacts(pv.Light),
		readHeavy:   readFacts(pv.FullCPU),
		writeLocal:  writeFacts(pv.ThisMachine),
		writeRemote: writeFacts(pv.Outside),
	}
	return slices.Concat(pzmBasicVerbs(facts), pzmNativeVerbs(facts),
		pzmWorkVerbs(facts), pzmRemoteVerbs(facts))
}

// pzmBasicVerbs is the commands for an ordinary local build.
func pzmBasicVerbs(facts pzmFactSet) []pzmVerb {
	return []pzmVerb{
		{name: "version", help: "says which commit this binary is",
			do: ageVerb, facts: facts.readLight},
		{name: "help-claude", help: "prints the agent help",
			do: helpVerb, facts: facts.readLight},
		{name: "check", help: "runs gofmt, vet and tests",
			do: withoutWords(check), facts: facts.readHeavy},
		{name: "mutants", help: "runs mutation tests",
			do: mutantsVerb, facts: facts.writeLocal,
			flags: mutantPzmFlags},
		{name: "build", help: "builds commands, targets or the stack",
			do: buildVerb, facts: facts.writeLocal,
			flags: []pzmFlag{
				valueFlag("from",
					"where stack members come from"),
				valueFlag("parallel", "members built at once"),
				switchFlag("here",
					"build members already in this clone"),
			}},
	}
}

// pzmNativeVerbs is the commands for native builds and audits.
func pzmNativeVerbs(facts pzmFactSet) []pzmVerb {
	return []pzmVerb{
		{name: "build-c", help: "builds a hardened C program",
			do: buildCVerb, facts: facts.writeRemote,
			flags: libraryFlags},
		{name: "build-xcode", help: "builds an Xcode scheme",
			do: buildXcodeVerb, facts: facts.writeLocal,
			flags: buildXcodePzmFlags},
		{name: "cover", help: "measures C or Xcode coverage",
			do: coverVerb, facts: facts.writeRemote,
			flags: nativeFlags},
		{name: "fuzz", help: "fuzzes a C program",
			do: fuzzVerb, facts: facts.writeRemote,
			flags: fuzzFlags},
		{name: "race", help: "checks C or Xcode code for races",
			do: raceVerb, facts: facts.writeRemote,
			flags: nativeFlags},
		{name: "aslr", help: "checks binaries for PIE",
			do: aslrVerb, facts: facts.writeRemote,
			flags: nativeFlags},
		{name: "leaks", help: "checks C, Xcode or Go code for leaks",
			do: leaksVerb, facts: facts.writeRemote,
			flags: nativeFlags},
		{name: "harden", help: "audits source, binaries and modules",
			do: hardenVerb, facts: facts.readHeavy,
			flags: xcodeFlags},
	}
}

// pzmWorkVerbs is the commands for a tree and its configured work.
func pzmWorkVerbs(facts pzmFactSet) []pzmVerb {
	return []pzmVerb{
		{name: "verify", help: "verifies one stack member",
			do: verifyVerb, facts: facts.writeLocal},
		{name: "patch", help: "lays a patch series",
			do: patchVerb, facts: facts.writeLocal,
			flags: []pzmFlag{
				valueFlag("from",
					"where stack members come from"),
				switchFlag("dry",
					"say whether each patch would land"),
			}},
		{name: "run", help: "runs a configured target",
			do: runVerb, facts: facts.writeRemote},
		{name: "lint", help: "runs the configured lint rules",
			do: lintVerb, facts: facts.readLight,
			flags: []pzmFlag{
				switchFlag("count",
					"show tallies without findings"),
				switchFlag("look", "list every finding"),
			}},
		{name: "clean", help: "removes build output and caches",
			do: cleanVerb, facts: cleanFacts(),
			flags: []pzmFlag{
				switchFlag("cache",
					"forget Go's build and test caches"),
			}},
		{name: "trees", help: "shows the road and dist",
			do: treesVerb, facts: facts.readLight},
		{name: "history", help: "shows the latest release and commits",
			do: whoVerb, facts: readRemoteFacts()},
	}
}

// pzmRemoteVerbs is the commands for releases and the other checks.
func pzmRemoteVerbs(facts pzmFactSet) []pzmVerb {
	return []pzmVerb{
		{name: "push", help: "pushes this branch to the road",
			do: withoutWords(pushRoad), facts: facts.writeRemote},
		{name: "stick", help: "claims this repository",
			do:    bareVerb("stick", aboutStick, doStick),
			facts: facts.writeRemote},
		{name: "sticky", help: "fast-forwards to the road's tip",
			do:    bareVerb("sticky", aboutSticky, doSticky),
			facts: facts.writeRemote},
		{name: "unstick", help: "releases this repository's claim",
			do:    bareVerb("unstick", aboutUnstick, doUnstick),
			facts: facts.writeRemote},
		{name: "release", help: "prepares, diffs or pushes a release",
			do: releaseVerb, facts: facts.writeRemote,
			flags: releasePzmFlags, flagsAfter: true},
		{name: "bounce", help: "checks third-person author references",
			do: bounceVerb, facts: facts.readLight},
		{name: "sweep", help: "checks a tree for what must not ship",
			do: sweepVerb, facts: facts.readLight},
		{name: "gargle-balls-dario", help: "answers the hidden verb",
			do: bareVerb(
				"gargle-balls-dario", aboutGargle, doGargle),
			facts: facts.readLight},
	}
}

type pzmInvocation struct {
	dir, whose, home string
}

type declaredPzmVerb struct {
	invocation pzmInvocation
	described  pzmVerb
}

// run loads the repository and runs one declared command.
func (declared declaredPzmVerb) run(call pzm.Call) error {
	if declared.described.facts.WritesAnything() && call.DryRun() {
		_, err := fmt.Fprintf(call.Out(), "would run game %s\n",
			declared.described.name)
		return err
	}
	here := repo.At(declared.invocation.dir, repo.Exec())
	cfg, err := config.Load(declared.invocation.whose,
		declared.invocation.home, here.Dir())
	if err != nil {
		return err
	}
	err = settle(cfg, declared.described.name, os.Stderr)
	if err != nil {
		return err
	}
	args := declared.legacyArgs(call)
	return declared.described.do(here, cfg, args)
}

// legacyArgs gives the existing command the flags pzm parsed.
func (declared declaredPzmVerb) legacyArgs(call pzm.Call) []string {
	var flags []string
	for _, flag := range declared.described.flags {
		name := flag.name
		switch flag.kind {
		case pzmSwitch:
			if call.On(name) {
				flags = append(flags, "--"+name)
			}
		case pzmValue:
			if call.Given(name) {
				flags = append(flags, "--"+name,
					call.Value(name))
			}
		case pzmList:
			items := call.List(name)
			if call.Given(name) && len(items) == count.Nothing {
				flags = append(flags, "--"+name, "")
			}
			for _, item := range items {
				flags = append(flags, "--"+name, item)
			}
		}
	}
	args := call.Args()
	if declared.described.flagsAfter {
		return append(args, flags...)
	}
	if foreignDashes[declared.described.name] {
		for at, arg := range args {
			if !strings.HasPrefix(arg, "-") {
				continue
			}
			separated := append([]string{}, args[:at]...)
			separated = append(separated, "--")
			args = append(separated, args[at:]...)
			break
		}
	}
	return append(flags, args...)
}

var foreignDashes = map[string]bool{
	"aslr": true, "cover": true, "fuzz": true, "leaks": true, "race": true,
}

// gameTool declares game and every command to pzm.
func gameTool(invocation pzmInvocation) *pzm.Tool {
	tool := pzm.New("game", "builds, checks and releases a project",
		pzm.Build(build), pzm.Built(built), pzm.Who(stampedWho),
		pzm.Sessions(stampedSids))
	for _, described := range pzmVerbs() {
		declared := declaredPzmVerb{
			invocation: invocation, described: described}
		flags := make([]pzm.Flag, count.Nothing, len(described.flags))
		for _, flag := range described.flags {
			flags = append(flags, flag.declare())
		}
		page := pv.Page("# " + described.name + "\n\n" + described.help)
		suffix := strings.ReplaceAll(described.name, "-", "")
		example := "Example_gameTool_" + suffix
		tool.Verb(pv.Name(described.name), pv.NoShort,
			pv.Help(described.help), page, pv.Example(example),
			described.facts, declared.run, flags...)
	}
	return tool
}

// normalizePzmArgs keeps legacy names pzm does not own.
func normalizePzmArgs(args []string) []string {
	out := append([]string{}, args...)
	if len(out) > count.Nothing && out[count.First] == "who" {
		out[count.First] = "history"
	}
	if len(out) > count.Nothing && out[count.First] == "--help-claude" {
		out[count.First] = "help-claude"
	}
	for at, arg := range out {
		if arg == "--" {
			break
		}
		if !strings.HasPrefix(arg, "-") ||
			strings.HasPrefix(arg, "--") {
			continue
		}
		name := strings.TrimPrefix(arg, "-")
		base, value, hasValue := strings.Cut(name, "=")
		if legacyLongFlags[base] {
			out[at] = "--" + base
			if hasValue {
				out[at] += "=" + value
			}
		}
	}
	return out
}

// runPzm runs game through pzm with its input and output given.
func runPzm(
	args []string,
	dir, whose, home string,
	in io.Reader,
	out, errs io.Writer,
) int {
	if len(args) == count.Nothing {
		usage()
		return exitUsage
	}
	invocation := pzmInvocation{dir: dir, whose: whose, home: home}
	args = normalizePzmArgs(args)
	return gameTool(invocation).RunWith(args, in, out, errs)
}
