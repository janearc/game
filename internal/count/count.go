// Package count names the numbers a count is read against, so no
// package writes a bare 0 or 1 to ask whether there is anything, or to
// step past a fencepost. libninja's counting.go is the model.
package count

// Nothing is the count of an empty thing, and where a count starts:
// defined so.
const Nothing = 0

// First is where a list begins, as go has defined it: nothing in.
const First = 0

// One is a single thing, and the step from one place to the next:
// defined so.
const One = 1

// Origin is where lines and columns are counted from, as go/token
// has defined them: one.
const Origin = 1

// Positive is whether a count is more than nothing.
func Positive(count int) bool {
	return count > Nothing
}

// None is whether a count is nothing.
func None(count int) bool {
	return count == Nothing
}

// Last is the place of the last of count things: a fencepost, since
// the count is one past it.
func Last(count int) int {
	return count - One
}

// Next is the place after at.
func Next(at int) int {
	return at + One
}
