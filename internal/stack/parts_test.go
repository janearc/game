package stack_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/stack"
)

// largest is a jitter that always draws the most it may.
func largest(ceiling int64) int64 { return ceiling - count.One }

// noWait is a sleep that does not.
func noWait(time.Duration) {}

// waited records the waits a build makes, in order.
type waited struct {
	mu   sync.Mutex
	seen []time.Duration
}

// record keeps one wait.
func (seen *waited) record(wait time.Duration) {
	seen.mu.Lock()
	defer seen.mu.Unlock()
	seen.seen = append(seen.seen, wait)
}

// scripted is a build that runs a shell script.
type scripted string

// child runs the script in a shell.
func (script scripted) child(string) *exec.Cmd {
	return exec.Command("sh", "-c", string(script))
}

// missingChild is a build that is not there.
func missingChild(dir string) *exec.Cmd {
	return exec.Command(filepath.Join(dir, "no-such-build"))
}

// noRoad is a remote that cannot say where a member lives.
func noRoad(string) (string, error) { return "", errors.New("no road") }

// made is stack.Build's options, policed.
func made(
	members []stack.Member, work string, child func(dir string) *exec.Cmd,
) stack.Options {
	return policed(stack.Build(members, work, child))
}

// policed is the options given what a build wants of its caller: the
// clock and the dice. nothing waits, and a wait is the most it may be.
func policed(options stack.Options) stack.Options {
	return options.Sleeping(noWait).Jittered(largest)
}

// cell is one member's row as the table a person reads says it.
type cell struct {
	result      string
	lint, tries int
}

// tableLine is a line of the table: the repository, the ref, the
// result, the lint count and the tries, in the widths Table prints.
var tableLine = regexp.MustCompile(`^(?P<name>.{18}) (?P<ref>.{10}) ` +
	`(?P<result>.{22}) +(?P<lint>\d+) +(?P<tries>\d+)$`)

// tableOf is the rows as the table says them, by repository.
func tableOf(t *testing.T, rows []stack.Row) map[string]cell {
	t.Helper()
	var out bytes.Buffer
	stack.Table(&out, rows)
	body := strings.TrimRight(out.String(), "\n")
	lines := strings.Split(body, "\n")
	cells := map[string]cell{}
	for _, line := range lines[count.One:] {
		match := tableLine.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("a table line that is not one: %q", line)
		}
		lint, _ := strconv.Atoi(match[tableLine.SubexpIndex("lint")])
		tries, _ := strconv.Atoi(match[tableLine.SubexpIndex("tries")])
		name := strings.TrimSpace(match[tableLine.SubexpIndex("name")])
		cells[name] = cell{
			result: strings.TrimSpace(match[tableLine.SubexpIndex("result")]),
			lint:   lint, tries: tries,
		}
	}
	return cells
}

// dressed writes a member's art directory and .game under work.
func dressed(t *testing.T, work string, files map[string]string) {
	t.Helper()
	openDir := os.FileMode(0o755)
	openFile := os.FileMode(0o644)
	for name, body := range files {
		path := filepath.Join(work, "m", name)
		if err := os.MkdirAll(filepath.Dir(path), openDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), openFile); err != nil {
			t.Fatal(err)
		}
	}
}

// inPlace is options for one member, m, already in its place under a
// fresh work directory, whose build is the child given.
func inPlace(
	t *testing.T,
	child func(dir string) *exec.Cmd,
) (stack.Options, string) {
	t.Helper()
	work := t.TempDir()
	dressed(t, work, map[string]string{".game": "name m\n"})
	tries := int(held(t, "in-place-tries"))
	members := []stack.Member{stack.Named("m", "")}
	options := made(members, work, child).
		Trying(tries).
		Saying(io.Discard)
	return options, work
}

