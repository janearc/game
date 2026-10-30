// Package stack builds many repos as one. a stack file lists each member
// and the ref to build. your config says where each one lives.
//
// each is cloned fresh and built by game in its own child process.
// the parent decides what passed.
//
// events are JSON lines described by a Go type. game needs git and go.
// the only reader of the stream is game.
package stack

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/janearc/game/internal/count"
)

// Member is one repository in a stack, and the tag or branch to build.
type Member struct {
	name string
	ref  string
}

// Named is a member of a stack: the repository's name and the tag or
// branch to build.
func Named(name, ref string) Member {
	return Member{name: name, ref: ref}
}

// Name is the member's name, which is the directory it is built in.
func (member Member) Name() string { return member.name }

// Ref is the tag or branch the member is built at.
func (member Member) Ref() string { return member.ref }

// Event is one line a child writes: a step starting, passing or failing.
// json reads and writes these fields; they are the wire between a
// member's verify and the stack.
type Event struct {
	Repo  string `json:"repo,omitempty"`
	Step  string `json:"step"`
	State string `json:"state"`
	Kind  string `json:"kind,omitempty"`
	Count int    `json:"count,omitempty"`
	Text  string `json:"text,omitempty"`
}

// Happened is one event, as a child writes it: the step, its state,
// the kind of failure when it is one, what it said, and its count.
func Happened(step, state, kind, text string, counted int) Event {
	return Event{Step: step, State: state, Kind: kind, Text: cut(text),
		Count: counted}
}

// transient matches network errors that may resolve on retry.
var transient = regexp.MustCompile(
	`(?i)dial tcp|i/o timeout|connection reset|TLS handshake|` +
		`temporary failure|could not resolve|\b50[234]\b|` +
		`unexpected EOF`,
)

// Kind is transient for network output, deterministic for others.
func Kind(output string) string {
	if transient.MatchString(output) {
		return "transient"
	}
	return "deterministic"
}

// Row is one member's outcome, for the table.
type Row struct {
	member Member
	result string // ok, flaky, or failed
	failed string // the step that failed, when one did
	lint   int
	tries  int
}

// Name is the member's name.
func (row Row) Name() string { return row.member.name }

// Result is how the member came out: ok, flaky, or failed.
func (row Row) Result() string { return row.result }

// Lint is how many findings the member's lint counted.
func (row Row) Lint() int { return row.lint }

// Options are what a stack build needs. Build makes them with what
// every build has; the rest is said by a method each.
type Options struct {
	members []Member
	url     func(name string) (string, error) // where a member lives
	// the directory clones go under
	work  string
	child func(dir string) *exec.Cmd // the build a member gets
	// patch lays a member's patches on its fresh clone, before it is
	// built, and says how many it laid. nil lays none. members already
	// here are never patched.
	patch    func(member Member, dir string) (int, error)
	parallel int
	// clear is whether the screen is cleared before a card: a terminal is.
	clear bool
	tries int // attempts at a transient failure, clone or build
	out   io.Writer
	// sleep waits and jitter is a random wait below a ceiling, for
	// backoff: the caller owns the clock and the dice.
	sleep  func(time.Duration)
	jitter func(ceiling int64) int64
}

// Build is a build of members under work, each by the child the
// function gives for its directory.
func Build(members []Member, work string,
	child func(dir string) *exec.Cmd) Options {
	return Options{members: members, work: work, child: child}
}

// From is the options with where each member lives, by name. Without
// it every member must already be in place.
func (asked Options) From(url func(name string) (string, error)) Options {
	asked.url = url
	return asked
}

// Patching is the options with what lays a member's patches on its
// fresh clone and says how many it laid.
func (asked Options) Patching(
	patch func(member Member, dir string) (int, error)) Options {
	asked.patch = patch
	return asked
}

// InParallel is the options building that many members at once.
func (asked Options) InParallel(members int) Options {
	asked.parallel = members
	return asked
}

// Saying is the options writing what happens on out.
func (asked Options) Saying(out io.Writer) Options {
	asked.out = out
	return asked
}

// Trying is the options giving a transient failure that many attempts.
func (asked Options) Trying(tries int) Options {
	asked.tries = tries
	return asked
}

