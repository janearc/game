package config

import (
	"fmt"
	"strings"

	pzm "github.com/janearc/puzzlemouth/config"
)

// spoken is a line as pzm read it: the words after the key, one space
// apart, with the one pair of quotes around them taken off; the tail
// after " :: " for a key that takes one; and the line it was on, from one.
// A key not said at all is a spoken with no line.
type spoken struct {
	Value string
	Tail  string
	At    int
}

// SetLine keeps the line. A pair of quotes may not change how many words
// the line has, unless it holds none: a value read as words later must
// still be the words pzm counted.
func (said *spoken) SetLine(from pzm.Line) error {
	joined := strings.Join(from.Words, " ")
	value := unquoted(joined)
	words, had := len(strings.Fields(value)), len(from.Words)
	if words != had && words != 0 {
		return fmt.Errorf("the quotes around %q change how many words "+
			"it has, from %d to %d", joined, had, words)
	}
	*said = spoken{Value: value, Tail: from.Tail, At: from.At}
	return nil
}

// file is every key game's files may say, the dotfile and a repository's
// .game alike. What a key means, and which of the two files may say it, is
// settled when it is read into the config.
type file struct {
	Author  spoken    `config:"author,1+" help:"who the bounce refuses in the third person"`
	License spoken    `config:"license,1+" help:"the one license every release carries"`
	Words   spoken    `config:"words,1+" help:"the sweep's word list, in the builder's config"`
	Name    spoken    `config:"name,1" help:"which project this repository is"`
	Road    []spoken  `config:"road,1+" help:"a project's dirty remote; it belongs in a project block"`
	Dist    []spoken  `config:"dist,1+" help:"a project's clean remote; it belongs in a project block"`
	Public  []spoken  `config:"public,1+" help:"retired, now dist"`
	Exclude []spoken  `config:"exclude,1+" help:"a path prefix the sweep leaves alone"`
	Omit    []spoken  `config:"omit,1+" help:"a file or directory left out of every release"`
	Stack   []spoken  `config:"stack,2-3" help:"a member: its name, its ref, and theirs if it is someone else's"`
	Patches []spoken  `config:"patches,1" help:"a directory where the stack keeps members' patches"`
	Needs   []spoken  `config:"needs,2+" help:"a setting the repository needs, and why"`
	Set     []spoken  `config:"set,2+" help:"a value the builder gives, a name and its value"`
	Target  []spoken  `config:"target,1-2,tail" help:"a build: its name, its directory if it has one, and its command"`
	Run     []spoken  `config:"run,1-2,tail" help:"a thing to run: its name, its directory if it has one, and its command"`
	Sweep   []spoken  `config:"sweep,3+" help:"the kinds of sweep hit one file carries: allow FILE KIND"`
	Lint    []spoken  `config:"lint,1+" help:"a lint rule and its arguments, or an allowance"`
	Rapid   spoken    `config:"rapid,2" help:"how many checks a property test runs: checks N"`
	Cover   []spoken  `config:"cover,1+" help:"the coverage floor, or a file left out of it"`
	Project []project `config:"project,1" help:"a project's block in the builder's config"`
}

// project is one block of the builder's config: the project's name on its
// opening line, then what is indented under it.
type project struct {
	Name spoken   `opening:"the project's name"`
	Road spoken   `config:"road,1+" help:"the dirty remote, which takes every commit"`
	Dist spoken   `config:"dist,1+" help:"the clean remote, which only a release push sends to"`
	Set  []spoken `config:"set,2+" help:"a value this project is given, a name and its value"`
}

// schema is what a file may say. It is nil when the declaration above is
// wrong, and every reading then fails; a test makes it.
var schema, _ = pzm.NewSchema[file]()
