// Package sweep finds forbidden content in a tree. forbidden content
// includes attribution in history, session urls, uuids, machine paths,
// emails, and never-words. never-words are in never.go as salted hashes.
package sweep

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/janearc/game/internal/repo"
)

// Hit is one thing found: what kind, in which file.
type Hit struct {
	kind string
	file string
}

// Found is one hit of a kind in a file.
func Found(kind, file string) Hit {
	return Hit{kind: kind, file: file}
}

// String is the hit as a release's refusal names it: the kind, in the
// file.
func (hit Hit) String() string { return hit.kind + " in " + hit.file }

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

// every is the count regexp has defined as all the matches there are.
const every = -1

// mailed checks if text has a non-reserved email.
func mailed(text string) bool {
	for _, address := range emails.FindAllString(text, every) {
		if reserved.MatchString(address) {
			continue
		}
		// git@host:path is a remote, not an address. the colon after
		// the host tells them apart.
		if strings.HasPrefix(address, "git@") &&
			strings.Contains(text, address+":") {
			continue
		}
		return true
	}
	return false
}

// Words reads the never-words list. It takes a word to a line and passes
// over blanks and lines that begin with a hash. Lines are read whole at
// any length.
//
// Lists that are not there are no list: no words and no error. Lists
// that are there and cannot be read to their end are an error and no
// words.
func Words(path string) ([]string, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out []string
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		word := strings.TrimSpace(line)
		if word != "" && !strings.HasPrefix(word, "#") {
			out = append(out, word)
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// History sweeps a revision's commit messages for attribution trailers.
func History(here repo.Repo, rev string) ([]Hit, error) {
	log, err := here.Git("log", rev, "--format=%B")
	if err != nil {
		return nil, err
	}
	if trailers.MatchString(log) {
		return []Hit{Found("attribution trailers", "(history)")}, nil
	}
	return nil, nil
}

// allowSaid maps game terms to hit kinds. word and author are prefixes,
// since the hit kind ends with the word or name found.
var allowSaid = map[string]string{
	"paths":    "machine paths",
	"sessions": "session urls",
	"uuids":    "uuids",
	"emails":   "email addresses",
	"author":   "the author ",
	"word":     "listed word ",
}

// allowed says whether the .game config permits a hit kind in a file.
// anything not listed is blocked.
func allowed(allow map[string][]string, file, kind string) bool {
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

// fixed is the patterns every swept file is held to, by the kind of hit
// each makes.
var fixed = []struct {
	kind  string
	found func(string) bool
}{
	{"session urls", urls.MatchString},
	{"uuids", bare},
	{"machine paths", paths.MatchString},
	{"email addresses", mailed},
}

// Tree sweeps every tracked file at rev, except under the excluded
// prefixes, for the fixed patterns, the never-words and the author's
// name when given. allow lets a file carry listed kinds, never a
// never-word.
func Tree(
	here repo.Repo,
	rev string,
	never Never,
	author string,
	allow map[string][]string,
	exclude ...string,
) ([]Hit, error) {
	sweeping := &sweeping{never: never, author: author, allow: allow}
	if author != "" {
		sweeping.authorRe = regexp.MustCompile(
			`(?i)\b` + regexp.QuoteMeta(author) + `\b`,
		)
	}
	if err := here.Blobs(rev, exclude, sweeping.file); err != nil {
		return nil, err
	}
	return sweeping.hits, nil
}

// sweeping is one sweep of a tree: what it is held to, and what it has
// found so far.
type sweeping struct {
	never    Never
	author   string
	authorRe *regexp.Regexp
	allow    map[string][]string
	hits     []Hit
}

// file sweeps one file's name and its body. The body is read as bytes.
func (sweep *sweeping) file(file, body string) error {
	named := nameHits(file, sweep.never, sweep.author, sweep.authorRe)
	sweep.hits = append(sweep.hits, named...)
	found := fileHits(file, readable(body), sweep.never, sweep.author,
		sweep.authorRe, sweep.allow)
	sweep.hits = append(sweep.hits, found...)
	return nil
}

// fileHits lists the hits in one file. a file is hit by each fixed pattern
// it is not allowed, by a never-word, and by the author's name unless it is a
// licence or allowed to name them.
func fileHits(
	file, body string,
	never Never,
	author string,
	authorRe *regexp.Regexp,
	allow map[string][]string,
) []Hit {
	var hits []Hit
	for _, pattern := range fixed {
		if !allowed(allow, file, pattern.kind) && pattern.found(body) {
			hits = append(hits, Found(pattern.kind, file))
		}
	}
	if never.found(body) {
		hits = append(hits, Found(neverWord, file))
	}
	// a licence file may name the author.
	if authorRe != nil && !isLicence(file) &&
		authorRe.MatchString(body) &&
		!allowed(allow, file, "the author "+author) {
		hits = append(hits, Found("the author "+author, file))
	}
	return hits
}

// isLicence says whether a path is a file called license, licence or
// copying, in any case, with any extension, at any depth.
func isLicence(file string) bool {
	base := strings.ToUpper(path.Base(file))
	base, _, _ = strings.Cut(base, ".")
	return base == "LICENSE" || base == "LICENCE" || base == "COPYING"
}

// readable is the text of a file to look in. Files with a nul in them are
// given again with the nuls taken out. That finds text written two bytes
// to a letter. Compressed data is not understood. It is not certified.
func readable(body string) string {
	if !strings.ContainsRune(body, nul) {
		return body
	}
	return body + "\n" + strings.ReplaceAll(body, "\x00", "")
}

// nul is the byte text never holds, defined as zero.
const nul = 0

// separators turns the marks that join words in a file's name into
// spaces. The author's name is then found in ada-lovelace.md.
var separators = strings.NewReplacer("-", " ", "_", " ", ".", " ", "/", " ")

// inName is added to the kind of a hit in a file's name.
const inName = " (name)"

// sessionNames is the session id pattern for names. It is the word then
// an underscore then eight hex digits. Names that go on in words are not
// one.
var sessionNames = regexp.MustCompile("sess" + "ion_[0-9a-f]{8}")

// inNames is the fixed patterns a file's name is held to. Addresses and
// machine paths are left out. Asset names for high density screens have
// the shape of an address. Every path is made of paths.
var inNames = []struct {
	kind  string
	found func(string) bool
}{
	{"session urls", sessionNames.MatchString},
	{"uuids", bare},
}

// nameHits lists the hits in a file's name. The kinds are the body's with
// (name) added. No allowance applies.
func nameHits(
	file string, never Never, author string, authorRe *regexp.Regexp,
) []Hit {
	var hits []Hit
	for _, pattern := range inNames {
		if pattern.found(file) {
			hits = append(hits, Found(pattern.kind+inName, file))
		}
	}
	if never.found(file) {
		hits = append(hits, Found(neverWord+inName, file))
	}
	if authorRe != nil && authorRe.MatchString(separators.Replace(file)) {
		kind := "the author " + author + inName
		hits = append(hits, Found(kind, file))
	}
	return hits
}
