package stack_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/stack"
)

// bareRepo makes a bare repository with a branch v1 holding the given
// files, and a branch fix holding a file named FIXED as well, and
// answers where it is.
func bareRepo(
	t *testing.T,
	dir, name string,
	files map[string]string,
) string {
	t.Helper()
	work := filepath.Join(dir, name+"-work")
	bare := filepath.Join(dir, name+".git")
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(work, openDir); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "init", "-q", "-b", "v1")
	// a container has no git identity and this test commits, so it
	// names its own.
	gitIn(t, work, "config", "user.email", "test@example.invalid")
	gitIn(t, work, "config", "user.name", "test")
	gitIn(t, work, "config", "commit.gpgsign", "false")
	openFile := os.FileMode(0o644)
	for name, body := range files {
		path := filepath.Join(work, name)
		if err := os.WriteFile(path, []byte(body), openFile); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, work, "add", "-A")
	gitIn(t, work, "commit", "-q", "-m", "one")
	gitIn(t, work, "checkout", "-q", "-b", "fix")
	fixed := filepath.Join(work, "FIXED")
	if err := os.WriteFile(fixed, []byte("yes\n"), openFile); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "-A")
	gitIn(t, work, "commit", "-q", "-m", "fix")
	gitIn(t, dir, "clone", "-q", "--bare", work, bare)
	return bare
}

// gitIn runs git in dir, outside the package under test, stopping the
// test on an error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
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

// child is a scripted build: the script named by the member's
// behaviour file decides what the events say.
func child(string) *exec.Cmd {
	return exec.Command("sh", "./build.sh")
}

// roads answers where a member lives, by name.
type roads map[string]string

// at is where the member lives.
func (urls roads) at(name string) (string, error) { return urls[name], nil }

// rowOf is the row for the member named, and whether there is one.
func rowOf(rows []stack.Row, name string) (stack.Row, bool) {
	for _, row := range rows {
		if row.Name() == name {
			return row, true
		}
	}
	return stack.Row{}, false
}

// the good build says lint counted two.
const goodBuild = `echo '{"step":"lint","state":"ok","count":2}'`

// failOnce is a build script that fails the first run in a directory
// with the step and kind given, and passes the next: the count
// directory remembers the failure across runs.
func failOnce(countDir, step, kind string) string {
	return `f="` + countDir + `/$(basename "$PWD")"; ` +
		`if [ ! -e "$f" ]; then touch "$f"; ` +
		`echo '{"step":"` + step + `","state":"fail","kind":"` + kind +
		`"}'; exit 1; fi; ` + goodBuild
}

// reposFor is a road for every outcome a member can have, built in dir:
// a pass with its lint count, a deterministic failure, a transient
// failure that passes when tried again, a test that fails once and is
// flaky, and a ref that does not exist.
func reposFor(t *testing.T, dir string) roads {
	t.Helper()
	countDir := filepath.Join(dir, "counts")
	openDir := os.FileMode(0o755)
	if err := os.MkdirAll(countDir, openDir); err != nil {
		t.Fatal(err)
	}
	badBuild := `echo '{"step":"check","state":"fail",` +
		`"kind":"deterministic","text":"no"}'; exit 1` + "\n"
	return roads{
		"good": bareRepo(t, dir, "good",
			map[string]string{"build.sh": goodBuild + "\n"}),
		"bad": bareRepo(t, dir, "bad", map[string]string{"build.sh": badBuild}),
		"net": bareRepo(t, dir, "net", map[string]string{
			"build.sh": failOnce(countDir, "build", "transient") + "\n"}),
		"flaky": bareRepo(t, dir, "flaky", map[string]string{
			"build.sh": failOnce(countDir, "test", "deterministic") + "\n"}),
		"gone": bareRepo(t, dir, "gone",
			map[string]string{"build.sh": goodBuild + "\n"}),
	}
}

