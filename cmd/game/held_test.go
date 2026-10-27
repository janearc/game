package main

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
	return number.Value()
}

// countHeld is the number the config holds for the name, as a count.
func countHeld(test *testing.T, name string) int {
	test.Helper()
	return int(held(test, name))
}

// secondsHeld is the seconds the config holds for the name, as a
// duration.
func secondsHeld(test *testing.T, name string) time.Duration {
	test.Helper()
	seconds := countHeld(test, name)
	return time.Duration(seconds) * time.Second
}
