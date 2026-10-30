package main

// game aslr reads a Mach-O header back with otool -hv. This is the part
// of that reading that is not about any one verb.

import "strings"

// machoHead is one Mach-O slice's filetype and flags, read from otool -hv.
type machoHead struct {
	filetype string
	flags    []string
}

// otool -hv prints one line per slice under a heading that names the
// columns: magic cputype cpusubtype caps filetype ncmds sizeofcmds
// flags. The filetype is the fifth column. The flags begin at the
// eighth and run to the end of the line.
const (
	magicColumn    = 0 // the magic, first of the columns otool defined
	filetypeColumn = 4 // the filetype, fifth of the columns otool defined
	flagsColumn    = 7 // the first flag, eighth of those otool defined
)

// machoHeads parses otool -hv into its slices.
func machoHeads(hv string) ([]machoHead, error) {
	var out []machoHead
	for _, line := range strings.Split(hv, "\n") {
		columns := strings.Fields(line)
		if len(columns) <= magicColumn ||
			!strings.HasPrefix(columns[magicColumn], "MH_") {
			continue
		}
		if len(columns) <= flagsColumn {
			return nil, errNoHeader
		}
		out = append(out, machoHead{filetype: columns[filetypeColumn],
			flags: columns[flagsColumn:]})
	}
	if out == nil {
		return nil, errNoHeader
	}
	return out, nil
}

// hasFlag says whether a slice's flags hold one word.
func hasFlag(flags []string, word string) bool {
	for _, flag := range flags {
		if flag == word {
			return true
		}
	}
	return false
}
