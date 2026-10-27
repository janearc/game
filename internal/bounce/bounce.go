// Package bounce flags added lines that name the repository's author.
//
// the check looks for the name as a whole word in added lines.
package bounce

import (
	"regexp"
	"strings"

	"github.com/janearc/game/internal/repo"
)

// Hit is one added line that names the author.
type Hit struct {
	line string
}

// Line is the added line, as the diff shows it.
func (hit Hit) Line() string {
	return hit.line
}

// check looks at a diff for added lines with the name in them.
func check(diff, name string) []Hit {
	if name == "" {
		return nil
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `(?:'s)?\b`)
	var hits []Hit
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") ||
			strings.HasPrefix(line, "+++") {
			continue
		}
		if re.MatchString(line) {
			hits = append(hits, Hit{line: line})
		}
	}
	return hits
}

// Staged checks what is staged in the repository.
func Staged(here repo.Repo, name string) ([]Hit, error) {
	diff, err := here.Git("diff", "--cached", "-U0")
	if err != nil {
		return nil, err
	}
	return check(diff, name), nil
}

// Range checks a range, e.g. main..release, or one commit. a range is
// the lines added across it. one commit is its own added lines, which
// is git show: git diff of one revision would be the tree since then,
// working tree and all, and not the commit at all.
//
// a merge is shown against its first parent, the lines it brought in:
// its combined diff is empty when nothing conflicted.
func Range(here repo.Repo, rng, name string) ([]Hit, error) {
	args := []string{
		"show", "-U0", "--format=", "--diff-merges=first-parent", rng,
	}
	if strings.Contains(rng, "..") {
		args = []string{"diff", "-U0", rng}
	}
	diff, err := here.Git(args...)
	if err != nil {
		return nil, err
	}
	return check(diff, name), nil
}