// Clearing is the options clearing the screen before each card, which a
// terminal wants and a log does not.
func (asked Options) Clearing(clear bool) Options {
	asked.clear = clear
	return asked
}

// Sleeping is the options waiting with the sleep given.
func (asked Options) Sleeping(sleep func(time.Duration)) Options {
	asked.sleep = sleep
	return asked
}

// Jittered is the options drawing its waits with the jitter given.
func (asked Options) Jittered(jitter func(ceiling int64) int64) Options {
	asked.jitter = jitter
	return asked
}

// oneAtATime is the fewest members built at once, and the count at
// which the first failure stops the run: defined so.
const oneAtATime = 1

// threeTries is how many attempts a transient failure gets by default,
// defined here as the first and two more.
const threeTries = 3

// settled is the options with defaults filled in. parallel is one,
// tries is three and out discards.
func (asked Options) settled() Options {
	if asked.parallel < oneAtATime {
		asked.parallel = oneAtATime
	}
	if asked.tries < count.One {
		asked.tries = threeTries
	}
	if asked.out == nil {
		asked.out = io.Discard
	}
	return asked
}

// folderMode is the mode the work directory is made with: the owner
// writes, everyone reads and enters, defined as 0755 in sys/stat.h's
// bits.
const folderMode = 0o755

// Run builds every member, at most parallel at once, and returns their
// rows in the stack's order. one at a time, it stops after the first
// failed row. the error is only for a stack that could not start.
func Run(ctx context.Context, asked Options) ([]Row, error) {
	asked = asked.settled()
	if asked.sleep == nil || asked.jitter == nil {
		return nil, errors.New("a stack build is given Sleeping and " +
			"Jittered: the caller owns the clock and the dice")
	}
	if err := os.MkdirAll(asked.work, folderMode); err != nil {
		return nil, err
	}
	var mu sync.Mutex
	if asked.parallel == oneAtATime {
		// each member's art shows as it is reached. the first failure
		// stops the run.
		rows := make([]Row, count.Nothing, len(asked.members))
		for _, member := range asked.members {
			asked.card(member, &mu)
			row := asked.member(ctx, member, &mu)
			rows = append(rows, row)
			if row.result == "failed" {
				return rows, nil
			}
		}
		return rows, nil
	}
	building := &building{
		options: asked, ctx: ctx, mu: &mu,
		rows:  make([]Row, len(asked.members)),
		slots: make(chan struct{}, asked.parallel),
	}
	for at, member := range asked.members {
		building.wg.Add(count.One)
		go building.one(at, member)
	}
	building.wg.Wait()
	return building.rows, nil
}

// building is a run of many members at once: the rows each fills in,
// and the slots that hold how many run together.
type building struct {
	options Options
	ctx     context.Context
	mu      *sync.Mutex
	rows    []Row
	slots   chan struct{}
	wg      sync.WaitGroup
}

// one builds one member in a slot, and puts its row in place.
func (build *building) one(at int, member Member) {
	defer build.wg.Done()
	build.slots <- struct{}{}
	defer build.free()
	build.rows[at] = build.options.member(build.ctx, member, build.mu)
}

// free gives a slot back.
func (build *building) free() {
	<-build.slots
}

// art reads a member's card from art/NAME.ansi. NAME is the project name in
// the member's .game, then the directory name. failing both, a lone .ansi in
// art/ is used. the bool is false when there is none.
func (asked Options) art(member Member) ([]byte, bool) {
	dir := filepath.Join(asked.work, member.name, "art")
	names := []string{member.name}
	if game, err := os.ReadFile(
		filepath.Join(asked.work, member.name, ".game"),
	); err == nil {
		if called, ok := projectNamed(string(game)); ok {
			names = append([]string{called}, names...)
		}
	}
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name+".ansi"))
		if err == nil && count.Positive(len(body)) {
			return body, true
		}
	}
	found, err := filepath.Glob(filepath.Join(dir, "*.ansi"))
	if err != nil || len(found) != count.One {
		return nil, false
	}
	body, err := os.ReadFile(found[count.First])
	if err != nil || count.None(len(body)) {
		return nil, false
	}
	return body, true
}

