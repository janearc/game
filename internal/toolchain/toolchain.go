// Package toolchain says when a module asks for a newer go than the one
// game was built with. game reads go source with the go/parser of its own
// build, so a newer module may hold syntax that parser has never seen,
// as go 1.27's methods with type parameters were to a game built on 1.26.
package toolchain

import (
	"fmt"
	"go/version"
	"os"
	"path/filepath"
	"strings"

	"github.com/janearc/game/internal/count"
)

// Explain is err, and when the module at dir asks for a newer go than
// ours, the go game was built with, it also says which two, and that game
// wants rebuilding with the newer one. nil stays nil. an ours that does not
// name a release, as a devel build does, says nothing.
func Explain(dir, ours string, err error) error {
	if err == nil || !version.IsValid(ours) {
		return err
	}
	theirs := wants(dir)
	if theirs == "" || !newer(theirs, ours) {
		return err
	}
	return fmt.Errorf("%w; this module asks for %s and game was built "+
		"with %s, whose parser may not read it: build game with %s",
		err, theirs, ours, theirs)
}

// newer is whether one go version is past another, as go/version
// compares them.
func newer(one, other string) bool {
	later := 1 // defined by go/version: Compare gives 1 when one is newer
	return version.Compare(one, other) == later
}

// wants is the newest go the go.mod at dir asks for, from its go and
// toolchain lines, as go1.N.M; empty without a go.mod or either line.
func wants(dir string) string {
	body, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	want := ""
	for line := range strings.SplitSeq(string(body), "\n") {
		line, _, _ = strings.Cut(line, "//")
		name := versionOn(line)
		if version.IsValid(name) && newer(name, want) {
			want = name
		}
	}
	return want
}

// keyAndValue is the words a go.mod directive line holds, as go has
// defined it: the key and its one value.
const keyAndValue = 2

// versionOn is the go version a go.mod line names, as go1.N.M, from a
// go line or a toolchain line; empty from any other.
func versionOn(line string) string {
	words := strings.Fields(line)
	if len(words) != keyAndValue {
		return ""
	}
	key, value := words[count.First], words[count.Next(count.First)]
	switch key {
	case "go":
		return "go" + value
	case "toolchain":
		return value
	}
	return ""
}
