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
	args := []string{"who", "stack", "-from=dist", "-i-mean-it", "-o", "sample"}
	want := []string{"history", "stack", "--from=dist", "--i-mean-it", "-o", "sample"}
	if got := normalizePzmArgs(args); !slices.Equal(got, want) {
		t.Fatalf("normalized %q, want %q", got, want)
	}
	args = []string{"leaks", "go", "--", "-from"}
	if got := normalizePzmArgs(args); !slices.Equal(got, args) {
		t.Fatalf("arguments after separator changed: %q", got)
	}
}

// capturedCall is the call a verb was run with.
type capturedCall struct {
	call pzm.Call
	ran  bool
}

func (captured *capturedCall) run(_ repo.Repo, _ config.Config, call pzm.Call) error {
	captured.call, captured.ran = call, true
	return nil
}

func captureTool(t *testing.T, described pzmVerb, captured *capturedCall) *pzm.Tool {
	t.Helper()
	invocation := pzmInvocation{dir: t.TempDir(), whose: t.TempDir(), home: t.TempDir()}
	return captureToolAt(t, invocation, described, captured)
}

func captureToolAt(
	t *testing.T,
	invocation pzmInvocation,
	described pzmVerb,
	captured *capturedCall,
) *pzm.Tool {
	t.Helper()
	described.do = captured.run
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

func TestPzmTakesReleaseFlagsBeforeOrAfterItsWords(t *testing.T) {
	for _, args := range [][]string{
		{"release", "stack", "v1", "-from", "road", "-i-mean-it"},
		{"release", "-from", "road", "-i-mean-it", "stack", "v1"},
		{"release", "stack", "-i-mean-it", "v1", "--from=road"},
	} {
		captured := &capturedCall{}
		described := pzmVerb{name: "release", facts: readFacts(pv.Light),
			flags: releasePzmFlags}
		tool := captureTool(t, described, captured)
		var out, errs bytes.Buffer
		code := tool.RunWith(normalizePzmArgs(args), strings.NewReader(""), &out, &errs)
		if code != exitOK {
			t.Fatalf("%q: pzm exit %d: %s", args, code, errs.String())
		}
		call := captured.call
		if !slices.Equal(call.Args(), []string{"stack", "v1"}) ||
			call.Value("from") != "road" || !call.On("i-mean-it") {
			t.Fatalf("%q: words %q, from %q, i-mean-it %v", args,
				call.Args(), call.Value("from"), call.On("i-mean-it"))
		}
	}
}

func TestPzmPropagatesConfigFailure(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config", "game", "config")
	if err := os.MkdirAll(path, os.FileMode(0o755)); err != nil {
		t.Fatal(err)
	}
	invocation := pzmInvocation{dir: t.TempDir(), whose: home, home: home}
	captured := &capturedCall{}
	described := pzmVerb{name: "check", facts: readFacts(pv.Light)}
	tool := captureToolAt(t, invocation, described, captured)
	var out, errs bytes.Buffer
	code := tool.RunWith([]string{"check"}, strings.NewReader(""), &out, &errs)
	if code != exitFailed || captured.ran {
		t.Fatalf("config failure: code %d, ran %v", code, captured.ran)
	}
}

func TestPzmPropagatesSettleFailure(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".config", "game", "config"),
		"project sample\n")
	write(t, filepath.Join(dir, ".game"), "name sample\nneeds TOKEN a test\n")
	t.Setenv("TOKEN", "")
	invocation := pzmInvocation{dir: dir, whose: home, home: home}
	captured := &capturedCall{}
	described := pzmVerb{name: "build", facts: readFacts(pv.Light)}
	tool := captureToolAt(t, invocation, described, captured)
	var out, errs bytes.Buffer
	code := tool.RunWith([]string{"build"}, strings.NewReader(""), &out, &errs)
	if code != exitFailed || captured.ran {
		t.Fatalf("settle failure: code %d, ran %v", code, captured.ran)
	}
}

// anti-tests: pzm refuses what no verb declares or what it cannot read,
// says why in its own words, and nothing runs. exit 2 is pzm's.
func TestPzmRefusesWhatNoVerbTakes(t *testing.T) {
	for args, want := range map[string]string{
		"lint --bogus":         "unknown flag: --bogus",
		"clean --cache=yes":    "a switch takes no value",
		"patch --from":         "flag needs an argument: --from",
		"lint --count --count": "it is given twice",
	} {
		var out, errs bytes.Buffer
		code := runPzm(strings.Fields(args), t.TempDir(), t.TempDir(),
			t.TempDir(), strings.NewReader(""), &out, &errs)
		if code != exitUsage || out.Len() != count.Nothing ||
			!strings.Contains(errs.String(), want) {
			t.Errorf("game %s: exit %d, out %q, errs %q", args, code,
				out.String(), errs.String())
		}
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