// projectNamed is the name a .game gives its project, from its name
// line, and whether it has one.
func projectNamed(game string) (string, bool) {
	for _, line := range strings.Split(game, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "name ") {
			continue
		}
		called := strings.TrimPrefix(line, "name ")
		return strings.TrimSpace(called), true
	}
	return "", false
}

// clearScreen is the terminal's words for a cleared screen with the
// cursor at its top, as ANSI defines them.
const clearScreen = "\x1b[H\x1b[2J"

// card writes a member's art. when told to, it clears the screen first.
// missing or unreadable art is skipped.
func (asked Options) card(member Member, mu *sync.Mutex) {
	art, ok := asked.art(member)
	if !ok {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	// the clear is for a terminal only. a redirected build keeps its log,
	// and a stopped build keeps its failure on screen, as nothing clears
	// after the last one.
	if asked.clear {
		fmt.Fprint(asked.out, clearScreen)
	}
	card := strings.TrimRight(string(art), "\n")
	fmt.Fprintf(asked.out, "%s\n\n", card)
}

// Clears is whether out is a terminal, which a character device is and a
// file, a pipe or a buffer is not.
func Clears(out io.Writer) bool {
	file, _ := out.(*os.File) // a nil file has no mode, and Stat says so
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != noMode
}

// noMode is a file mode with no bits set.
var noMode os.FileMode

// voice is how one member's build says what happens: on the stack's
// out, under the member's name, one at a time.
type voice struct {
	out  io.Writer
	name string
	mu   *sync.Mutex
}

// say writes one line for the member.
func (said voice) say(format string, args ...any) {
	said.mu.Lock()
	defer said.mu.Unlock()
	fmt.Fprintf(said.out, "stack %s: "+format+"\n",
		append([]any{said.name}, args...)...)
}

// placed puts a member under its name. with no remote it must already be
// there, else it is cloned at its ref. it returns false if the member
// could not be placed, with the row saying why.
func (asked Options) placed(
	member Member,
	dir string,
	row *Row,
	said voice,
) bool {
	if asked.url == nil {
		// no remote to ask, so the member must already be here with a
		// .game. one with no go.mod is checked by its own test target.
		if _, err := os.Stat(filepath.Join(dir, ".game")); err != nil {
			row.result, row.failed = "failed", "here"
			return false
		}
		said.say("in place")
		return true
	}
	found, err := asked.url(member.name)
	if err != nil {
		row.result, row.failed = "failed", "remote"
		return false
	}
	if err := asked.clone(found, member.ref, dir, said); err != nil {
		row.result, row.failed = "failed", "clone"
		return false
	}
	if err := asked.lay(member, dir, said); err != nil {
		row.result, row.failed = "failed", "patch"
		return false
	}
	return true
}

// member places one member and builds it. a failed test is run again to
// tell flaky from failing.
func (asked Options) member(
	ctx context.Context,
	member Member,
	mu *sync.Mutex,
) Row {
	row := Row{member: member}
	said := voice{out: asked.out, name: member.name, mu: mu}
	dir := filepath.Join(asked.work, member.name)
	ok := asked.placed(member, dir, &row, said)
	if !ok {
		return row
	}
	ev, ok := asked.build(ctx, dir, &row, said)
	if ok {
		row.result = "ok"
		return row
	}
	if ev.Step == "test" {
		said.say("test failed; running it again to tell flaky " +
			"from failing")
		row.tries++
		if _, again := asked.run(ctx, dir, &row); again {
			row.result = "flaky"
			return row
		}
	}
	row.result, row.failed = "failed", ev.Step
	return row
}

// lay lays the member's patches on its fresh clone at dir, when the
// stack has a patch to lay them with. it says so only when it laid one.
func (asked Options) lay(member Member, dir string, said voice) error {
	if asked.patch == nil {
		return nil
	}
	laid, err := asked.patch(member, dir)
	if err != nil {
		return err
	}
	if count.Positive(laid) {
		said.say("%d %s laid", laid, plural(laid, "patch", "patches"))
	}
	return nil
}

// plural is one or many of a thing, by count.
func plural(counted int, one, many string) string {
	if counted == count.One {
		return one
	}
	return many
}

// build runs the child, retrying a transient failure with backoff. it returns
// the failing event and false when it does not pass.
func (asked Options) build(
	ctx context.Context,
	dir string,
	row *Row,
	said voice,
) (Event, bool) {
	var last Event
	for attempt := count.First; attempt < asked.tries; attempt++ {
		row.tries++
		ev, ok := asked.run(ctx, dir, row)
		if ok {
			return ev, true
		}
		last = ev
		if ev.Kind != "transient" {
			return ev, false
		}
		// no wait after the last try: nothing comes after it
		if attempt == count.Last(asked.tries) {
			break
		}
		wait := asked.backoff(attempt)
		said.say(
			"%s failed on the network; again in %s",
			ev.Step,
			wait.Round(time.Millisecond),
		)
		asked.sleep(wait)
	}
	return last, false
}

// eventLimit is defined by this package: the most bytes an event line may
// hold, newline included. Any child that writes a longer line fails.
const eventLimit = 1 << 20

// maxText is defined by this package: the most bytes of a failure's text
// that Happened keeps. JSON writes a byte that is not text as six, so a
// line with this much text stays under eventLimit.
const maxText = eventLimit / 8

// cutNote ends a text that was cut.
const cutNote = "\n[output cut]\n"

// cut is text as an event carries it. Text over maxText is cut at the
// end of a character. It says so.
func cut(text string) string {
	if len(text) <= maxText {
		return text
	}
	end := maxText
	floor := end - utf8.UTFMax
	for end > floor && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + cutNote
}

// pipeDelay is defined by this package: how long Wait gives the other
// pipes of a child to close once it has exited. Grandchildren that keep
// one open do not hold the build past this.
const pipeDelay = 2 * time.Second

// run is one child. It reads events as they come and keeps the lint
// count. It returns the first failure. Lines past eventLimit stop the
// child and fail the run. So do failed reads and a cancelled ctx. No
// child is left writing to a pipe that nobody reads.
func (asked Options) run(ctx context.Context, dir string,
	row *Row) (Event, bool) {
	if asked.child == nil {
		none := errors.New("the build was given no child")
		return notStarted(none), false
	}
	if err := ctx.Err(); err != nil {
		text := "cancelled before it started: " + err.Error()
		return Event{Step: "start", State: "fail", Text: text}, false
	}
	cmd := asked.child(dir)
	cmd.Dir = dir
	cmd.WaitDelay = pipeDelay
	out, err := cmd.StdoutPipe()
	if err != nil {
		return notStarted(err), false
	}
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return notStarted(err), false
	}
	finished := make(chan struct{})
	defer close(finished)
	go stopOnCancel(ctx, cmd, out, finished)
	fail, unread := readEvents(out, row)
	if unread != nil {
		stop(cmd, out)
	}
	waitErr := cmd.Wait()
	return outcome(ctx, fail, unread, waitErr, errb.String())
}

