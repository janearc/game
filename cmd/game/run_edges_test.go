package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// go test is given ./... unless rapid checks are set; then go lists the
// packages, and those whose tests import rapid run apart, with the
// checks, before the rest. go test's lines are said, less the empty
// packages.
func TestTestsGivesGoWhatTheConfigAsks(t *testing.T) {
	t.Parallel()
	listing := "x/a [pgregory.net/rapid testing] []\n" +
		"x/b [testing] []\nx/c [] [testing]\n"
	for _, each := range []struct {
		checks int
		want   []string
	}{
		{count.Nothing, []string{"root: go test ./..."}},
		{count.One, []string{
			"root: go list -f {{.ImportPath}} {{.TestImports}} " +
				"{{.XTestImports}} ./...",
			"root: go test x/a -rapid.checks=1",
			"root: go test x/b x/c",
		}},
	} {
		work := cleaned(t, "root", nil)
		stand := &asked{gives: map[string]string{
			"go list": listing,
			"go test": "ok\tx/a\t0.1s\n\n?\tx/d\t[no test files]\n",
		}}
		var out bytes.Buffer
		cfg := checking(t, each.checks)
		if err := tests(work.Dir(), cfg, stand.run, &out); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(stand.said, each.want) {
			t.Errorf("checks %d: asked %q, want %q", each.checks,
				stand.said, each.want)
		}
		// go list says nothing; each go test says the one line
		tested := len(each.want) - min(each.checks, count.One)
		said := strings.Repeat("ok\tx/a\t0.1s\n", tested)
		if out.String() != said {
			t.Errorf("checks %d: said %q, want %q", each.checks,
				out.String(), said)
		}
	}
}

// when every package's tests import rapid there is no plain go test to
// run, and none is run.
func TestTestsRunsNoEmptyGroup(t *testing.T) {
	t.Parallel()
	stand := &asked{gives: map[string]string{
		"go list": "x/a [pgregory.net/rapid] []\n"}}
	checks := countHeld(t, "some-checks")
	cfg := checking(t, checks)
	err := tests(cleaned(t, "root", nil).Dir(), cfg, stand.run, &bytes.Buffer{})
	want := []string{"root: go list -f {{.ImportPath}} {{.TestImports}} " +
		"{{.XTestImports}} ./...",
		fmt.Sprintf("root: go test x/a -rapid.checks=%d", checks)}
	if err != nil || !reflect.DeepEqual(stand.said, want) {
		t.Errorf("%v: asked %q", err, stand.said)
	}
}

// go list failing stops the tests before any run; a group failing is
// the error, and the other group still runs.
func TestTestsSaysWhatFailed(t *testing.T) {
	t.Parallel()
	listing := map[string]string{
		"go list": "x/a [pgregory.net/rapid] []\nx/b [] []\n"}
	cfg := checking(t, countHeld(t, "some-checks"))
	stand := &asked{fail: "go list", gives: listing}
	err := tests(cleaned(t, "root", nil).Dir(), cfg, stand.run,
		&bytes.Buffer{})
	if err == nil || len(stand.said) != count.One {
		t.Errorf("go list failed: %v, asked %q", err, stand.said)
	}
	for _, fail := range []string{"x/a", "x/b"} {
		stand := &asked{fail: "go test " + fail, gives: listing}
		err := tests(cleaned(t, "root", nil).Dir(), cfg, stand.run,
			&bytes.Buffer{})
		// the listing and both groups
		asked := countHeld(t, "listed-and-two-groups")
		if err == nil || !strings.Contains(err.Error(), fail) ||
			len(stand.said) != asked {
			t.Errorf("%s failed: %v, asked %q", fail, err, stand.said)
		}
	}
}

// a named run runs each step where it says, a relative program from its
// own directory, without tmux's variables and with everything else, and
// stops at the first step that fails.
func TestRunNamedRunsEachStepWhereItSays(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	t.Setenv("TMUX_PANE", "%1")
	t.Setenv("GAME_TEST_KEPT", "kept")
	root := t.TempDir()
	work := cleaned(t, "root", nil)
	heard := filepath.Join(root, "heard")
	script := "#!/bin/sh\necho \"$(pwd -P)|$TMUX|$TMUX_PANE|" +
		"$GAME_TEST_KEPT|$*\" >> " + heard + "\n"
	write(t, work.Path("sub", "say.sh"), script)
	openScript := os.FileMode(0o755)
	if err := os.Chmod(work.Path("sub", "say.sh"), openScript); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(root, "elsewhere")
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(elsewhere, openDir); err != nil {
		t.Fatal(err)
	}
	second := scripted(t, "echo \"$(pwd -P)|second\" >> "+heard)
	cfg := gamed(t, "run other :: false", "run two sub :: ./say.sh a b",
		"run two "+elsewhere+" :: "+second)
	if err := runNamed(work, cfg, "two"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(heard)
	if err != nil {
		t.Fatal(err)
	}
	sub, _ := filepath.EvalSymlinks(work.Path("sub"))
	away, _ := filepath.EvalSymlinks(elsewhere)
	want := sub + "|||kept|a b\n" + away + "|second\n"
	if string(got) != want {
		t.Errorf("heard %q, want %q", got, want)
	}
	err = runNamed(work, cfg, "other")
	if err == nil || !strings.HasPrefix(err.Error(), "run other: false: ") {
		t.Errorf("a failed step: %v", err)
	}
}

// a run that is not described is refused with the ones that are.
func TestRunNamedNamesTheRunsThereAre(t *testing.T) {
	t.Parallel()
	cfg := gamed(t, "run a :: true", "run b :: true")
	err := runNamed(cleaned(t, "root", nil), cfg, "nope")
	if err == nil ||
		err.Error() != `no run called "nope"; described: a, b` {
		t.Errorf("%v", err)
	}
}

// game run wants a name: with none it says how to use it and exits 2;
// with one, it runs that, or says there is no such run and exits 1.
func TestRunVerbWantsAName(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	for _, each := range []struct {
		args []string
		want int
		said string
	}{
		{[]string{"run"}, exitUsage, "run NAME"},
		{[]string{"run", "nope"}, exitFailed, `no run called "nope"`},
	} {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := exec.Command(self, each.args...)
		child.Dir = t.TempDir()
		child.Env = append(os.Environ(), asMain+"=1", "GAME_HOME="+home,
			"HOME="+home)
		var errs bytes.Buffer
		child.Stderr = &errs
		err = child.Run()
		code := exitOK
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			code = exited.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != each.want || !strings.Contains(errs.String(), each.said) ||
			strings.Contains(errs.String(), "panic") {
			t.Errorf("game %q: exit %d, want %d; said %q", each.args,
				code, each.want, errs.String())
		}
	}
}

// Named runs stop at the first step that fails. The steps after it are
// not run. The error names the step.
func TestRunNamedStopsAtTheFirstStepThatFails(t *testing.T) {
	t.Parallel()
	tree := cleaned(t, "root", nil)
	cfg := gamed(t, "run two . :: false", "run two . :: touch later")
	err := runNamed(tree, cfg, "two")
	if err == nil || !strings.Contains(err.Error(), "run two: false") {
		t.Errorf("err %v, want the first step named", err)
	}
	later := filepath.Join(tree.Dir(), "later")
	if _, err := os.Stat(later); err == nil {
		t.Error("a step after the one that failed was run")
	}
}
