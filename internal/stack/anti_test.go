package stack_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/stack"
)

func TestAntiStackParallelZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	urls := reposFor(t, dir)
	members := []stack.Member{stack.Named("bad", "v1"), stack.Named("good", "v1")}
	options := made(members, filepath.Join(dir, "work"), child).
		From(urls.at).InParallel(count.Nothing)
	rows, err := runBefore(t, context.Background(), options)
	if err != nil || len(rows) != count.One {
		t.Fatalf("zero parallelism ran %d members one at a time: %v", len(rows), err)
	}
}

func TestAntiStackNoArtNoCard(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	options, _ := inPlace(t, passing)
	ranOne(t, options.Saying(&out).Clearing(true))
	if got := out.String(); len(got) > count.Nothing && got[count.First] == '\x1b' {
		t.Fatalf("a screen was cleared for no card: %q", got)
	}
}

func TestAntiStackAFixOnlyForADeterministicFailure(t *testing.T) {
	t.Parallel()
	note := &recording{}
	options, _ := inPlace(t,
		scripted(`echo '{"step":"check","state":"fail","kind":"transient"}'`).child)
	ranOne(t, options.Fixing(note))
	if len(note.asked) != count.One {
		t.Fatalf("a failure on the network, tried to the end, was offered "+
			"%d times", len(note.asked))
	}
}

func TestAntiStackDefaultOutputDiscards(t *testing.T) {
	t.Parallel()
	options, _ := inPlace(t, passing)
	rows, err := runBefore(t, context.Background(), options)
	if err != nil || len(rows) != count.One {
		t.Fatalf("a build with no writer: %v", err)
	}
}

// runCame is what stack.Run gave back.
type runCame struct {
	rows []stack.Row
	err  error
}

// sendRun runs the build and sends back what it gave.
func sendRun(ctx context.Context, options stack.Options,
	back chan<- runCame) {
	rows, err := stack.Run(ctx, options)
	back <- runCame{rows: rows, err: err}
}

// runBefore is stack.Run, which ends the test binary when it has not come
// back in the time a build here is given. a build that waits on workers
// that never come would hold every test after it, so it runs apart from
// the test.
func runBefore(t *testing.T, ctx context.Context,
	options stack.Options) ([]stack.Row, error) {
	t.Helper()
	back := make(chan runCame, count.One)
	go sendRun(ctx, options, back)
	limit := secondsHeld(t, "backoff-doubled-fourth-seconds")
	select {
	case got := <-back:
		return got.rows, got.err
	case <-time.After(limit):
		panic("the build never came back")
	}
}
