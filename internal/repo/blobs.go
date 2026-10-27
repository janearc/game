package repo

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/count"
)

// ls-tree -r prints each entry as mode, kind and object, a tab, then
// the path. the kind is the second word and the object the third, as
// git has defined the line.
const (
	metaWords  = 3 // defined by ls-tree: mode kind object
	kindWord   = 1 // defined by ls-tree: mode kind object
	objectWord = 2 // defined by ls-tree: mode kind object
)

// Listed reads one record of ls-tree -r as git prints it: the kind and
// object of what is at path. ok is false for a line that is not one,
// such as the empty string after the last nul.
func Listed(record string) (kind, object, path string, ok bool) {
	meta, path, cut := strings.Cut(record, "\t")
	fields := strings.Fields(meta)
	if !cut || len(fields) != metaWords {
		return "", "", "", false
	}
	return fields[kindWord], fields[objectWord], path, true
}

// cat-file --batch answers with a header of object, kind and size, as
// git has defined it: three words, the size last.
const (
	headerWords = 3 // defined by cat-file --batch: object kind size
	sizeWord    = 2 // defined by cat-file --batch: object kind size
)

// entry is one file in a tree, by its object and its path.
type entry struct {
	kind, object, path string
}

// entries is every file at a revision, less those under an exclude
// prefix, as ls-tree lists them: -z, so a path comes as committed.
func (here Repo) entries(rev string, exclude []string) ([]entry, error) {
	out, err := here.output("", nil, []string{"ls-tree", "-r", "-z", rev})
	if err != nil {
		return nil, err
	}
	var listed []entry
	for _, line := range strings.Split(out, "\x00") {
		kind, object, path, ok := Listed(line)
		if !ok || excluded(path, exclude) {
			continue
		}
		listed = append(listed, entry{kind: kind, object: object,
			path: path})
	}
	return listed, nil
}

// excluded is whether a path is under any of the prefixes.
func excluded(path string, exclude []string) bool {
	for _, prefix := range exclude {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// Blobs gives each file at a revision, less those under an exclude
// prefix, to each in ls-tree's order, with its body as Show gives it. it
// is one ls-tree and one cat-file for the whole tree, where Show is a git
// a file.
//
// an error from each stops the reading and is returned. a submodule is
// refused: it is a commit, and has no body.
func (here Repo) Blobs(
	rev string, exclude []string, each func(path, body string) error,
) error {
	listed, err := here.entries(rev, exclude)
	if err != nil {
		return err
	}
	var ask strings.Builder
	for _, file := range listed {
		if file.kind != "blob" {
			return fmt.Errorf("%s at %s is a %s, not a file",
				file.path, rev, file.kind)
		}
		ask.WriteString(file.object + "\n")
	}
	out, err := here.output(ask.String(), nil,
		[]string{"cat-file", "--batch"})
	if err != nil {
		return err
	}
	for _, file := range listed {
		body, rest, err := answer(out, file.path)
		if err != nil {
			return err
		}
		if err := each(file.path, strings.TrimSpace(body)); err != nil {
			return err
		}
		out = rest
	}
	return nil
}

// answer is the first of cat-file --batch's answers and the rest after
// it. an answer is a line naming the object, its kind and its size, then
// that many bytes and a newline. an object the store has lost is answered
// "missing", in place of a kind and a size, and is refused here.
func answer(out, path string) (body, rest string, err error) {
	header, rest, _ := strings.Cut(out, "\n")
	fields := strings.Fields(header)
	if len(fields) != headerWords {
		return "", "", fmt.Errorf("git cat-file, reading %s: %q", path,
			header)
	}
	size, err := strconv.Atoi(fields[sizeWord])
	if err != nil || size >= len(rest) {
		return "", "", fmt.Errorf("git cat-file, reading %s: %q and "+
			"%d bytes after it", path, header, len(rest))
	}
	afterNewline := count.Next(size)
	return rest[:size], rest[afterNewline:], nil
}
