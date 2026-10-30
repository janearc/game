package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// a module is checked by gofmt, then vet, then its tests, each stopping
// the rest when it fails; with no module there is nothing to check.
func TestCheckAsksGofmtVetAndTestsInTurn(t *testing.T) {
	t.Parallel()
	module := map[string]string{"go.mod": "module x\n"}
	all := []string{"root: go env GOROOT", "root: gofmt -l .",
		"root: go vet ./...", "root: go test ./..."}
	for _, each := range []struct {
		name  string
		stand *asked
		stop  string
		err   string
	}{
		{"all pass", &asked{}, "", ""},
		{"gofmt has words", &asked{gives: map[string]string{
			"gofmt": "a.go\nb.go\n"}}, "root: gofmt -l .",
			"gofmt would change: a.go b.go"},
		{"gofmt fails", &asked{fail: "gofmt"}, "root: gofmt -l .",
			"no: root: gofmt"},
		{"vet fails", &asked{fail: "go vet"}, "root: go vet ./...",
			"no: root: go vet"},
		{"tests fail", &asked{fail: "go test"}, "root: go test ./...",
			"no: root: go test"},
	} {
		var out bytes.Buffer
		err := check(cleaned(t, "root", module), blankConfig(),
			each.stand.run, &out)
		if each.err == "" && err != nil ||
			each.err != "" && (err == nil ||
				!strings.HasPrefix(err.Error(), each.err)) {
			t.Errorf("%s: %v, want %q", each.name, err, each.err)
		}
		want := all
		if each.stop != "" {
			want = all[:count.Next(slices.Index(all, each.stop))]
		}
		if !reflect.DeepEqual(each.stand.said, want) {
			t.Errorf("%s: asked %q, want %q", each.name, each.stand.said,
				want)
		}
	}
	stand := &asked{}
	var out bytes.Buffer
	if err := check(cleaned(t, "root", nil), blankConfig(),
		stand.run, &out); err != nil || stand.said != nil ||
		!strings.HasPrefix(out.String(), "check: no go.mod,") {
		t.Errorf("no module: %v, asked %q, said %q", err, stand.said,
			out.String())
	}
}

// when go cannot say its root, the gofmt on PATH is used, and said.
func TestGofmtWithNoRootIsPathsAndSaysSo(t *testing.T) {
	t.Parallel()
	var errs bytes.Buffer
	got := gofmtFor(t.TempDir(), (&asked{fail: "go env"}).run, &errs)
	if got != "gofmt" ||
		!strings.HasPrefix(errs.String(), "check: go env GOROOT failed") {
		t.Errorf("%q, said %q", got, errs.String())
	}
}

// a test target is test, or test- and more.
func TestATestTargetIsNamedSo(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"test": true, "test-race": true, "tests": false, "build": false,
		"a-test": false,
	} {
		if testTarget(name) != want {
			t.Errorf("%q: %v", name, !want)
		}
	}
}

// stepTo is a target line whose step notes its name in the ran file.
func stepTo(t *testing.T, ran, name string) string {
	t.Helper()
	script := scripted(t, "echo "+name+" >> "+ran)
	return "target " + name + " :: " + script
}

// a release needs its settings set, the lint at nothing unless meant,
// the check where there is a module, and each test target passed.
func TestReleaseChecksEachInTurn(t *testing.T) {
	t.Parallel()
	ran := filepath.Join(t.TempDir(), "ran")
	cfg := gamed(t, stepTo(t, ran, "test"), stepTo(t, ran, "build"),
		stepTo(t, ran, "test-more"))
	work := cleaned(t, "root", map[string]string{"go.mod": "module x\n"})
	stand := &asked{}
	if err := releaseChecks(work, cfg, false, stand.run,
		&bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(ran)
	asks := int(held(t, "release-check-asks"))
	if string(got) != "test\ntest-more\n" || len(stand.said) != asks {
		t.Errorf("ran %q, asked %q", got, stand.said)
	}
	bare := cleaned(t, "bare", nil)
	stand = &asked{}
	if err := releaseChecks(bare, blankConfig(), false, stand.run,
		&bytes.Buffer{}); err != nil || stand.said != nil {
		t.Errorf("no module: %v, asked %q", err, stand.said)
	}
	stand = &asked{fail: "go vet"}
	if err := releaseChecks(work, cfg, false, stand.run,
		&bytes.Buffer{}); err == nil {
		t.Error("a check that failed was passed")
	}
	if err := releaseChecks(bare, failingTest(t), false, (&asked{}).run,
		&bytes.Buffer{}); err == nil {
		t.Error("a test target that failed was passed")
	}
}

// a needed setting unset is said and refused, and so is any lint at all
// unless meant.
func TestReleaseChecksRefuseWhatIsMissingOrFound(t *testing.T) {
	t.Parallel()
	needs := gamed(t, "needs A_URL where a lives")
	var out bytes.Buffer
	err := releaseChecks(cleaned(t, "root", nil), needs, true,
		(&asked{}).run, &out)
	if err == nil || !strings.HasPrefix(err.Error(),
		"refused: set the needed settings") ||
		out.String() != "release: A_URL is needed: where a lives\n" {
		t.Errorf("a setting unset: %v, said %q", err, out.String())
	}
	dir, cfg := linted(t)
	work := repoAt(dir)
	out.Reset()
	err = releaseChecks(work, cfg, false, (&asked{}).run, &out)
	found := int(held(t, "linted-wide-lines"))
	if err == nil || !strings.HasPrefix(err.Error(),
		"refused: the lint found 3;") ||
		strings.Count(out.String(), "a.go:") != found {
		t.Errorf("lint found: %v, said %q", err, out.String())
	}
	if err := releaseChecks(work, cfg, true, (&asked{}).run,
		&bytes.Buffer{}); err != nil {
		t.Errorf("lint found, meant: %v", err)
	}
	one := gamed(t, "lint width 50")
	err = releaseChecks(work, one, false, (&asked{}).run, &bytes.Buffer{})
	if err == nil || !strings.HasPrefix(err.Error(),
		"refused: the lint found 1;") {
		t.Errorf("one found: %v", err)
	}
	bad := gamed(t, "lint width wide")
	if err := releaseChecks(work, bad, true, (&asked{}).run,
		&bytes.Buffer{}); err == nil {
		t.Error("lint rules it cannot read were passed")
	}
	missing := filepath.Join(cleaned(t, "x", nil).Dir(), "nowhere")
	gone := repoAt(missing)
	if err := releaseChecks(gone, cfg, true, (&asked{}).run,
		&bytes.Buffer{}); err == nil {
		t.Error("a tree it cannot lint was passed")
	}
}
