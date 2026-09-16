// Package stack builds many repositories as one. A stack file names each
// member and the ref to build; the builder's config says where each one
// lives, on the road or on dist.
//
// Each is cloned fresh and built by game in a child process of its own,
// which reports back as events, one line of JSON each. The parent alone
// decides what passed.
//
// The events are JSON lines described by a Go type rather than a protobuf
// message: game needs git and go and nothing else, and the only reader of
// the stream is game.
package stack

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Member is one repository in a stack, and the tag or branch to build.
type Member struct {
	Name string
	Ref  string
}

// Event is one line a child writes: a step starting, passing or failing.
type Event struct {
	Repo  string `json:"repo,omitempty"`
	Step  string `json:"step"`           // check, test, build, lint
	State string `json:"state"`          // start, ok, fail
	Kind  string `json:"kind,omitempty"` // transient or deterministic, on a fail
	Count int    `json:"count,omitempty"`
	Text  string `json:"text,omitempty"`
}

// transient is output that says the network failed rather than the code:
// worth another try, where a compile error is not.
var transient = regexp.MustCompile(
	`(?i)dial tcp|i/o timeout|connection reset|TLS handshake|` +
		`temporary failure|could not resolve|\b50[234]\b|` +
		`unexpected EOF`,
)

// Kind is transient for output that reads as the network, deterministic
// for everything else.
func Kind(output string) string {
	if transient.MatchString(output) {
		return "transient"
	}
	return "deterministic"
}

// Failure is what a Fixer is handed: which member, where it is checked
// out, which step failed, and the child's last words.
type Failure struct {
	Member
	Dir  string
	Step string
	Text string
}

// Fixer is the seam where an agent repairs a deterministic failure. It
// returns the name of a branch holding a fix, which the stack rebuilds
// from, or an empty name to give up. A fix never makes a ref pass: the
// row says what failed and where it passed after.
type Fixer interface {
	Fix(ctx context.Context, f Failure) (branch string, err error)
}

// GiveUp is the Fixer that fixes nothing, the only one game ships.
type GiveUp struct{}

// Fix gives up.
func (GiveUp) Fix(context.Context, Failure) (string, error) { return "", nil }

// Row is one member's outcome, for the table.
type Row struct {
	Member
	Result string // ok, flaky, failed, or fixed on a named branch
	Failed string // the step that failed, when one did
	Lint   int
	Tries  int
	Text   string
}

// Options are what a stack build needs.
type Options struct {
	Members []Member
	URL     func(name string) (string, error) // where a member lives
	// the directory clones go under
	Work     string
	Child    func(dir string) *exec.Cmd // the build a member gets
	Parallel int
	// Quiet leaves the members' art out, for a log rather than a
	// terminal.
	Quiet bool
	Tries int // attempts at a transient failure, clone or build
	Fixer Fixer
	Out   io.Writer
	Sleep func(time.Duration)
}

// Run builds every member, at most Parallel at once, and returns a row
// each, in the stack's order. The error is only for a stack that could
// not start; a member that fails is a row, not an error.
func Run(ctx context.Context, o Options) ([]Row, error) {
	if o.Parallel < 1 {
		// one at a time by default: the members announce themselves
		// with their own art as they are reached, and a failure stops
		// the run where it happened rather than in a table at the end.
		o.Parallel = 1
	}
	if o.Tries < 1 {
		o.Tries = 3
	}
	if o.Fixer == nil {
		o.Fixer = GiveUp{}
	}
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	if err := os.MkdirAll(o.Work, 0o755); err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(o.Members))
	var mu sync.Mutex
	if o.Parallel == 1 {
		// one at a time, which is what lets a member announce itself
		// as it is reached and what makes a stop mean "here".
		for _, m := range o.Members {
			o.card(m, &mu)
			row := o.member(ctx, m, &mu)
			rows = append(rows, row)
			if row.Result == "failed" {
				return rows, nil
			}
		}
		return rows, nil
	}
	all := make([]Row, len(o.Members))
	sem := make(chan struct{}, o.Parallel)
	var wg sync.WaitGroup
	for i, m := range o.Members {
		wg.Add(1)
		go func(i int, m Member) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			all[i] = o.member(ctx, m, &mu)
		}(i, m)
	}
	wg.Wait()
	return all, nil
}

