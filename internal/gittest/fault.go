package gittest

import "fmt"

// check stops the call that made it when err is not nil.
// Go-git on memory fails only when what was asked for is not there.
// The exported calls and Run give the stop back as an error.
func check(err error) {
	if err != nil {
		panic(err)
	}
}

// refuse stops the call that made it, with what it says.
func refuse(format string, args ...any) {
	panic(fmt.Sprintf(format, args...))
}

// caught gives back as an error whatever stopped the call it is deferred
// in.
func caught(err *error) {
	if stopped := recover(); stopped != nil {
		*err = fmt.Errorf("%v", stopped)
	}
}