// every outcome a member can have, in one stack.
func TestRun(t *testing.T) {
	dir := t.TempDir()
	urls := reposFor(t, dir)
	members := []stack.Member{
		stack.Named("good", "v1"),
		stack.Named("bad", "v1"),
		stack.Named("net", "v1"),
		stack.Named("flaky", "v1"),
		stack.Named("gone", "v9"),
	}
	slept := &waited{}
	var out bytes.Buffer
	// the default tries stand: the first and two more. every
	// classification at once: this test is about how a failure is
	// named, not about the order they are reached in; the stop at the
	// first failure is its own test below.
	options := made(members, filepath.Join(dir, "work"), child).
		From(urls.at).
		InParallel(len(members)).
		Saying(&out).
		Sleeping(slept.record)
	rows, err := runBefore(t, context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	checkResults(t, rows, slept)
	checkTable(t, rows)
}

// checkResults is each member's outcome: the result, and for the
// failures the step and the tries.
func checkResults(t *testing.T, rows []stack.Row, slept *waited) {
	t.Helper()
	want := map[string]string{
		"good":  "ok",
		"bad":   "failed",
		"net":   "ok",
		"flaky": "flaky",
		"gone":  "failed",
	}
	for _, row := range rows {
		if row.Result() != want[row.Name()] {
			t.Errorf("%s: result %q, want %q", row.Name(), row.Result(),
				want[row.Name()])
		}
	}
	cells := tableOf(t, rows)
	good, _ := rowOf(rows, "good")
	if good.Lint() != int(held(t, "lint-reported")) {
		t.Errorf("good: lint %d, want the count it reported", good.Lint())
	}
	if bad := cells["bad"]; bad.result != "failed at check" || bad.tries != count.One {
		t.Errorf("bad: a deterministic failure is tried once and "+
			"named: %q after %d", bad.result, bad.tries)
	}
	if net := cells["net"]; net.tries != int(held(t, "transient-tries")) {
		t.Errorf("net: want the failure and the retry, got %d", net.tries)
	}
	if gone := cells["gone"]; gone.result != "failed at clone" {
		t.Errorf("gone: want a clone failure, got %q", gone.result)
	}
	if count.None(len(slept.seen)) {
		t.Error("nothing waited before trying again")
	}
}

// checkTable is the table a person reads: the failure's step.
func checkTable(t *testing.T, rows []stack.Row) {
	t.Helper()
	var table bytes.Buffer
	stack.Table(&table, rows)
	if !strings.Contains(table.String(), "failed at check") {
		t.Errorf("table:\n%s", table.String())
	}
}

// a member's patches are laid on its fresh clone before it is built, and
// a member whose patches do not land fails at patch and is never built.
func TestAMemberIsPatchedBeforeItIsBuilt(t *testing.T) {
	dir := t.TempDir()
	build := `if [ -e PATCHED ]; then echo '{"step":"check","state":"ok"}'; ` +
		`else echo '{"step":"check","state":"fail","text":"unpatched"}'; ` +
		`exit 1; fi` + "\n"
	urls := roads{}
	for _, name := range []string{"patched", "refused"} {
		urls[name] = bareRepo(t, dir, name,
			map[string]string{"build.sh": build})
	}
	seen := &built{}
	members := []stack.Member{
		stack.Named("patched", "v1"), stack.Named("refused", "v1"),
	}
	options := made(members, filepath.Join(dir, "work"), seen.child).
		From(urls.at).
		Patching(patchFirst{t}.lay).
		InParallel(len(members)).
		Trying(count.One)
	rows, err := runBefore(t, context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	cells := tableOf(t, rows)
	if cells["patched"].result != "ok" {
		t.Errorf("the patched member did not pass: %+v", cells["patched"])
	}
	if cells["refused"].result != "failed at patch" {
		t.Errorf("the refused member did not fail at patch: %+v", cells["refused"])
	}
	if seen.fix["refused"] {
		t.Error("a member whose patches did not land was built")
	}
}

// built notes which members were built, by name.
type built struct {
	mu  sync.Mutex
	fix map[string]bool
}

// child notes the build, then builds as usual.
func (seen *built) child(dir string) *exec.Cmd {
	seen.mu.Lock()
	defer seen.mu.Unlock()
	if seen.fix == nil {
		seen.fix = map[string]bool{}
	}
	seen.fix[filepath.Base(dir)] = true
	return child(dir)
}

// patchFirst lays one patch, or refuses the member named refused.
type patchFirst struct{ t *testing.T }

// lay writes the marker the build looks for, or refuses.
func (lay patchFirst) lay(member stack.Member, dir string) (int, error) {
	if member.Name() == "refused" {
		return count.Nothing, errors.New("01.patch does not apply")
	}
	openFile := os.FileMode(0o644)
	err := os.WriteFile(filepath.Join(dir, "PATCHED"), nil, openFile)
	if err != nil {
		return count.Nothing, err
	}
	return count.One, nil
}

// the network's words are transient, a compiler's are not.
func TestKind(t *testing.T) {
	if stack.Kind("go: downloading x: dial tcp: i/o timeout") != "transient" {
		t.Error("a timeout is transient")
	}
	if stack.Kind("./main.go:3: undefined: x") != "deterministic" {
		t.Error("a compile error is deterministic")
	}
}

// secondFails is a build where the member named two fails.
type secondFails struct{}

// child builds: two fails deterministically, the rest pass.
func (secondFails) child(dir string) *exec.Cmd {
	script := `echo '{"step":"check","state":"ok"}'`
	if filepath.Base(dir) == "two" {
		script = `echo '{"step":"check","state":"fail",` +
			`"kind":"deterministic","text":"no"}'; exit 1`
	}
	return exec.Command("sh", "-c", script)
}

// TestRunStopsAtTheFirstFailure: walking one at a time, a member that
// fails ends the run where it happened. a person watching wants the
// failure on the screen, not a table after eight more builds.
func TestRunStopsAtTheFirstFailure(t *testing.T) {
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	names := []string{"one", "two", "three"}
	for _, name := range names {
		at := filepath.Join(work, name)
		if err := os.MkdirAll(at, openDir); err != nil {
			t.Fatal(err)
		}
		card := filepath.Join(at, ".game")
		if err := os.WriteFile(card, []byte("name "+name+"\n"),
			openFile); err != nil {
			t.Fatal(err)
		}
	}
	members := []stack.Member{
		stack.Named("one", ""), stack.Named("two", ""),
		stack.Named("three", ""),
	}
	var out bytes.Buffer
	options := made(members, work, secondFails{}.child).
		Trying(count.One).
		Saying(&out)
	rows, err := runBefore(t, context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != count.Last(len(members)) {
		t.Fatalf("want the run stopped at the second: %+v", rows)
	}
	failed, _ := rowOf(rows, "two")
	if failed.Result() != "failed" {
		t.Errorf("the second member is the one that failed: %+v", failed)
	}
	if strings.Contains(out.String(), "three") {
		t.Error("the third member was reached after a failure")
	}
}
