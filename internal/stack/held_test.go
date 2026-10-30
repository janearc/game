package stack_test

import (
	"os"
	"testing"
	"time"

	"github.com/janearc/puzzlemouth/numbers"
)

// held is a number the tests are held to, by its name in
// testdata/numbers.yaml. The run stops and says the name when the file
// or the name is not there.
func held(test *testing.T, name string) float64 {
	test.Helper()
	raw, err := os.ReadFile("testdata/numbers.yaml")
	if err != nil {
		test.Fatal(err)
	}
	from, err := numbers.Read("testdata/numbers.yaml", raw)
	if err != nil {
		test.Fatal(err)
	}
	number, err := from.Get(name)
	if err != nil {
		test.Fatal(err)
	}
	return number.Value
}

// secondsHeld is the seconds the config holds for the name, as a
// duration.
func secondsHeld(test *testing.T, name string) time.Duration {
	test.Helper()
	heldSeconds := int(held(test, name))
	return time.Duration(heldSeconds) * time.Second
}
