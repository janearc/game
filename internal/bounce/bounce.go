// Package bounce keeps a repository in the first person: a change that
// adds its author's name in the third person is refused until she signs
// it off. The name is the repository author's, given by the caller; the
// check is the name as a whole word in added lines, so identifiers keep
// their case and are not names.
package bounce

import (
	"regexp"
	"strings"

	"github.com/janearc/game/internal/repo"
)

// Hit is one added line that names the author.
type Hit struct {
	Line string
}

// Check looks at a diff for added lines with the name in them.
func Check(diff, name string) []Hit {
	if name == "" {
		return nil
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `(?:'s)?\b`)
	var hits []Hit
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		if re.MatchString(line) {
			hits = append(hits, Hit{Line: line})
		}
	}
	return hits
}

// Staged checks what is staged in the repository.
func Staged(r repo.Repo, name string) ([]Hit, error) {
	diff, err := r.Git("diff", "--cached", "-U0")
	if err != nil {
		return nil, err
	}
	return Check(diff, name), nil
}

// Range checks a revision or range, e.g. main..release.
func Range(r repo.Repo, rng, name string) ([]Hit, error) {
	diff, err := r.Git("diff", "-U0", rng)
	if err != nil {
		diff, err = r.Git("show", "-U0", "--format=", rng)
		if err != nil {
			return nil, err
		}
	}
	return Check(diff, name), nil
}
