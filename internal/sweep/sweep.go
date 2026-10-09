// Package sweep finds forbidden content in a tree. forbidden content
// includes attribution in history, session urls, uuids, machine paths,
// emails, and never-words. never-words are in never.go as salted hashes.
package sweep

import (
	"bufio"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/janearc/game/internal/repo"
)

// Hit is one thing found: what kind, in which file.
type Hit struct {
	Kind string
	File string
}

// the patterns avoid matching this source when a tree is swept.
var (
	trailers = regexp.MustCompile("Co-Auth" + "ored-By|Cla" + "ude-Session")
	urls     = regexp.MustCompile(
		"cla" + `ude\.ai|anth` + "ropic|sess" + "ion_[0-9a-f]",
	)
	uuids = regexp.MustCompile(
		`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`,
	)
	links = regexp.MustCompile(`https?://\S+`)
	paths = regexp.MustCompile(
		"/Us" + "ers/|~/me" + "sh|~/te" + "mp|~/va" + "r|/ho" + "me/",
	)
	emails = regexp.MustCompile(
		`[A-Za-z0-9._-]+@[A-Za-z0-9.-]+\.[a-z]{2,}`,
	)

	// reserved matches addresses in the domains RFC 2606 reserves. they
	// are nobody's, so they pass.
	reserved = regexp.MustCompile(
		`@example\.(com|org|net)$|` +
			`@[A-Za-z0-9.-]*\.(example|invalid|test|localhost)$`,
	)
)

// bare checks if text holds a uuid not inside a link.
func bare(text string) bool {
	return uuids.MatchString(links.ReplaceAllString(text, ""))
}

// mailed checks if text has a non-reserved email.
func mailed(text string) bool {
	for _, at := range emails.FindAllStringIndex(text, -1) {
		m := text[at[0]:at[1]]
		if reserved.MatchString(m) {
			continue
		}
		// git@host:path is a remote, not an address. the colon after
		// the host tells them apart.
		if strings.HasPrefix(m, "git@") && at[1] < len(text) &&
			text[at[1]] == ':' {
			continue
		}
		return true
	}
	return false
}

// Words reads the never-words list. ignores blanks and # lines. returns empty
// if file missing.
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
		return []Hit{
			{Kind: "attribution trailers", File: "(history)"},
		}, nil
	}
	return nil, nil
}

// allowSaid maps game terms to hit kinds.
// two are prefixes, because the hit names the word or the author found.
var allowSaid = map[string]string{
	"paths":    "machine paths",
	"sessions": "session urls",
	"uuids":    "uuids",
	"emails":   "email addresses",
	"author":   "the author ",
	"word":     "listed word ",
}

// Allowed checks if a file and kind are permitted by the .game config.
// it allows specific kinds in specific files. anything else is blocked.
func Allowed(allow map[string][]string, file, kind string) bool {
	for _, said := range allow[file] {
		want, ok := allowSaid[said]
		if !ok {
			continue
		}
		if strings.HasSuffix(want, " ") {
			if strings.HasPrefix(kind, want) {
				return true
			}
			continue
		}
		if kind == want {
			return true
		}
	}
	return false
}

// Tree sweeps every tracked file at rev, except under the excluded prefixes.
// it checks the fixed patterns, the never-words and the author's name when
// given. allow lets a file carry listed kinds, never a never-word.
func Tree(
	r repo.Repo,
	rev string,
	never Never,
	author string,
	allow map[string][]string,
	exclude ...string,
) ([]Hit, error) {
	files, err := r.Files(rev, exclude...)
	if err != nil {
		return nil, err
	}
	var authorRe *regexp.Regexp
	if author != "" {
		authorRe = regexp.MustCompile(
			`(?i)\b` + regexp.QuoteMeta(author) + `\b`,
		)
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
			kind  string
			found func(string) bool
		}{
			{"session urls", urls.MatchString},
			{"uuids", bare},
			{"machine paths", paths.MatchString},
			{"email addresses", mailed},
		} {
			if p.found(body) && !Allowed(allow, f, p.kind) {
				hits = append(hits, Hit{p.kind, f})
			}
		}
		if never.found(body) {
			hits = append(hits, Hit{NeverWord, f})
		}
		// a licence file may name the author.
		if authorRe != nil && !isLicence(f) &&
			authorRe.MatchString(body) &&
			!Allowed(allow, f, "the author "+author) {
			hits = append(hits, Hit{"the author " + author, f})
		}
	}
	return hits, nil
}

// isLicence is whether a path is a file called `LICENSE`, `LICENCE` or
// `COPYING`, in any case, with any extension, at any depth.
func isLicence(f string) bool {
	base := strings.ToUpper(path.Base(f))
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	return base == "LICENSE" || base == "LICENCE" || base == "COPYING"
}

// utf8ish is a cheap "is this text": no NUL in the first kilobyte.
func utf8ish(s string) bool {
	head := s
	if len(head) > 1024 {
		head = head[:1024]
	}
	return !strings.ContainsRune(head, 0)
}

// Text sweeps one piece of prose, a commit subject say, for the fixed
// patterns, attribution trailers and the never-words. hits name label as
// their file.
func Text(label, text string, never Never) []Hit {
	var hits []Hit
	for _, p := range []struct {
		kind  string
		found func(string) bool
	}{
		{"attribution trailers", trailers.MatchString},
		{"session urls", urls.MatchString},
		{"uuids", bare},
		{"machine paths", paths.MatchString},
		{"email addresses", mailed},
	} {
		if p.found(text) {
			hits = append(hits, Hit{p.kind, label})
		}
	}
	if never.found(text) {
		hits = append(hits, Hit{NeverWord, label})
	}
	return hits
}
