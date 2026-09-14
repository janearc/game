// Package sweep looks through a tree for what must not be published:
// attribution trailers in history, session urls, uuids, paths on one
// machine, email addresses, and any word from a list kept outside the
// tree, where the names that must never appear live.
package sweep

import (
	"bufio"
	"os"
	"regexp"
	"strings"

	"github.com/janearc/game/internal/repo"
)

// Hit is one thing found: what kind, in which file.
type Hit struct {
	Kind string
	File string
}

// the patterns are assembled from pieces so this source never matches
// itself when a tree that holds it is swept.
var (
	trailers = regexp.MustCompile("Co-Auth" + "ored-By|Cla" + "ude-Session")
	urls     = regexp.MustCompile("cla" + `ude\.ai|anth` + "ropic|sess" + "ion_[0-9a-f]")
	uuids    = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	paths    = regexp.MustCompile("/Us" + "ers/|~/me" + "sh|~/te" + "mp|~/va" + "r|/ho" + "me/")
	emails   = regexp.MustCompile(`[A-Za-z0-9._-]+@[A-Za-z0-9.-]+\.[a-z]{2,}`)
)

// Words reads the word list, one per line, blanks and # lines ignored.
// A missing file is an empty list.
func Words(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		w := strings.TrimSpace(sc.Text())
		if w != "" && !strings.HasPrefix(w, "#") {
			out = append(out, w)
		}
	}
	return out
}

// History sweeps a revision's commit messages for attribution trailers.
func History(r repo.Repo, rev string) ([]Hit, error) {
	log, err := r.Git("log", rev, "--format=%B")
	if err != nil {
		return nil, err
	}
	if trailers.MatchString(log) {
		return []Hit{{Kind: "attribution trailers", File: "(history)"}}, nil
	}
	return nil, nil
}

// Tree sweeps every tracked file at a revision, except those under the
// excluded prefixes, for the fixed patterns and the words.
func Tree(r repo.Repo, rev string, words []string, exclude ...string) ([]Hit, error) {
	files, err := r.Files(rev, exclude...)
	if err != nil {
		return nil, err
	}
	wordRes := make([]*regexp.Regexp, 0, len(words))
	for _, w := range words {
		wordRes = append(wordRes, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(w)+`\b`))
	}
	var hits []Hit
	for _, f := range files {
		body, err := r.Show(rev, f)
		if err != nil {
			return nil, err
		}
		if !utf8ish(body) {
			continue
		}
		for _, p := range []struct {
			kind string
			re   *regexp.Regexp
		}{{"session urls", urls}, {"uuids", uuids}, {"machine paths", paths}, {"email addresses", emails}} {
			if p.re.MatchString(body) {
				hits = append(hits, Hit{p.kind, f})
			}
		}
		for i, re := range wordRes {
			if re.MatchString(body) {
				hits = append(hits, Hit{"listed word " + words[i], f})
			}
		}
	}
	return hits, nil
}

// utf8ish is a cheap "is this text": no NUL in the first kilobyte.
func utf8ish(s string) bool {
	head := s
	if len(head) > 1024 {
		head = head[:1024]
	}
	return !strings.ContainsRune(head, 0)
}
