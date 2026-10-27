package mutants_test

import (
	"context"
	"github.com/janearc/game/internal/count"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// startedWhenNoted sends the time the stand-in noted that many pids.
func startedWhenNoted(t *testing.T, stub goStub, many int,
	started chan<- time.Time) {
	for len(stub.pidsOf(t)) < many {
		time.Sleep(standEvery)
	}
	started <- time.Now()
}

// waited runs the one mutant of Hello. It says how long the run lasted from
// the mutant ending. With hold the mutant leaves a child on the output.
func waited(t *testing.T, hold bool) time.Duration {
	t.Helper()
	placed(t)
	stub := stubGo(t)
	root := fixture(t, "")
	stub.answer(t, count.One, "ok\n", count.Nothing)
	if hold {
		holdFor := secondsHeld(t, "wait-delay-hold-seconds")
		seconds := int(holdFor.Seconds())
		stub.keep(t, "hold.2", strconv.Itoa(seconds))
	}
	files, _ := disk(t)
	started := make(chan time.Time, count.One)
	go startedWhenNoted(t, stub, wantPids, started)
	only := regexp.MustCompile(`tiny\.go:Hello`)
	begun := time.Now()
	opts := askFor(count.One, secondsHeld(t, "test-timeout-seconds"))
	_, err := runBefore(t, context.Background(), root,
		opts.OnlyWhere(only).Writing(files))
	returned := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if !hold {
		return returned.Sub(begun)
	}
	select {
	case noted := <-started:
		return returned.Sub(noted)
	case <-time.After(standEvery):
		t.Fatal("the stand-in never left a child holding its output")
	}
	return count.Nothing
}

// TestAGoTestThatLeavesItsOutputHeldIsLetGoOfAfterFiveSeconds: not before
// five and not after.
func TestAGoTestThatLeavesItsOutputHeldIsLetGoOfAfterFiveSeconds(t *testing.T) {
	took := waited(t, true)
	delay := secondsHeld(t, "wait-delay-seconds")
	slack := millisHeld(t, "wait-delay-slack-millis")
	if took < delay-slack {
		t.Errorf("let go of after %v, before the %v it is given", took, delay)
	}
	if took > delay+slack {
		t.Errorf("let go of after %v, past the %v it is given", took, delay)
	}
}

// TestAGoTestThatLeavesNothingHeldIsNotMadeToWait: it ends at once.
func TestAGoTestThatLeavesNothingHeldIsNotMadeToWait(t *testing.T) {
	took := waited(t, false)
	delay := secondsHeld(t, "wait-delay-seconds")
	slack := millisHeld(t, "wait-delay-slack-millis")
	if took > delay-slack {
		t.Errorf("the run took %v with nothing held, near the %v delay",
			took, delay)
	}
}