// ranOne is the member m's row as the table says it, after the options
// run.
func ranOne(t *testing.T, options stack.Options) cell {
	t.Helper()
	rows, err := runBefore(t, context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	return tableOf(t, rows)["m"]
}

// TestRunNeedsItsCallersClockAndDice: a build is given what waits and
// what draws a wait, and is refused without either; with both it runs.
func TestRunNeedsItsCallersClockAndDice(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	bare := stack.Build(nil, work, nil)
	for name, options := range map[string]stack.Options{
		"no sleep":  bare.Jittered(largest),
		"no jitter": bare.Sleeping(noWait),
		"nothing":   bare,
	} {
		if _, err := runBefore(t, context.Background(), options); err == nil {
			t.Errorf("%s: the build ran", name)
		}
	}
	if _, err := runBefore(t, context.Background(), made(nil, work, nil)); err != nil {
		t.Errorf("a build given both: %v", err)
	}
}

// said is what a caller says of how many members to build at once and
// how many tries a transient failure gets. nothing said is zero.
type said struct{ parallel, tries int }

// apply is the options with that said.
func (say said) apply(options stack.Options) stack.Options {
	return options.InParallel(say.parallel).Trying(say.tries)
}

// buildTwo builds two members, m and n, whose build fails on the network
// every time, with that said, and gives the table.
func buildTwo(t *testing.T, say said) map[string]cell {
	t.Helper()
	work := t.TempDir()
	members := []stack.Member{stack.Named("m", ""), stack.Named("n", "")}
	for _, member := range members {
		path := filepath.Join(work, member.Name(), ".game")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("name x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	flaky := scripted(`echo '{"step":"check","state":"fail","kind":"transient"}'`)
	options := say.apply(made(members, work, flaky.child))
	rows, err := runBefore(t, context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	return tableOf(t, rows)
}

// TestADefaultIsUsedOnlyForWhatIsMissing: with nothing said a build is one
// at a time and gives a transient failure three tries; what is said, even
// at its least, is kept.
func TestADefaultIsUsedOnlyForWhatIsMissing(t *testing.T) {
	t.Parallel()
	missing := buildTwo(t, said{})
	tries := int(held(t, "default-tries"))
	if len(missing) != count.One || missing["m"].tries != tries {
		t.Errorf("nothing said: %v, want the first member alone, %d tries",
			missing, tries)
	}
	two := count.Next(count.One)
	given := buildTwo(t, said{parallel: two, tries: count.One})
	if len(given) != two || given["m"].tries != count.One ||
		given["n"].tries != count.One {
		t.Errorf("one try said, two at once: %v", given)
	}
	more := said{
		parallel: int(held(t, "given-parallel")),
		tries:    int(held(t, "given-tries")),
	}
	above := buildTwo(t, more)
	if above["m"].tries != more.tries || above["n"].tries != more.tries {
		t.Errorf("tries given above the least: %v", above)
	}
}

// TestBackoffDoublesFromASecondToThirty: the most a wait can be starts at
// a second, doubles each attempt, and stops at thirty seconds. a clone
// that cannot be made waits between its tries, and the waits show it.
func TestBackoffDoublesFromASecondToThirty(t *testing.T) {
	t.Parallel()
	under := int(held(t, "backoff-last-under"))
	past := int(held(t, "backoff-past-cap"))
	tries := count.Next(count.Next(past))
	slept := &waited{}
	gone := filepath.Join(t.TempDir(), "gone.git")
	members := []stack.Member{stack.Named("m", "v1")}
	work := filepath.Join(t.TempDir(), "w")
	options := made(members, work, nil).
		From(roads{"m": gone}.at).
		Trying(tries).
		Sleeping(slept.record)
	ranOne(t, options)
	ceilings := map[int]time.Duration{
		count.First:       secondsHeld(t, "backoff-first-seconds"),
		count.One:         secondsHeld(t, "backoff-doubled-seconds"),
		under:             secondsHeld(t, "backoff-doubled-fourth-seconds"),
		count.Next(under): secondsHeld(t, "backoff-longest-seconds"),
		past:              secondsHeld(t, "backoff-longest-seconds"),
	}
	if len(slept.seen) != count.Last(tries) {
		t.Fatalf("waited %d times in %d tries: %v", len(slept.seen), tries,
			slept.seen)
	}
	for attempt, ceiling := range ceilings {
		if got := slept.seen[attempt]; got != ceiling-count.One {
			t.Errorf("attempt %d: waited %v, want just under %v", attempt,
				got, ceiling)
		}
	}
}

// TestArtFindsTheMembersCard: the .game's first name before the
// directory's; an empty card is passed over; a lone card is taken, and
// two lone ones, an empty one or none at all give nothing. what is shown
// is the card, its own blank lines trimmed, then a blank line.
func TestArtFindsTheMembersCard(t *testing.T) {
	t.Parallel()
	for _, card := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"by the directory", map[string]string{"art/m.ansi": "dir"}, "dir"},
		{"by the .game first", map[string]string{
			".game":           "lint width 80\n  name   pretty  \nname other\n",
			"art/pretty.ansi": "pretty", "art/other.ansi": "other",
			"art/m.ansi": "dir"}, "pretty"},
		{"an empty card passed over", map[string]string{
			".game": "name pretty\n", "art/pretty.ansi": "",
			"art/m.ansi": "dir"}, "dir"},
		{"a lone card", map[string]string{"art/lone.ansi": "lone"}, "lone"},
		{"a one byte card beside another", map[string]string{
			"art/m.ansi": "x", "art/other.ansi": "o"}, "x"},
		{"two lone cards", map[string]string{"art/a.ansi": "a",
			"art/b.ansi": "b"}, ""},
		{"a lone empty card", map[string]string{"art/lone.ansi": ""}, ""},
		{"no art", map[string]string{".game": "name m\n"}, ""},
		{"a card with blank lines", map[string]string{
			"art/m.ansi": "card\n\n"}, "card"},
	} {
		work := t.TempDir()
		files := map[string]string{".game": "lint width 80\n"}
		for name, body := range card.files {
			files[name] = body
		}
		dressed(t, work, files)
		var out bytes.Buffer
		members := []stack.Member{stack.Named("m", "")}
		options := made(members, work, passing).
			Trying(count.One).Saying(&out)
		if _, err := runBefore(t, context.Background(), options); err != nil {
			t.Fatal(err)
		}
		shown := card.want + "\n\n"
		if card.want == "" {
			shown = ""
		}
		if !strings.HasPrefix(out.String(), shown) ||
			(card.want == "" && strings.HasPrefix(out.String(), "\n")) {
			t.Errorf("%s: said %q, want it to open with %q", card.name,
				out.String(), shown)
		}
	}
}

// passing is a build that passes.
func passing(string) *exec.Cmd {
	return exec.Command("sh", "-c", `echo '{"step":"done","state":"ok"}'`)
}

// TestACardClearsAScreenOnlyWhenTold: asked to clear, a build clears the
// screen before the card; not asked, it does not.
func TestACardClearsAScreenOnlyWhenTold(t *testing.T) {
	t.Parallel()
	for clear, want := range map[bool]bool{true: true, false: false} {
		work := t.TempDir()
		dressed(t, work, map[string]string{
			".game": "name m\n", "art/m.ansi": "card\n"})
		var out bytes.Buffer
		members := []stack.Member{stack.Named("m", "")}
		options := made(members, work, passing).
			Trying(count.One).Clearing(clear).Saying(&out)
		if _, err := runBefore(t, context.Background(), options); err != nil {
			t.Fatal(err)
		}
		cleared := strings.HasPrefix(out.String(), "\x1b[H\x1b[2Jcard\n\n")
		plain := strings.HasPrefix(out.String(), "card\n\n")
		if cleared != want || plain == want {
			t.Errorf("clearing %v: said %q", clear, out.String())
		}
	}
}

// TestClearsOnlyATerminal: a character device clears; a file and a
// buffer do not.
func TestClearsOnlyATerminal(t *testing.T) {
	t.Parallel()
	device, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer device.Close()
	plain := filepath.Join(t.TempDir(), "plain")
	file, err := os.Create(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if !stack.Clears(device) || stack.Clears(file) ||
		stack.Clears(&bytes.Buffer{}) {
		t.Errorf("device %v, file %v, buffer %v", stack.Clears(device),
			stack.Clears(file), stack.Clears(&bytes.Buffer{}))
	}
}

// TestCloneTriesThenSaysWhy: a clone that cannot be made is tried the
// times asked, waiting the backoff between and not after the last, and
// the member is failed at clone; one that can be made replaces what was in
// its directory.
func TestCloneTriesThenSaysWhy(t *testing.T) {
	t.Parallel()
	slept := &waited{}
	var said bytes.Buffer
	tries := int(held(t, "clone-tries"))
	work := t.TempDir()
	members := []stack.Member{stack.Named("m", "v1")}
	gone := filepath.Join(t.TempDir(), "gone.git")
	options := made(members, work, passing).
		Trying(tries).
		Sleeping(slept.record).
		Saying(&said)
	got := ranOne(t, options.From(roads{"m": gone}.at))
	if got.result != "failed at clone" {
		t.Errorf("a clone of nothing: %+v", got)
	}
	first := secondsHeld(t, "backoff-first-seconds")
	second := count.Next(count.First)
	if len(slept.seen) != count.Last(tries) ||
		slept.seen[count.First] != first-count.One ||
		slept.seen[second] != (first<<count.One)-count.One ||
		strings.Count(said.String(), "clone failed") != count.Last(tries) {
		t.Errorf("slept %v, said %q", slept.seen, said.String())
	}
	bare := bareRepo(t, t.TempDir(), "m", map[string]string{"a.txt": "a\n"})
	dressed(t, work, map[string]string{"stray.txt": "old\n"})
	got = ranOne(t, options.From(roads{"m": bare}.at))
	if got.result != "ok" {
		t.Fatalf("a clone that can be made: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(work, "m", "stray.txt")); err == nil {
		t.Error("what was in the directory is still there")
	}
	if _, err := os.Stat(filepath.Join(work, "m", "a.txt")); err != nil {
		t.Errorf("the clone is not there: %v", err)
	}
}

// TestTableSaysWhereAFailureWas: a failure names its step; a pass does
// not.
func TestTableSaysWhereAFailureWas(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	urls := reposFor(t, dir)
	members := []stack.Member{
		stack.Named("good", "v1"), stack.Named("bad", "v1"),
	}
	options := made(members, filepath.Join(dir, "work"), child).
		From(urls.at).
		InParallel(len(members))
	rows, err := runBefore(t, context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	stack.Table(&out, rows)
	text := out.String()
	if !strings.Contains(text, "failed at check") ||
		strings.Contains(text, "ok at") {
		t.Errorf("table:\n%s", text)
	}
}

// TestPlacedSaysWhyAMemberIsNotThere: with no remote, a member must be
// in place with a .game; with one, a remote that cannot say where the
// member lives fails at remote.
func TestPlacedSaysWhyAMemberIsNotThere(t *testing.T) {
	t.Parallel()
	work := t.TempDir()
	members := []stack.Member{stack.Named("m", "")}
	options := made(members, work, passing).Trying(count.One)
	if got := ranOne(t, options); got.result != "failed at here" {
		t.Errorf("not here: %+v", got)
	}
	if got := ranOne(t, options.From(noRoad)); got.result != "failed at remote" {
		t.Errorf("no remote: %+v", got)
	}
}

// TestRunMakesItsWorkDirectory: the work directory is made, open to its
// owner and readable by others; one that cannot be made is an error.
func TestRunMakesItsWorkDirectory(t *testing.T) {
	t.Parallel()
	work := filepath.Join(t.TempDir(), "work")
	if _, err := runBefore(t, context.Background(),
		made(nil, work, nil)); err != nil {
		t.Fatal(err)
	}
	openDir := os.FileMode(0o755)
	if info, err := os.Stat(work); err != nil ||
		info.Mode().Perm() != openDir {
		t.Errorf("the work directory: %v, %v, want %v", info, err, openDir)
	}
	blocked := filepath.Join(t.TempDir(), "file")
	openFile := os.FileMode(0o644)
	if err := os.WriteFile(blocked, nil, openFile); err != nil {
		t.Fatal(err)
	}
	underAFile := filepath.Join(blocked, "work")
	if _, err := runBefore(t, context.Background(),
		made(nil, underAFile, nil)); err == nil {
		t.Error("work made under a file")
	}
}

// TestAFlakyTestCountsBothTries: a test that fails once and passes when
// run again is flaky, and both runs are counted.
func TestAFlakyTestCountsBothTries(t *testing.T) {
	t.Parallel()
	options, _ := inPlace(t, scripted(`if [ -f ran ]; then
	echo '{"step":"test","state":"ok"}'
else
	touch ran; echo '{"step":"test","state":"fail"}'
fi`).child)
	got := ranOne(t, options)
	if got.result != "flaky" || got.tries != int(held(t, "in-place-tries")) {
		t.Errorf("%+v, want flaky after two tries", got)
	}
}

// TestATransientBuildIsTriedWithNoWaitAfterTheLast: a build failing on
// the network is tried the times asked, waiting between tries only.
func TestATransientBuildIsTriedWithNoWaitAfterTheLast(t *testing.T) {
	t.Parallel()
	options, _ := inPlace(t,
		scripted(`echo '{"step":"check","state":"fail","kind":"transient"}'`).child)
	slept := &waited{}
	first := secondsHeld(t, "backoff-first-seconds")
	got := ranOne(t, options.Sleeping(slept.record))
	if got.result != "failed at check" ||
		got.tries != int(held(t, "in-place-tries")) ||
		len(slept.seen) != count.One ||
		slept.seen[count.First] != first-count.One {
		t.Errorf("%+v, slept %v", got, slept.seen)
	}
}

// redirectedChild is a build whose output is already taken.
type redirectedChild struct{ command *exec.Cmd }

// start makes the command with its stdout set.
func (child *redirectedChild) start(string) *exec.Cmd {
	child.command = exec.Command("sh", "-c", "exit 0")
	child.command.Stdout = io.Discard
	return child.command
}

// TestRunReadsTheChildsEvents: a child that cannot start fails at start;
// one whose output is taken fails there and is not run; a line that is
// not an event is passed over, even one with a state of fail; the lint
// count comes from lint alone; and a child that exits badly with no event
// fails as the child, on the network's word tried again.
func TestRunReadsTheChildsEvents(t *testing.T) {
	t.Parallel()
	options, _ := inPlace(t, missingChild)
	if got := ranOne(t, options); got.result != "failed at start" {
		t.Errorf("no child: %+v", got)
	}
	redirected := &redirectedChild{}
	options, _ = inPlace(t, redirected.start)
	if got := ranOne(t, options); got.result != "failed at start" {
		t.Errorf("a child with its output taken: %+v", got)
	}
	if redirected.command.Process != nil {
		t.Error("the child ran despite an unreadable event stream")
	}
	for script, want := range map[string]cell{
		`echo '{"step":"test","state":"fail","count":"many"}'
echo '{"step":"done","state":"ok"}'`: {"ok", count.Nothing, count.One},
		`echo '{"step":"lint","state":"ok","count":3}'
echo '{"step":"build","state":"ok","count":7}'`: {"ok", int(held(t, "lint-counted")), count.One},
		`echo 'dial tcp: refused' >&2; exit 1`: {"failed at child",
			count.Nothing, int(held(t, "in-place-tries"))},
		`echo 'a compiler said no' >&2; exit 1`: {"failed at child",
			count.Nothing, count.One},
	} {
		one, _ := inPlace(t, scripted(script).child)
		if got := ranOne(t, one); got != want {
			t.Errorf("%s:\n%+v, want %+v", script, got, want)
		}
	}
}

// TestAMemberWithNoChildToBuildItFailsAtStart: a member that clones and
// is given no child to build it is a failed row, not a panic.
func TestAMemberWithNoChildToBuildItFailsAtStart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	urls := roads{"m": bareRepo(t, dir, "m",
		map[string]string{"build.sh": goodBuild + "\n"})}
	members := []stack.Member{stack.Named("m", "v1")}
	got := ranOne(t, made(members, filepath.Join(dir, "w"), nil).From(urls.at))
	if got.result != "failed at start" {
		t.Fatalf("a member with no child came out %q, want failed at start", got.result)
	}
}

// TestAMemberWithAChildStillBuilds: the same member given the child
// that builds it passes.
func TestAMemberWithAChildStillBuilds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	urls := roads{"m": bareRepo(t, dir, "m",
		map[string]string{"build.sh": goodBuild + "\n"})}
	members := []stack.Member{stack.Named("m", "v1")}
	got := ranOne(t, made(members, filepath.Join(dir, "w"), child).From(urls.at))
	if got.result != "ok" {
		t.Fatalf("a member with a child came out %q, want ok", got.result)
	}
}
