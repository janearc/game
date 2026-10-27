package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/puzzlemouth/pzm"
	pv "github.com/janearc/puzzlemouth/verb"
)

func TestGameToolDeclarationsAreSound(t *testing.T) {
	invocation := pzmInvocation{dir: t.TempDir(), whose: t.TempDir(), home: t.TempDir()}
	if err := gameTool(invocation).Check(); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizePzmArgsKeepsLegacyNames(t *testing.T) {
	args := []string{"who", "stack", "-with=theft", "-i-mean-it", "-o", "sample"}
	want := []string{"history", "stack", "--with=theft", "--i-mean-it", "-o", "sample"}
	if got := normalizePzmArgs(args); !slices.Equal(got, want) {
		t.Fatalf("normalized %q, want %q", got, want)
	}
	args = []string{"leaks", "c", "--", "-with"}
	if got := normalizePzmArgs(args); !slices.Equal(got, args) {
		t.Fatalf("arguments after separator changed: %q", got)
	}
	args = []string{"--help-claude"}
	want = []string{"help-claude"}
	if got := normalizePzmArgs(args); !slices.Equal(got, want) {
		t.Fatalf("help alias became %q, want %q", got, want)
	}
}

type capturedArgs struct{ args []string }

func (captured *capturedArgs) run(_ repo.Repo, _ config.Config, args []string) error {
	captured.args = append([]string{}, args...)
	return nil
}

func captureTool(t *testing.T, described pzmVerb, captured *capturedArgs) *pzm.Tool {
	t.Helper()
	invocation := pzmInvocation{dir: t.TempDir(), whose: t.TempDir(), home: t.TempDir()}
	return captureToolAt(t, invocation, described, captured)
}

func captureToolAt(
	t *testing.T,
	invocation pzmInvocation,
	described pzmVerb,
	captured *capturedArgs,
) *pzm.Tool {
	t.Helper()
	tool := pzm.New("game", "test", "", "", "", "")
	declared := declaredPzmVerb{invocation: invocation, described: described}
	flags := make([]pzm.Flag, count.Nothing, len(described.flags))
	for _, flag := range described.flags {
		flags = append(flags, flag.declare())
	}
	tool.Verb(pv.Name(described.name), pv.NoShort, "captures arguments",
		"page", "Example_capture", described.facts, declared.run, flags...)
	return tool
}

func TestPzmForwardsLibraryAndProgramArguments(t *testing.T) {
	captured := &capturedArgs{}
	described := pzmVerb{name: "leaks", do: captured.run,
		facts: readFacts(pv.Light), flags: libraryFlags}
	tool := captureTool(t, described, captured)
	args := []string{"leaks", "-with", "theft,fox", "-o", "sample",
		"source.c", "--", "-v"}
	var out, errs bytes.Buffer
	if code := tool.RunWith(normalizePzmArgs(args), strings.NewReader(""), &out, &errs); code != exitOK {
		t.Fatalf("pzm exit %d: %s", code, errs.String())
	}
	want := []string{"--o", "sample", "--with", "theft", "--with", "fox",
		"source.c", "--", "-v"}
	if !slices.Equal(captured.args, want) {
		t.Fatalf("forwarded %q, want %q", captured.args, want)
	}
}

func TestPzmPreservesAnExplicitlyEmptyList(t *testing.T) {
	captured := &capturedArgs{}
	described := pzmVerb{name: "leaks", do: captured.run,
		facts: readFacts(pv.Light), flags: libraryFlags}
	tool := captureTool(t, described, captured)
	args := []string{"leaks", "-with=", "source.c"}
	var out, errs bytes.Buffer
	if code := tool.RunWith(normalizePzmArgs(args), strings.NewReader(""), &out, &errs); code != exitOK {
		t.Fatalf("pzm exit %d: %s", code, errs.String())
	}
	want := []string{"--with", "", "source.c"}
	if !slices.Equal(captured.args, want) {
		t.Fatalf("forwarded %q, want %q", captured.args, want)
	}
}

func TestPzmPutsReleaseFlagsAfterItsWords(t *testing.T) {
	captured := &capturedArgs{}
	described := pzmVerb{name: "release", do: captured.run,
		facts: readFacts(pv.Light), flags: releasePzmFlags, flagsAfter: true}
	tool := captureTool(t, described, captured)
	args := []string{"release", "stack", "v1", "-from", "road", "-i-mean-it"}
	var out, errs bytes.Buffer
	if code := tool.RunWith(normalizePzmArgs(args), strings.NewReader(""), &out, &errs); code != exitOK {
		t.Fatalf("pzm exit %d: %s", code, errs.String())
	}
	want := []string{"stack", "v1", "--i-mean-it", "--from", "road"}
	if !slices.Equal(captured.args, want) {
		t.Fatalf("forwarded %q, want %q", captured.args, want)
	}
}

func TestPzmPropagatesConfigFailure(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config", "game", "config")
	if err := os.MkdirAll(path, os.FileMode(0o755)); err != nil {
		t.Fatal(err)
	}
	invocation := pzmInvocation{dir: t.TempDir(), whose: home, home: home}
	captured := &capturedArgs{}
	described := pzmVerb{name: "check", do: captured.run,
		facts: readFacts(pv.Light)}
	tool := captureToolAt(t, invocation, described, captured)
	var out, errs bytes.Buffer
	code := tool.RunWith([]string{"check"}, strings.NewReader(""), &out, &errs)
	if code != exitFailed || captured.args != nil {
		t.Fatalf("config failure: code %d, args %q", code, captured.args)
	}
}

func TestPzmPropagatesSettleFailure(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".config", "game", "config"),
		"project sample\n")
	write(t, filepath.Join(dir, ".game"), "name sample\nneeds TOKEN a test\n")
	t.Setenv("TOKEN", "")
	invocation := pzmInvocation{dir: dir, whose: home, home: home}
	captured := &capturedArgs{}
	described := pzmVerb{name: "build", do: captured.run,
		facts: readFacts(pv.Light)}
	tool := captureToolAt(t, invocation, described, captured)
	var out, errs bytes.Buffer
	code := tool.RunWith([]string{"build"}, strings.NewReader(""), &out, &errs)
	if code != exitFailed || captured.args != nil {
		t.Fatalf("settle failure: code %d, args %q", code, captured.args)
	}
}

