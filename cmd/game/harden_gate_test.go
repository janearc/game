package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
)

// hardened is a config whose .game says lint harden.
func hardened(t *testing.T) config.Config { return gamed(t, "lint harden") }

// unhardened is a config whose .game says no lint at all.
func unhardened() config.Config { return blankConfig() }

// setuidGo is a Go file the harden rule finds.
const setuidGo = "package main\n\nimport \"syscall\"\n\n" +
	"func main() { syscall.Setuid(0) }\n"

// a release with lint harden is refused on a harden finding in the
// source through the lint, and --i-mean-it is the glass to break over
// it, as over any lint finding.
func TestReleaseRefusesHardenFindingsUnlessMeant(t *testing.T) {
	work := cleaned(t, "root", map[string]string{"a.go": setuidGo})
	standTools(t, "staticcheck", "semgrep")
	var out bytes.Buffer
	err := releaseChecks(work, hardened(t), false, (&asked{}).run, &out)
	if err == nil || !strings.HasPrefix(err.Error(), "refused: the lint found") {
		t.Errorf("not meant: %v", err)
	}
	if !strings.Contains(out.String(), "syscall.Setuid gives the process") {
		t.Errorf("said %q", out.String())
	}
	stand := &asked{gives: map[string]string{"semgrep": `{"results":[]}`}}
	if err := releaseChecks(work, hardened(t), true, stand.run,
		&bytes.Buffer{}); err != nil {
		t.Errorf("meant: %v", err)
	}
}

// with the source clean, the layers beyond it run after the tests, and
// a tool that is not standing refuses the release as a finding, unless
// meant, when the findings are said and the override with them. with
// every tool standing and saying nothing, the release passes. without
// lint harden, none of it is asked.
func TestReleaseRunsTheHardenLayersBeyondSource(t *testing.T) {
	work := cleaned(t, "root", map[string]string{"README.md": "# x\n"})
	standTools(t, "staticcheck")
	var out bytes.Buffer
	stand := &asked{}
	err := releaseChecks(work, hardened(t), false, stand.run, &out)
	if err == nil || !strings.HasPrefix(err.Error(), "refused: harden found") ||
		!strings.Contains(out.String(), "semgrep did not run") {
		t.Errorf("semgrep out: %v, said %q", err, out.String())
	}
	out.Reset()
	err = releaseChecks(work, hardened(t), true, stand.run, &out)
	if err != nil || !strings.Contains(out.String(), "semgrep did not run") ||
		!strings.Contains(out.String(), "harden found 1, and you meant it") {
		t.Errorf("semgrep out, meant: %v, said %q", err, out.String())
	}
	standTools(t, "staticcheck", "semgrep")
	stand = &asked{gives: map[string]string{"semgrep": `{"results":[]}`}}
	if err := releaseChecks(work, hardened(t), false, stand.run,
		&bytes.Buffer{}); err != nil {
		t.Errorf("clean: %v", err)
	}
	if len(stand.said) != count.One ||
		!strings.HasSuffix(stand.said[count.First],
			"/semgrep --json --config auto .") {
		t.Errorf("asked %q", stand.said)
	}
	stand = &asked{}
	if err := releaseChecks(work, unhardened(), false, stand.run,
		&bytes.Buffer{}); err != nil || count.Positive(len(stand.said)) {
		t.Errorf("without harden: %v, asked %q", err, stand.said)
	}
}

// verify with lint harden adds a harden step after the lint, and a
// finding fails the member there.
func TestVerifyFailsTheMemberOnHarden(t *testing.T) {
	work := cleaned(t, "root", map[string]string{"a.go": setuidGo})
	standTools(t, "staticcheck")
	var out bytes.Buffer
	err := verify(work, hardened(t), &out)
	got := events(out.String())
	if count.None(len(got)) {
		t.Fatalf("err %v, no events", err)
	}
	last := got[count.Last(len(got))]
	if err == nil || !strings.HasPrefix(last, "harden fail") {
		t.Errorf("err %v, events %v", err, got)
	}
	if !strings.Contains(err.Error(), "syscall.Setuid") {
		t.Errorf("the first finding is not named: %v", err)
	}
	out.Reset()
	err = verify(work, unhardened(), &out)
	for _, ev := range events(out.String()) {
		if strings.HasPrefix(ev, "harden") {
			t.Errorf("without harden: %v, %v", err, ev)
		}
	}
}

// hardenOf keeps the harden findings out of a lint's.
func TestHardenOfKeepsTheRule(t *testing.T) {
	t.Parallel()
	dir, _ := linted(t)
	write(t, dir+"/b.go", setuidGo)
	cfg := gamed(t, "lint width 30", "lint harden")
	linter, err := described(cfg)
	if err != nil {
		t.Fatal(err)
	}
	found, err := linter.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	hard := hardenOf(found)
	if len(hard) != count.One || hard[count.First].File() != "b.go" ||
		len(found) <= len(hard) {
		t.Errorf("harden %v of %v", hard, found)
	}
}
