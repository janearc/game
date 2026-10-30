package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/sticky"
)

// needStack is the error for a config that describes no stack.
func needStack(cfg config.Config) error {
	if count.None(len(cfg.Stack())) {
		return fmt.Errorf("no stack described; add " +
			"\"stack NAME REF\" lines to .game")
	}
	return nil
}

// hasGoMod is whether this tree is a go module: it has a go.mod.
func hasGoMod(here repo.Repo) bool {
	_, err := os.Stat(here.Path("go.mod"))
	return err == nil
}

// isStuck is whether this working tree holds a claim.
func isStuck(here repo.Repo) (bool, error) {
	_, stuck, err := sticky.Stuck(here.Dir())
	return stuck, err
}

// refuseStuck refuses a release of what, the flat tree or the stack,
// while this working tree holds a claim: it would carry a trace of it.
func refuseStuck(here repo.Repo, what string) error {
	stuck, err := isStuck(here)
	if err != nil {
		return err
	}
	if stuck {
		return fmt.Errorf("stuck: unstick first, so the %s this "+
			"release makes carries no trace of the claim", what)
	}
	return nil
}

// check passes when gofmt has nothing to say, vet passes and the tests
// pass, and, with a cover floor, every package reaches it. its exit code
// is the tests' own, never a pipe's. it runs go and gofmt with by and
// says what it found on out.
func check(here repo.Repo, cfg config.Config, by runner,
	out io.Writer) error {
	if !hasGoMod(here) {
		fmt.Fprintln(
			out,
			"check: no go.mod, so nothing for gofmt, vet or go "+
				"test; the test targets are the check here",
		)
		return nil
	}
	gofmt := gofmtFor(here.Dir(), by, os.Stderr)
	said, err := by(here.Dir(), gofmt, "-l", ".")
	if err != nil {
		return err
	}
	if changed := strings.TrimSpace(said); changed != "" {
		return fmt.Errorf("gofmt would change: %s",
			strings.ReplaceAll(changed, "\n", " "))
	}
	if _, err := by(here.Dir(), "go", "vet", "./..."); err != nil {
		return err
	}
	if err := tests(here.Dir(), cfg, by, out); err != nil {
		return err
	}
	return coverGate(here.Dir(), cfg, by, out)
}

// tests runs go test with by and says what it said on out. with `rapid
// checks N` in the config, the packages whose tests import
// pgregory.net/rapid run apart from the rest, with that many checks of
// each property.
func tests(dir string, cfg config.Config, by runner,
	out io.Writer) error {
	withRapid := count.Positive(cfg.RapidChecks())
	plain, rapid, err := testable(dir, withRapid, by)
	if err != nil {
		return err
	}
	var failed error
	if count.Positive(len(rapid)) {
		checks := fmt.Sprintf("-rapid.checks=%d", cfg.RapidChecks())
		args := append(append([]string{"test"}, rapid...), checks)
		said, err := by(dir, "go", args...)
		failed = sayTests(out, said, err)
	}
	if count.Positive(len(plain)) {
		// both groups run whatever the first said, so every failure is
		// seen, as one go test ./... would show them.
		args := append([]string{"test"}, plain...)
		said, err := by(dir, "go", args...)
		err = sayTests(out, said, err)
		if failed == nil {
			failed = err
		}
	}
	return failed
}

// sayTests prints go test's lines, less the packages with no tests, and
// keeps its error.
func sayTests(out io.Writer, said string, err error) error {
	for _, line := range worthSaying(said) {
		fmt.Fprintln(out, line)
	}
	return err
}

// worthSaying returns go test's lines less the empty ones and the packages
// with no tests.
func worthSaying(out string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" && !strings.Contains(line, "no test files") {
			lines = append(lines, line)
		}
	}
	return lines
}

// testable lists what go test is given, ./... unless rapid is asked for.
// then the packages whose tests import rapid directly go apart from the
// rest.
func testable(dir string, rapid bool, by runner) (plain,
	withRapid []string, err error) {
	if !rapid {
		return []string{"./..."}, nil, nil
	}
	out, err := by(
		dir,
		"go", "list", "-f",
		"{{.ImportPath}} {{.TestImports}} {{.XTestImports}}",
		"./...",
	)
	if err != nil {
		return nil, nil, err
	}
	plain, withRapid = splitRapid(out)
	return plain, withRapid, nil
}

// splitRapid reads go list's lines, a package and its test imports each,
// into the packages whose tests import rapid and the rest.
func splitRapid(listing string) (plain, withRapid []string) {
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		path, imports, _ := strings.Cut(line, " ")
		switch {
		case path == "":
		case strings.Contains(imports, "pgregory.net/rapid"):
			withRapid = append(withRapid, path)
		default:
			plain = append(plain, path)
		}
	}
	return plain, withRapid
}

// gofmtFor is the gofmt of the toolchain that builds the module at dir.
// go switches to a newer toolchain when go.mod asks for one, and the
// gofmt on PATH does not follow it, so it cannot read the newer syntax.
// with no toolchain to ask, it is the gofmt on PATH.
func gofmtFor(dir string, by runner, errs io.Writer) string {
	root, err := by(dir, "go", "env", "GOROOT")
	if err != nil {
		fmt.Fprintf(errs, "check: go env GOROOT failed, so the "+
			"gofmt on PATH: %v\n", err)
		return "gofmt"
	}
	own := filepath.Join(strings.TrimSpace(root), "bin", "gofmt")
	if _, err := os.Stat(own); err != nil {
		return "gofmt"
	}
	return own
}
