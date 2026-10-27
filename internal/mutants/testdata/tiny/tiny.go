// Package tiny is a fixture for the mutation run: one function held by
// its tests, one not, and one whose only change does not compile.
package tiny

// Abs is x without its sign.
func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Twice is x doubled, and nothing tests it.
func Twice(x int) int {
	return x * 2
}

// Hello greets; a string has no minus, so its one mutant will not build.
func Hello(name string) string {
	return "hi " + name
}
