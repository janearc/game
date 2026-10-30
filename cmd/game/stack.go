package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/patch"
	"github.com/janearc/game/internal/repo"
	"github.com/janearc/game/internal/stack"
	"github.com/janearc/puzzlemouth/pzm"
)

// stackAsk is build stack's flags: where members come from, how many
// build at once, and whether they are already here.
type stackAsk struct {
	from     string
	parallel int
	here     bool
}

// stackAsked reads build's flags from the call. they belong to build
// stack alone, so a call for anything else that gives one is refused.
func stackAsked(call pzm.Call) (stackAsk, error) {
	ask := stackAsk{from: valueOr(call, "from", "dist"),
		parallel: count.One, here: call.On("here")}
	if call.Given("parallel") {
		many, err := strconv.Atoi(call.Value("parallel"))
		if err != nil {
			return ask, fmt.Errorf(
				"build: --parallel wants a number, not %q",
				call.Value("parallel"))
		}
		ask.parallel = many
	}
	word, _, _ := verbWord(call.Args())
	given := call.Given("from") || call.Given("parallel") || ask.here
	if given && word != "stack" {
		return ask, errors.New("build: --from, --parallel and --here " +
			"belong to build stack")
	}
	return ask, nil
}

// memberURL is where a member comes from, its remote of the kind asked
// for and checked to be there, or why there is none.
func memberURL(
	here repo.Repo,
	cfg config.Config,
	remotes map[string]string,
	from string,
) func(string) (string, error) {
	lookup := &remoting{here: here, cfg: cfg, remotes: remotes, from: from}
	return lookup.url
}

// remoting is where the stack's members come from: the remotes your
// config names, and which kind was asked for.
type remoting struct {
	here    repo.Repo
	cfg     config.Config
	remotes map[string]string
	from    string
}

// url is where one member lives, or why it cannot be found.
func (lookup *remoting) url(name string) (string, error) {
	if found := lookup.remotes[name]; found != "" {
		return found, there(found, lookup.here.Dir(), name,
			lookup.cfg.Dotfile())
	}
	return "", fmt.Errorf(
		"no %s for %s in %s",
		lookup.from,
		name,
		lookup.cfg.Dotfile(),
	)
}

// stackTries is how many attempts a member's clone or build gets at a
// failure on the network, defined here as the first and two more.
const stackTries = 3

// buildStack is game build stack. it builds every member, cloned at its ref
// or taken from this clone with --here, one at a time unless --parallel
// says more. the rows say what passed.
func buildStack(
	here repo.Repo,
	cfg config.Config,
	flags stackAsk,
	out io.Writer,
	child func(string) *exec.Cmd,
) error {
	from, parallel, inPlace := flags.from, flags.parallel, flags.here
	if err := needStack(cfg); err != nil {
		return err
	}
	var remotes map[string]string
	if !inPlace {
		// in place the members sit under their names; none is fetched
		var err error
		if remotes, err = remotesFrom(cfg, from); err != nil {
			return err
		}
	}
	members := membersOf(cfg)
	where, work := from, here.Path("bin", "stack")
	url := memberURL(here, cfg, remotes, from)
	if inPlace {
		where, work, url = "this clone", here.Dir(), nil
	}
	fmt.Fprintf(out, "stack: %d members from %s\n", len(members), where)
	patching := laid(here.Dir(), cfg.Patches())
	asked := stack.Build(members, work, child).From(url).
		Patching(patching).InParallel(parallel).Saying(out).
		Trying(stackTries).Clearing(stack.Clears(out)).
		Sleeping(time.Sleep).Jittered(rand.Int63n)
	rows, err := stack.Run(context.Background(), asked)
	if err != nil {
		return err
	}
	stack.Table(out, rows)
	return tellBuilt(out, rows, work)
}

// laid lays a member's patches on its fresh clone and says how many it
// laid. they come from patches/NAME in the stack at root or from NAME
// under a directory .game names.
func laid(root string, dirs []string) func(stack.Member, string) (int,
	error) {
	return laying{root: root, dirs: dirs}.lay
}

// laying is where a stack keeps its patches: its root, and the
// directories .game names beside patches/.
type laying struct {
	root string
	dirs []string
}

// lay lays one member's patches on its clone at dir and says how many.
func (patches laying) lay(member stack.Member, dir string) (int, error) {
	series, err := patch.Series(patches.root, patches.dirs, member.Name())
	if err != nil {
		return count.Nothing, err
	}
	results, err := patch.Lay(dir, series, false)
	if err != nil {
		return count.Nothing, err
	}
	return len(results), nil
}

// membersOf returns the stack's members in .game's order.
func membersOf(cfg config.Config) []stack.Member {
	stacked := cfg.Stack()
	members := make([]stack.Member, count.Nothing, len(stacked))
	for _, member := range stacked {
		named := stack.Named(member.Name(), member.Ref())
		members = append(members, named)
	}
	return members
}

// tellBuilt shows each member's art, which a child writing to a pipe could
// not, and counts what built clean. any that did not is the error.
func tellBuilt(out io.Writer, rows []stack.Row, work string) error {
	var clean int
	for _, row := range rows {
		if row.Result() == "ok" && count.None(row.Lint()) {
			clean++
		}
	}
	for _, row := range rows {
		paintOk(out, work, row.Name(), row.Result(), painted)
	}
	fmt.Fprintf(out, "stack: %d of %d built clean\n", clean, len(rows))
	if clean != len(rows) {
		return fmt.Errorf(
			"stack: %d did not build clean",
			len(rows)-clean,
		)
	}
	return nil
}

// paintOk shows a member's art with paint when it built, and shows
// nothing of one that did not: a failure is told in the table.
func paintOk(out io.Writer, work, name, result string,
	paint func(out io.Writer, dir, name string) bool) {
	if result != "ok" {
		return
	}
	paint(out, filepath.Join(work, name), name)
}