// art is a member's own card: art/NAME.ansi, where NAME is what the
// member's .game calls the project, since a directory called
// cassowary-dist holds a project that is still called cassowary.
//
// the directory name is tried too, and then a lone .ansi in art/, so a
// member that named its file sensibly is not punished for it.
func (o Options) art(m Member) ([]byte, bool) {
	dir := filepath.Join(o.Work, m.Name, "art")
	names := []string{m.Name}
	if game, err := os.ReadFile(
		filepath.Join(o.Work, m.Name, ".game"),
	); err == nil {
		for _, line := range strings.Split(string(game), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "name ") {
				continue
			}
			called := strings.TrimSpace(
				strings.TrimPrefix(line, "name "),
			)
			names = append([]string{called}, names...)
			break
		}
	}
	for _, n := range names {
		if b, err := os.ReadFile(
			filepath.Join(dir, n+".ansi"),
		); err == nil && len(b) > 0 {
			return b, true
		}
	}
	found, err := filepath.Glob(filepath.Join(dir, "*.ansi"))
	if err != nil || len(found) != 1 {
		return nil, false
	}
	b, err := os.ReadFile(found[0])
	if err != nil || len(b) == 0 {
		return nil, false
	}
	return b, true
}

// card writes a member's art to the output as the build reaches it, if
// the member has any and the output is something a person is watching.
//
// it is deliberately quiet about failure: art that cannot be read is not
// a reason to stop a build, and a member with none is the ordinary case.
func (o Options) card(m Member, mu *sync.Mutex) {
	if o.Quiet {
		return
	}
	b, ok := o.art(m)
	if !ok {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	// one screen at a time: the card for the member being built is the
	// whole of it. only when a person is watching -- a redirected build
	// keeps its log, and a stopped build keeps its failure on the
	// screen, because nothing clears after the last one.
	if f, ok := o.Out.(*os.File); ok {
		if fi, err := f.Stat(); err == nil &&
			fi.Mode()&os.ModeCharDevice != 0 {
			fmt.Fprint(o.Out, "\x1b[H\x1b[2J")
		}
	}
	fmt.Fprintf(o.Out, "%s\n\n", strings.TrimRight(string(b), "\n"))
}

// member clones one member and builds it, retrying what is transient,
// telling a flaky test from a failing one, and handing a deterministic
// failure to the Fixer.
func (o Options) member(ctx context.Context, m Member, mu *sync.Mutex) Row {
	row := Row{Member: m}
	say := func(format string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(
			o.Out,
			"stack %s: "+format+"\n",
			append([]any{m.Name}, a...)...)
	}
	dir, url := filepath.Join(o.Work, m.Name), ""
	if o.URL == nil {
		// no remote to ask: the members are already here, which is
		// what a clone of an assembled stack looks like. a member is
		// whatever sits under its name; one with no go.mod is checked
		// by its own test target, not by go.
		if _, err := os.Stat(filepath.Join(dir, ".game")); err != nil {
			row.Result, row.Failed = "failed", "here"
			row.Text = m.Name + " is not in this clone"
			return row
		}
		say("in place")
	} else {
		found, err := o.URL(m.Name)
		if err != nil {
			row.Result, row.Failed = "failed", "remote"
			row.Text = err.Error()
			return row
		}
		url = found
		if err := o.clone(url, m.Ref, dir, &row, say); err != nil {
			row.Result, row.Failed = "failed", "clone"
			row.Text = err.Error()
			return row
		}
	}
	ev, ok := o.build(ctx, dir, &row, say)
	if ok {
		row.Result = "ok"
		return row
	}
	if ev.Step == "test" {
		say("test failed; running it again to tell flaky from failing")
		row.Tries++
		if _, again := o.run(ctx, dir, &row); again {
			row.Result = "flaky"
			return row
		}
	}
	row.Result, row.Failed, row.Text = "failed", ev.Step, ev.Text
	branch, err := o.Fixer.Fix(
		ctx,
		Failure{Member: m, Dir: dir, Step: ev.Step, Text: ev.Text},
	)
	if err != nil || branch == "" {
		return row
	}
	if url == "" {
		// a fix lives on a branch of a remote, and there is no remote
		// when the members are already here.
		return row
	}
	say("a fix is offered on %s; building it", branch)
	fixed := filepath.Join(o.Work, m.Name+"-fix")
	if err := o.clone(url, branch, fixed, &row, say); err != nil {
		return row
	}
	if _, ok := o.build(ctx, fixed, &row, say); ok {
		row.Result = "fixed on " + branch
	}
	return row
}

// build runs the child, retrying a transient failure with backoff, and
// returns the failing event when it does not pass.
func (o Options) build(
	ctx context.Context,
	dir string,
	row *Row,
	say func(string, ...any),
) (Event, bool) {
	var last Event
	for attempt := 0; attempt < o.Tries; attempt++ {
		row.Tries++
		ev, ok := o.run(ctx, dir, row)
		if ok {
			return ev, true
		}
		last = ev
		if ev.Kind != "transient" {
			return ev, false
		}
		wait := o.backoff(attempt)
		say(
			"%s failed on the network; again in %s",
			ev.Step,
			wait.Round(time.Millisecond),
		)
		o.Sleep(wait)
	}
	return last, false
}

// run is one child: its events read as they come, the lint count kept, and
// the first failure returned.
func (o Options) run(ctx context.Context, dir string, row *Row) (Event, bool) {
	cmd := o.Child(dir)
	cmd.Dir = dir
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Event{
			Step:  "start",
			State: "fail",
			Text:  err.Error(),
		}, false
	}
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return Event{
			Step:  "start",
			State: "fail",
			Text:  err.Error(),
		}, false
	}
	var fail *Event
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		var ev Event
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Step == "lint" && ev.State == "ok" {
			row.Lint = ev.Count
		}
		if ev.State == "fail" && fail == nil {
			e := ev
			fail = &e
		}
	}
	waitErr := cmd.Wait()
	if fail != nil {
		return *fail, false
	}
	if waitErr != nil {
		text := strings.TrimSpace(errb.String())
		return Event{
			Step:  "child",
			State: "fail",
			Kind:  Kind(text),
			Text:  text,
		}, false
	}
	return Event{Step: "done", State: "ok"}, true
}

