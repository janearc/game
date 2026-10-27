package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/janearc/game/internal/count"
)

// a target's steps run in order, each in its own directory, each said as
// it starts and kept in the log under a line of its own; then the log's
// last four lines and how long it took.
func TestBuildTargetRunsEachStepAndShowsTheTail(t *testing.T) {
	t.Parallel()
	work := cleaned(t, "root", map[string]string{"sub/.keep": ""})
	first := scripted(t, "pwd -P; seq 1 6")
	second := scripted(t, "echo done")
	cfg := gamed(t, "target two sub :: "+first, "target two :: "+second)
	var out bytes.Buffer
	if err := buildTargetTo(work, cfg, "two", &out); err != nil {
		t.Fatal(err)
	}
	logPath := work.Path("bin", "two.log")
	said := regexp.QuoteMeta("build two: step 1 of 2: "+first+"\nbuild two: step 2 of 2: "+
		second+"\n  5\n  6\n  == step 2, in : "+second+"\n  done\n"+
		"build two: done in ") + `\d+s` +
		regexp.QuoteMeta("; the log is "+logPath+"\n")
	if !regexp.MustCompile("^" + said + "$").MatchString(out.String()) {
		t.Errorf("said:\n%s", out.String())
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	sub, _ := filepath.EvalSymlinks(work.Path("sub"))
	want := "== step 1, in sub: " + first + "\n" + sub +
		"\n1\n2\n3\n4\n5\n6\n== step 2, in : " + second + "\ndone\n"
	if string(logged) != want {
		t.Errorf("logged %q, want %q", logged, want)
	}
}

// a step that fails stops the target, shows the log's last twelve lines,
// and says which step and where the log is.
func TestBuildTargetStopsAtAFailingStep(t *testing.T) {
	t.Parallel()
	work := cleaned(t, "root", nil)
	fails := scripted(t, "seq 1 20; exit 3")
	never := scripted(t, "echo never")
	cfg := gamed(t, "target bad :: "+fails, "target bad :: "+never)
	var out bytes.Buffer
	err := buildTargetTo(work, cfg, "bad", &out)
	logPath := work.Path("bin", "bad.log")
	if err == nil ||
		!strings.HasPrefix(err.Error(), "build bad: step 1 failed after ") ||
		!strings.HasSuffix(err.Error(), "; the log is "+logPath) {
		t.Errorf("err %v", err)
	}
	from := int(held(t, "tail-shown-from"))
	to := int(held(t, "seq-counts-to"))
	var shown []string
	for line := from; line <= to; line++ {
		shown = append(shown, "  "+strconv.Itoa(line))
	}
	want := "build bad: step 1 of 2: " + fails + "\n" +
		strings.Join(shown, "\n") + "\n"
	if out.String() != want {
		t.Errorf("said %q, want %q", out.String(), want)
	}
}

// a target that is not described is refused with those that are, and a
// log that cannot be made stops it before any step.
func TestBuildTargetRefusesWhatItCannotRun(t *testing.T) {
	t.Parallel()
	cfg := gamed(t, "target a :: true", "target b :: true")
	err := buildTargetTo(cleaned(t, "root", nil), cfg, "x",
		&bytes.Buffer{})
	if err == nil ||
		err.Error() != `no target called "x"; described: a, b` {
		t.Errorf("a stranger: %v", err)
	}
	work := cleaned(t, "root", map[string]string{"bin": "a file"})
	var out bytes.Buffer
	if err := buildTargetTo(work, cfg, "a", &out); err == nil ||
		count.Positive(out.Len()) {
		t.Errorf("no log: %v, said %q", err, out.String())
	}
}

// tail of a file that is not there says nothing.
func TestTailOfNothingIsNothing(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	nowhere := filepath.Join(t.TempDir(), "nowhere.log")
	lines := int(held(t, "tail-of-nothing"))
	tail(&out, nowhere, lines)
	if count.Positive(out.Len()) {
		t.Errorf("said %q", out.String())
	}
}
