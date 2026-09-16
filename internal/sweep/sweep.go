// Package sweep looks through a tree for what must not be published:
// attribution trailers in history, session urls, uuids, paths on one
// machine, email addresses, and any word from a list kept outside the
// tree, where the names that must never appear live.
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

// the patterns are assembled from pieces so this source never matches
// itself when a tree that holds it is swept.
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

	// the names RFC 2606 reserves are not anybody's address, and a usage
	// line that shows what a setting looks like has to show something.
	// these are the one shape an address may take in a release.
	reserved = regexp.MustCompile(
		`@example\.(com|org|net)$|` +
			`@[A-Za-z0-9.-]*\.(example|invalid|test|localhost)$`,
	)
)

// bare is whether text holds a uuid standing on its own. one inside a
// link is a record id in somebody's catalogue, which a data source is
// entitled to have; a session link is the urls rule's business, so
// nothing is lost by looking past every link here.
func bare(text string) bool {
	return uuids.MatchString(links.ReplaceAllString(text, ""))
}

// mailed is whether text holds an email address that is somebody's. An
// address in a reserved documentation domain is nobody's, and a usage
// line has to be able to show the shape of one.
func mailed(text string) bool {
	for _, at := range emails.FindAllStringIndex(text, -1) {
		m := text[at[0]:at[1]]
		if reserved.MatchString(m) {
			continue
		}
		// git@host:path is a remote in the old ssh spelling, and the
		// dist line of a config is written that way. the colon after
		// it is what tells them apart.
		if strings.HasPrefix(m, "git@") && at[1] < len(text) &&
			text[at[1]] == ':' {
			continue
		}
		return true
	}
	return false
}

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
		return []Hit{
			{Kind: "attribution trailers", File: "(history)"},
		}, nil
	}
	return nil, nil
}

// allowSaid translates the word a .game uses for a kind of hit into the
// kind a hit carries.
//
// two are prefixes, because the hit names the word or the author found.
var allowSaid = map[string]string{
	"paths":    "machine paths",
	"sessions": "session urls",
	"uuids":    "uuids",
	"emails":   "email addresses",
	"author":   "the author ",
	"word":     "listed word ",
}

// Allowed says whether a repository has declared this kind of hit, in this
// file, to be something it means to carry.
//
// a forensics tool's subject is the estate it was used on, so the marks are
// the product rather than a leak. the declaration is per file and per kind
// so that a future real leak in the same file is still found, which a whole
// file exclusion or an --i-mean-it would hide.
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

// Tree sweeps every tracked file at a revision, except those under the
// excluded prefixes, for the fixed patterns and the words.
//
// The author's name is a word like the others when given: a repository
// in the first person does not name its author in the third, in a change
// or in the tree that changes made.
func Tree(
	r repo.Repo,
	rev string,
	words []string,
	author string,
	allow map[string][]string,
	exclude ...string,
) ([]Hit, error) {
	files, err := r.Files(rev, exclude...)
	if err != nil {
		return nil, err
	}
	wordRes := make([]*regexp.Regexp, 0, len(words))
	for _, w := range words {
		wordRes = append(
			wordRes,
			regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(w)+`\b`),
		)
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
		for i, re := range wordRes {
			kind := "listed word " + words[i]
			if re.MatchString(body) && !Allowed(allow, f, kind) {
				hits = append(hits, Hit{kind, f})
			}
		}
		// a licence is the one file whose job is to name the author.
		if authorRe != nil && !isLicence(f) &&
			authorRe.MatchString(body) &&
			!Allowed(allow, f, "the author "+author) {
			hits = append(hits, Hit{"the author " + author, f})
		}
	}
	return hits, nil
}

// isLicence is whether a path is the licence file: `LICENSE` or
// `COPYING`, any case, any extension, at any depth.
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
// patterns and the words; the file named in a hit is the label given.
func Text(label, text string, words []string) []Hit {
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
	for _, w := range words {
		if regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + `\b`).
			MatchString(text) {
			hits = append(hits, Hit{"listed word " + w, label})
		}
	}
	return hits
}
