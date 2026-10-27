package lint_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/janearc/puzzlemouth/numbers"

	"github.com/janearc/game/internal/count"
)

// heldFile is the numbers file beside this helper's source, wherever
// the test has moved to by the time it asks.
func heldFile() string {
	_, here, _, _ := runtime.Caller(count.First)
	return filepath.Join(filepath.Dir(here), "testdata", "numbers.yaml")
}

// held is a number the tests are held to, by its name in
// testdata/numbers.yaml. The run stops and says the name when the file
// or the name is not there.
func held(test *testing.T, name string) float64 {
	test.Helper()
	raw, err := os.ReadFile(heldFile())
	if err != nil {
		test.Fatal(err)
	}
	from, err := numbers.Read(heldFile(), raw)
	if err != nil {
		test.Fatal(err)
	}
	number, err := from.Get(name)
	if err != nil {
		test.Fatal(err)
	}
	return number.Value()
}