func TestPzmBuildXcodeAcceptsLibrary(t *testing.T) {
	captured := &capturedArgs{}
	described := pzmVerb{name: "build-xcode", do: captured.run,
		facts: readFacts(pv.Light), flags: buildXcodePzmFlags}
	tool := captureTool(t, described, captured)
	args := []string{"build-xcode", "-scheme", "Sample", "-with", "fox"}
	var out, errs bytes.Buffer
	code := tool.RunWith(normalizePzmArgs(args), strings.NewReader(""), &out, &errs)
	if code != exitOK {
		t.Fatalf("pzm exit %d: %s", code, errs.String())
	}
	want := []string{"--scheme", "Sample", "--with", "fox"}
	if !slices.Equal(captured.args, want) {
		t.Fatalf("forwarded %q, want %q", captured.args, want)
	}
}

func TestHistoryReachesOutsideMachine(t *testing.T) {
	found := false
	for _, described := range pzmVerbs() {
		if described.name != "history" {
			continue
		}
		found = true
		if !strings.Contains(described.facts.String(), "outside") {
			t.Fatalf("history facts are %q", described.facts.String())
		}
	}
	if !found {
		t.Fatal("history is not declared")
	}
}

func TestPzmDryRunWritesNothing(t *testing.T) {
	for _, described := range pzmVerbs() {
		if !described.facts.WritesAnything() {
			continue
		}
		args := []string{described.name, "--dry-run"}
		var out, errs bytes.Buffer
		code := runPzm(args, t.TempDir(), t.TempDir(), t.TempDir(),
			strings.NewReader(""), &out, &errs)
		if code != exitOK || out.String() != "would run game "+described.name+"\n" {
			t.Fatalf("%s dry run: code %d, out %q, errs %q",
				described.name, code, out.String(), errs.String())
		}
	}
}
