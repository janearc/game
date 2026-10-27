// Package stoptiny is a fixture for the test that stops a mutation run:
// three small functions, each held by a test that notes when it runs.
package stoptiny

// Sum adds.
func Sum(a, b int) int { return a + b }

// Diff subtracts.
func Diff(a, b int) int { return a - b }

// Larger is the greater of two.
func Larger(a, b int) int {
	if a > b {
		return a
	}
	return b
}