// outcome is how a run ended. It is given the first failure event read
// and the error that stopped the reading. It is given how the child
// exited and what it said on its standard error. The failure the child
// reported comes first.
func outcome(
	ctx context.Context, fail *Event, unread, waitErr error, said string,
) (Event, bool) {
	switch {
	case fail != nil:
		return *fail, false
	case ctx.Err() != nil:
		return Event{Step: "child", State: "fail",
			Text: "cancelled: " + ctx.Err().Error()}, false
	case unread != nil:
		return Event{Step: "events", State: "fail",
			Text: unreadText(unread)}, false
	case waitErr != nil:
		text := strings.TrimSpace(said)
		return Event{Step: "child", State: "fail", Kind: Kind(text),
			Text: text}, false
	}
	return Event{Step: "done", State: "ok"}, true
}

// unreadText is why the events stopped being read.
func unreadText(unread error) string {
	return fmt.Sprintf("an event line passed the limit of %d bytes or "+
		"could not be read, so the child was stopped: %v", eventLimit,
		unread)
}

// errTooLong is why a line that looks like an event was refused: it is
// past eventLimit.
var errTooLong = errors.New("an event line passed the limit")

// readEvents reads the child's output to its end. It keeps the lint count
// in row and returns the first failure event. The child also says things
// that are not events. Those lines are skipped whatever their length.
//
// Lines that begin like an event and pass eventLimit are not skipped.
// They are returned as an error, as is a read that failed.
func readEvents(out io.Reader, row *Row) (*Event, error) {
	var fail *Event
	reader := bufio.NewReaderSize(out, eventLimit)
	for {
		line, err := reader.ReadSlice('\n')
		long := errors.Is(err, bufio.ErrBufferFull)
		if long && bytes.HasPrefix(line, []byte("{")) {
			return fail, errTooLong
		}
		for errors.Is(err, bufio.ErrBufferFull) {
			_, err = reader.ReadSlice('\n')
		}
		var seen Event
		if !long && json.Unmarshal(line, &seen) == nil {
			fail = noted(fail, seen, row)
		}
		if err != nil {
			return fail, nonEnd(err)
		}
	}
}