// clone checks a ref out fresh, retrying with backoff: a clone that fails
// is almost always the network.
func (o Options) clone(
	url, ref, dir string,
	row *Row,
	say func(string, ...any),
) error {
	var err error
	for attempt := 0; attempt < o.Tries; attempt++ {
		os.RemoveAll(dir)
		cmd := exec.Command(
			"git",
			"clone",
			"--quiet",
			"--depth",
			"1",
			"--branch",
			ref,
			url,
			dir,
		)
		var b strings.Builder
		cmd.Stderr = &b
		if err = cmd.Run(); err == nil {
			return nil
		}
		err = fmt.Errorf(
			"clone %s at %s: %s",
			url,
			ref,
			strings.TrimSpace(b.String()),
		)
		if attempt < o.Tries-1 {
			wait := o.backoff(attempt)
			say(
				"clone failed; again in %s",
				wait.Round(time.Millisecond),
			)
			o.Sleep(wait)
		}
	}
	return err
}

// backoff is full jitter: a random wait up to a cap that doubles with
// each attempt, so many builds waiting on one remote do not return to it
// in step.
func (o Options) backoff(attempt int) time.Duration {
	ceiling := time.Second << attempt
	if ceiling > 30*time.Second {
		ceiling = 30 * time.Second
	}
	return time.Duration(rand.Int63n(int64(ceiling)))
}

// Table writes the rows as a table a person reads at the end.
func Table(w io.Writer, rows []Row) {
	fmt.Fprintf(
		w,
		"%-18s %-10s %-22s %5s %5s\n",
		"repository",
		"ref",
		"result",
		"lint",
		"tries",
	)
	for _, r := range rows {
		result := r.Result
		if r.Failed != "" && !strings.HasPrefix(result, "fixed") {
			result += " at " + r.Failed
		}
		fmt.Fprintf(
			w,
			"%-18s %-10s %-22s %5d %5d\n",
			r.Name,
			r.Ref,
			result,
			r.Lint,
			r.Tries,
		)
	}
}
