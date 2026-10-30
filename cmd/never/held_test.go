package main

import (
	"os"
	"testing"

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
