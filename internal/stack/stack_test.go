package stack

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// repo makes a bare repository with a branch v1 holding the given files,
// and a branch fix holding a file named `FIXED` as well.
func repo(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	work := filepath.Join(dir, name+"-work")
	bare := filepath.Join(dir, name+".git")
	git := func(d string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = append(
			os.Environ(),
			"GIT_AUTHOR_NAME=t",
			"GIT_AUTHOR_EMAIL=t@t.t",
			"GIT_COMMITTER_NAME=t",
			"GIT_COMMITTER_EMAIL=t@t.t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(work, 0o755)
	git(work, "init", "-q", "-b", "v1")
	// the same reason as release_test: a container has no git identity
	// and this test commits, so it names its own.
	git(work, "config", "user.email", "test@example.invalid")
	git(work, "config", "user.name", "test")
	git(work, "config", "commit.gpgsign", "false")
	for f, body := range files {
		os.WriteFile(filepath.Join(work, f), []byte(body), 0o644)
	}
	git(work, "add", "-A")
	git(work, "commit", "-q", "-m", "one")
	git(work, "checkout", "-q", "-b", "fix")
	os.WriteFile(filepath.Join(work, "FIXED"), []byte("yes\n"), 0o644)
	git(work, "add", "-A")
	git(work, "commit", "-q", "-m", "fix")
	git(dir, "clone", "-q", "--bare", work, bare)
	return bare
}

// child is a scripted build: the script named by the member's
// behaviour file decides what the events say.
func child(dir string) *exec.Cmd {
	return exec.Command("sh", "./build.sh")
}

// every outcome a member can have, in one stack: a pass with its lint
// count, a deterministic failure, a transient failure that passes when
// tried again, a test that fails once and is flaky, a failure fixed on a
// branch, and a ref that does not exist.
func TestRun(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "counts")
	os.MkdirAll(count, 0o755)
	ok := `echo '{"step":"lint","state":"ok","count":2}'`
	once := func(step, kind string) string {
		return `f="` + count + `/$(basename "$PWD")"; if [ ! -e "$f" ]; then touch "$f"; ` +
			`echo '{"step":"` + step + `","state":"fail","kind":"` + kind + `"}'; exit 1; fi; ` + ok
	}
	urls := map[string]string{
		"good": repo(
			t,
			dir,
			"good",
			map[string]string{"build.sh": ok + "\n"},
		),
		"bad": repo(
			t,
			dir,
			"bad",
			map[string]string{
				"build.sh": `echo '{"step":"check","state":"fail","kind":"deterministic","text":"no"}'; exit 1` + "\n",
			},
		),
		"net": repo(
			t,
			dir,
			"net",
			map[string]string{
				"build.sh": once("build", "transient") + "\n",
			},
		),
		"flaky": repo(
			t,
			dir,
			"flaky",
			map[string]string{
				"build.sh": once(
					"test",
					"deterministic",
				) + "\n",
			},
		),
		"fixme": repo(
			t,
			dir,
			"fixme",
			map[string]string{
				"build.sh": `if [ -e FIXED ]; then ` + ok + `; else echo '{"step":"build","state":"fail","kind":"deterministic"}'; exit 1; fi` + "\n",
			},
		),
		"gone": repo(
			t,
			dir,
			"gone",
			map[string]string{"build.sh": ok + "\n"},
		),
	}
	members := []Member{
		{"good", "v1"},
		{"bad", "v1"},
		{"net", "v1"},
		{"flaky", "v1"},
		{"fixme", "v1"},
		{"gone", "v9"},
	}
	var slept []time.Duration
	var out bytes.Buffer
	rows, err := Run(context.Background(), Options{
		Members: members,
		URL:     func(n string) (string, error) { return urls[n], nil },
		Work:    filepath.Join(dir, "work"),
		Child:   child,
		// every classification at once: this test is about how a
		// failure is named, not about the order they are reached in.
		// the stop at the first failure is its own test below.
		Parallel: len(members),
		Tries:    3,
		Fixer:    selective{},
		Out:      &out,
		Sleep:    func(d time.Duration) { slept = append(slept, d) },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"good":  "ok",
		"bad":   "failed",
		"net":   "ok",
		"flaky": "flaky",
		"fixme": "fixed on fix",
		"gone":  "failed",
	}
	for _, r := range rows {
		if r.Result != want[r.Name] {
			t.Errorf(
				"%s: result %q, want %q (failed %q: %s)",
				r.Name,
				r.Result,
				want[r.Name],
				r.Failed,
				r.Text,
			)
		}
	}
	if rows[0].Lint != 2 {
		t.Errorf("good: lint %d, want 2", rows[0].Lint)
	}
	if rows[1].Failed != "check" || rows[1].Tries != 1 {
		t.Errorf(
			"bad: a deterministic failure is tried once "+
				"and named: %+v",
			rows[1],
		)
	}
	if rows[2].Tries != 2 {
		t.Errorf("net: want two tries, got %d", rows[2].Tries)
	}
	if rows[5].Failed != "clone" {
		t.Errorf("gone: want a clone failure, got %+v", rows[5])
	}
	if len(slept) == 0 {
		t.Error("nothing waited before trying again")
	}
	var table bytes.Buffer
	Table(&table, rows)
	if !strings.Contains(table.String(), "fixed on fix") ||
		!strings.Contains(table.String(), "failed at check") {
		t.Errorf("table:\n%s", table.String())
	}
}

// selective offers a fix only for the member that has one.
type selective struct{}

// Fix offers the fix branch to fixme and gives up on the rest.
func (selective) Fix(_ context.Context, f Failure) (string, error) {
	if f.Name == "fixme" {
		return "fix", nil
	}
	return "", nil
}

// the network's words are transient, a compiler's are not.
func TestKind(t *testing.T) {
	if Kind("go: downloading x: dial tcp: i/o timeout") != "transient" {
		t.Error("a timeout is transient")
	}
	if Kind("./main.go:3: undefined: x") != "deterministic" {
		t.Error("a compile error is deterministic")
	}
}

// TestRunStopsAtTheFirstFailure: walking one at a time, a member that
// fails ends the run where it happened. a person watching wants the
// failure on the screen, not a table after eight more builds.
func TestRunStopsAtTheFirstFailure(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	for _, name := range []string{"one", "two", "three"} {
		at := filepath.Join(work, name)
		if err := os.MkdirAll(at, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(at, ".game"), []byte("name "+name+"\n"), 0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
	child := func(dir string) *exec.Cmd {
		bad := filepath.Base(dir) == "two"
		script := `echo '{"step":"check","state":"ok"}'`
		if bad {
			script = `echo '{"step":"check","state":"fail",` +
				`"kind":"deterministic","text":"no"}'; exit 1`
		}
		return exec.Command("sh", "-c", script)
	}
	var out bytes.Buffer
	rows, err := Run(context.Background(), Options{
		Members: []Member{{Name: "one"}, {Name: "two"}, {Name: "three"}},
		Work:    work,
		Child:   child,
		Tries:   1,
		Out:     &out,
		Quiet:   true,
		Sleep:   func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want two rows, stopping at the second: %+v", rows)
	}
	if rows[1].Result != "failed" || rows[1].Member.Name != "two" {
		t.Errorf("the second member is the one that failed: %+v", rows[1])
	}
	if strings.Contains(out.String(), "three") {
		t.Error("the third member was reached after a failure")
	}
}
