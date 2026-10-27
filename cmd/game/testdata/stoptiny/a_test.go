package stoptiny

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// pause is how long the third run of the tests waits: past the end of
// any test that signals the run, and short enough that nothing waits long
// when it is forgotten.
const pause = 20 * time.Second

// heldAt is how many lines the note holds when the tests wait.
const heldAt = 3

// note is the file the run is told of in STOPTINY_NOTE, one line a run
// of these tests, with the pid of the run so that a test that watches the
// file can end it. the third run waits, so a test that watches the file can
// stop the run while a mutant is under test.
func note() {
	path := os.Getenv("STOPTINY_NOTE")
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	fmt.Fprintf(file, "ran %d\n", os.Getpid())
	file.Close()
	body, _ := os.ReadFile(path)
	if strings.Count(string(body), "\n") >= heldAt {
		time.Sleep(pause)
	}
}

func TestFunctionsAreHeld(t *testing.T) {
	note()
	if Sum(1, 2) != 3 || Diff(3, 1) != 2 {
		t.Fatal("Sum or Diff is wrong")
	}
	if Larger(2, 3) != 3 || Larger(3, 2) != 3 {
		t.Fatal("Larger is wrong")
	}
}