// noted is the first failure after one more event: the failure so far, or
// this event if it is the first. It keeps the lint count in row.
func noted(fail *Event, seen Event, row *Row) *Event {
	if seen.Step == "lint" && seen.State == "ok" {
		row.lint = seen.Count
	}
	if seen.State == "fail" && fail == nil {
		return &seen
	}
	return fail
}

// nonEnd is err unless it is the end of the output.
func nonEnd(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// stop ends a child and lets go of its output. Readers waiting on the
// output then return. Others may still hold the other end of it.
func stop(cmd *exec.Cmd, out io.Closer) {
	_ = cmd.Process.Kill()
	_ = out.Close()
}

// stopOnCancel stops the child if ctx is cancelled before finished is
// closed.
func stopOnCancel(
	ctx context.Context, cmd *exec.Cmd, out io.Closer,
	finished <-chan struct{},
) {
	select {
	case <-ctx.Done():
		stop(cmd, out)
	case <-finished:
	}
}

// notStarted is the event for a child that could not start.
func notStarted(err error) Event {
	return Event{Step: "start", State: "fail", Text: err.Error()}
}

// Clone checks ref of the repository at url out into dir, shallow, once.
// when git refuses it returns what git said, and the error.
func Clone(url, ref, dir string) (string, error) {
	said, err := exec.Command("git", "clone", "--quiet", "--depth", "1",
		"--branch", ref, url, dir).CombinedOutput()
	return strings.TrimSpace(string(said)), err
}

// clone checks ref out into dir, removing dir first. it retries with backoff.
func (asked Options) clone(url, ref, dir string, said voice) error {
	var err error
	for attempt := count.First; attempt < asked.tries; attempt++ {
		os.RemoveAll(dir)
		why, cloneErr := Clone(url, ref, dir)
		if cloneErr == nil {
			return nil
		}
		err = fmt.Errorf("clone %s at %s: %s", url, ref, why)
		// no wait after the last try: nothing comes after it
		if attempt == count.Last(asked.tries) {
			break
		}
		wait := asked.backoff(attempt)
		said.say("clone failed; again in %s",
			wait.Round(time.Millisecond))
		asked.sleep(wait)
	}
	return err
}

// the backoff's ceiling starts at one second and doubles each attempt,
// up to thirty seconds, defined here for a clone or build on a network
// that may come back.
const (
	firstWait   = time.Second      // defined here: the first ceiling
	longestWait = 30 * time.Second // defined here: the last ceiling
)

// backoff is full jitter. it returns a random wait up to a cap that
// starts at one second, doubles each attempt and stops at thirty.
func (asked Options) backoff(attempt int) time.Duration {
	ceiling := firstWait << attempt
	if ceiling > longestWait {
		ceiling = longestWait
	}
	wait := asked.jitter(int64(ceiling))
	return time.Duration(wait)
}

// Table writes the rows as a table a person reads at the end.
func Table(out io.Writer, rows []Row) {
	fmt.Fprintf(
		out,
		"%-18s %-10s %-22s %5s %5s\n",
		"repository",
		"ref",
		"result",
		"lint",
		"tries",
	)
	for _, row := range rows {
		result := row.result
		if row.failed != "" {
			result += " at " + row.failed
		}
		fmt.Fprintf(
			out,
			"%-18s %-10s %-22s %5d %5d\n",
			row.member.name,
			row.member.ref,
			result,
			row.lint,
			row.tries,
		)
	}
}
