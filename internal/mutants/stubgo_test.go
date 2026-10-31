package mutants_test

import (
	"fmt"

	"github.com/janearc/game/internal/count"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// goStubScript is a go ahead of the real one. go list is the real one's
// unless the test gave a listing. go test answers from the files the test
// left. the nth run prints out.n and waits sleep.n seconds if it has
// them. it exits with exit.n and leaves a note of where it ran. it counts
// its runs in a file, so only one run goes at a time.
//
// every run notes its pid in pids, so the test can end it. stubs that
// run themselves are what a broken REAL_GO makes. they would run at a
// full processor until the machine is restarted. so a run that finds it
// has been exec'd more than a few times in a row says so and exits.
//
// runs that wait do so as the sleep of the stub's own folder, which is a
// link to sleep. the test can then tell its own wait from any other
// process given the same pid since. child.n is a wait that go test starts
// and leaves running, as go test leaves the test binary.
// hold.n is a wait that go test starts and leaves holding the output it
// was given, then exits without waiting for it.
//
// every run leaves a folder in its TMPDIR, as a test binary killed at its
// first failure leaves its own. before it notes its TMPDIR it notes in
// outlived every earlier run's TMPDIR that is still there.
const goStubScript = `#!/bin/sh
d="$GOSTUB"
echo "$$" >> "$d/pids"
depth=$(( ${GOSTUB_DEPTH:-0} + 1 ))
if [ "$depth" -gt 5 ]; then
	echo "go stub: it runs itself; REAL_GO is not a go" >&2
	exit 97
fi
export GOSTUB_DEPTH="$depth"
case "$1" in
list)
	if [ -f "$d/list" ]; then cat "$d/list"; exit 0; fi
	exec "$REAL_GO" "$@" ;;
test)
	pwd >> "$d/where"
	if [ -f "$d/tmpdirs" ]; then
		while read -r was; do
			if [ -d "$was" ]; then echo "$was" >> "$d/outlived"; fi
		done < "$d/tmpdirs"
	fi
	echo "$TMPDIR" >> "$d/tmpdirs"
	if [ -n "$TMPDIR" ]; then mkdir -p "$TMPDIR/left-by-a-killed-test" 2>/dev/null; fi
	n=$(cat "$d/count" 2>/dev/null || echo 0); n=$((n+1))
	echo "$n" > "$d/count"
	{ pwd; echo "$@"; find . | sort; } > "$d/tree.$n"
	ls -lnR . > "$d/long.$n"
	if [ -f "$d/out.$n" ]; then cat "$d/out.$n"; fi
	if [ -f "$d/child.$n" ]; then
		"$GOSTUB_BIN/sleep" "$(cat "$d/child.$n")" &
		echo "$!" >> "$d/pids"
		wait
	fi
	if [ -f "$d/hold.$n" ]; then
		"$GOSTUB_BIN/sleep" "$(cat "$d/hold.$n")" &
		echo "$!" >> "$d/pids"
		exit 0
	fi
	if [ -f "$d/sleep.$n" ]; then exec "$GOSTUB_BIN/sleep" "$(cat "$d/sleep.$n")"; fi
	exit "$(cat "$d/exit.$n" 2>/dev/null || echo 0)" ;;
esac
exec "$REAL_GO" "$@"
`

// goStub is the directory the stub go keeps its notes and answers in, and
// the folder the stub itself and its sleep are in.
type goStub struct{ dir, bin string }

// stubGo puts the stub go first on PATH. it sets PATH, so a test that
// uses it runs alone. when the test is over, whatever the stub started and
// left running is ended.
func stubGo(t *testing.T) goStub {
	t.Helper()
	real := os.Getenv("REAL_GO")
	if real == "" {
		found, err := exec.LookPath("go")
		if err != nil {
			t.Fatal(err)
		}
		real = found
	}
	bin, notes := t.TempDir(), t.TempDir()
	script := []byte(goStubScript)
	if err := os.WriteFile(filepath.Join(bin, "go"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sleep, filepath.Join(bin, "sleep")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REAL_GO", real)
	t.Setenv("GOSTUB", notes)
	t.Setenv("GOSTUB_BIN", bin)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	stub := goStub{dir: notes, bin: bin}
	t.Cleanup(ending{stub: stub, t: t}.end)
	return stub
}

// the lowest pid the stub's cleanup signals: pid 1 is the system's first
// process, and group 0 is the test's own. defined here, as the run's
// guard defines it.
const lowestPid = 2

// end ends every process the stub noted and its group, whatever the test
// found. it gives the pids of those it ended. pids that are not the
// stub's now are left alone. they have gone and the number may be another
// process's. end looks at what the pid is running. for the stub's own
// that is a path in its folder.
func (stub goStub) end(t *testing.T) []int {
	t.Helper()
	noted, err := os.ReadFile(filepath.Join(stub.dir, "pids"))
	if err != nil {
		return nil
	}
	var ended []int
	seen := map[int]bool{}
	for _, field := range strings.Fields(string(noted)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid < lowestPid || seen[pid] {
			continue
		}
		seen[pid] = true
		running, _ := exec.Command("ps", "-o", "command=", "-p", field).Output()
		if !strings.Contains(string(running), stub.bin) {
			continue
		}
		syscall.Kill(-pid, syscall.SIGKILL)
		syscall.Kill(pid, syscall.SIGKILL)
		ended = append(ended, pid)
	}
	return ended
}

// keep writes one note file.
func (stub goStub) keep(t *testing.T, name, text string) {
	t.Helper()
	path := filepath.Join(stub.dir, name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// answer sets what the nth go test prints and how it exits.
func (stub goStub) answer(t *testing.T, nth int, out string, exit int) {
	t.Helper()
	stub.keep(t, "out."+strconv.Itoa(nth), out)
	stub.keep(t, "exit."+strconv.Itoa(nth), strconv.Itoa(exit))
}

// pause sets how long the nth go test waits, in seconds.
func (stub goStub) pause(t *testing.T, nth int, seconds string) {
	t.Helper()
	stub.keep(t, "sleep."+strconv.Itoa(nth), seconds)
}

// listing sets what go list prints.
func (stub goStub) listing(t *testing.T, text string) {
	t.Helper()
	stub.keep(t, "list", text)
}

// ran is what the nth go test noted: where it ran, its words, and
// the files it found there, one a line.
func (stub goStub) ran(t *testing.T, nth int) string {
	t.Helper()
	name := "tree." + strconv.Itoa(nth)
	noted, err := os.ReadFile(filepath.Join(stub.dir, name))
	if err != nil {
		t.Fatalf("go test never ran %d times: %v", nth, err)
	}
	return string(noted)
}

// long is the nth go test's long listing of where it ran.
func (stub goStub) long(t *testing.T, nth int) string {
	t.Helper()
	name := "long." + strconv.Itoa(nth)
	noted, err := os.ReadFile(filepath.Join(stub.dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(noted)
}

// where is the directories every go test ran in, one a line.
func (stub goStub) where(t *testing.T) []string {
	t.Helper()
	noted, err := os.ReadFile(filepath.Join(stub.dir, "where"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(noted))
}

// calls is how many times go test ran.
func (stub goStub) calls() int {
	counted, err := os.ReadFile(filepath.Join(stub.dir, "count"))
	if err != nil {
		return count.Nothing
	}
	text := strings.TrimSpace(string(counted))
	number, _ := strconv.Atoi(text)
	return number
}

// ticking is a clock a test scripts: each reading gives the time and
// moves it on by the next step, and by the rest when the steps are used.
type ticking struct {
	mu    sync.Mutex
	at    time.Time
	steps []time.Duration
	rest  time.Duration
}

// now is the clock's time, and moves it on.
func (clock *ticking) now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	was := clock.at
	step := clock.rest
	if count.Positive(len(clock.steps)) {
		step, clock.steps = clock.steps[count.First], clock.steps[count.One:]
	}
	clock.at = clock.at.Add(step)
	return was
}

// manyFunctions is a go file of that many functions, each with a sum of
// two literals and so three changes.
func manyFunctions(functions int) string {
	var src strings.Builder
	src.WriteString("package many\n")
	for place := range functions {
		fmt.Fprintf(&src, "\n// F%d adds.\nfunc F%d() int { return 1 + 1 }\n",
			place, place)
	}
	return src.String()
}

// tempDirs is the TMPDIR every go test ran with, one a line.
func (stub goStub) tempDirs(t *testing.T) []string {
	t.Helper()
	noted, err := os.ReadFile(filepath.Join(stub.dir, "tmpdirs"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(noted))
}

// outlived is every TMPDIR a later go test found still there. A run that
// removes each go test's folder as it returns has none.
func (stub goStub) outlived() []string {
	noted, _ := os.ReadFile(filepath.Join(stub.dir, "outlived"))
	return strings.Fields(string(noted))
}

// ending ends what the stub started, when the test is over.
type ending struct {
	stub goStub
	t    *testing.T
}

// end ends what the stub noted and its group.
func (done ending) end() {
	done.stub.end(done.t)
}
