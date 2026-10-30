package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/janearc/game/internal/count"
)

// painted shows one directory's art on out, art/<project>.ansi or
// art/header.ansi, and says whether it found any.
//
// look at it. somebody drew this project a whole ansi, one cell at a
// time, in escape codes, and put it in art/<project>.ansi (or
// art/header.ansi). it rules. it absolutely rules.
//
// so every single time a build works, it goes on the screen. fuck yeah
// ansi. fuuuuuck yeah.
//
// it only prints to a terminal, so pipes, logs and child games see
// nothing, and NO_COLOR turns it off.
func painted(out io.Writer, dir, name string) bool {
	if !stdoutColour() {
		return false
	}
	return paint(out, dir, name)
}

// stdoutColour is colour for this process: its NO_COLOR and its stdout.
func stdoutColour() bool {
	info, err := os.Stdout.Stat()
	return colour(os.Getenv("NO_COLOR"), info, err)
}

// colour is whether art goes out at all: not when NO_COLOR is set, and
// only to a terminal, which a character device is and a file or a pipe
// is not.
func colour(noColor string, out os.FileInfo, err error) bool {
	return noColor == "" && err == nil &&
		out.Mode()&os.ModeCharDevice != noMode
}

// noMode is a file mode with no bits set.
var noMode os.FileMode

// paint writes a directory's art to out, and says whether it had any.
func paint(out io.Writer, dir, name string) bool {
	art, found := artFor(dir, name)
	if found {
		show(out, art)
	}
	return found
}

// artFor is a directory's art: art/<name>.ansi, or else art/header.ansi,
// and whether there was any.
func artFor(dir, name string) ([]byte, bool) {
	for _, at := range []string{name + ".ansi", "header.ansi"} {
		art, err := os.ReadFile(filepath.Join(dir, "art", at))
		if err == nil {
			return art, true
		}
	}
	return nil, false
}

// show writes art, ending it with a newline if it has none of its own.
func show(out io.Writer, art []byte) {
	out.Write(art)
	if count.Positive(len(art)) && !bytes.HasSuffix(art, []byte("\n")) {
		fmt.Fprintln(out)
	}
}
