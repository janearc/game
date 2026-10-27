package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/janearc/game/internal/config"
	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/repo"
)

// signed matches the line this house puts last in a commit message, the
// owner's initials, a slash and the session's first field. on a machine
// where every agent commits as its owner, that line is who.
var signed = regexp.MustCompile(`^[a-z]{1,4}/[0-9a-f]{6,}$`)

// whoVerb is game who.
func whoVerb(here repo.Repo, cfg config.Config, _ []string) error {
	return who(here, cfg, os.Stdout)
}

// who says who made the last release on dist, and who the last three
// commits on this branch were.
func who(here repo.Repo, cfg config.Config, out io.Writer) error {
	mine, held := cfg.Mine()
	if !held || mine.Dist() == "" {
		fmt.Fprintln(
			out,
			"released: no dist in your config, so nothing to ask",
		)
	} else {
		said, err := lastRelease(here, mine.Dist())
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "released: "+said)
	}
	branch, err := here.Branch()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "last three on %s:\n", branch)
	lines, err := lastCommits(here, lastThree)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Fprintln(out, "  "+line)
	}
	return nil
}

// lastThree is how many commits who shows, and how many session lines
// a build carries: three, defined here as enough for a glance.
const lastThree = 3

// lastRelease is dist's main as one line, fetched first so it can be
// read.
func lastRelease(here repo.Repo, dist string) (string, error) {
	main, err := here.RemoteMain(dist)
	if err != nil {
		return "", err
	}
	if main == "" {
		return "nothing on " + dist + " yet", nil
	}
	_, err = here.Git("fetch", "-q", dist, "refs/heads/main")
	if err != nil {
		return "", err
	}
	return describe(here, main)
}

// lastCommits is the last few commits on this branch, one line each.
func lastCommits(here repo.Repo, count int) ([]string, error) {
	shas, err := here.Git("log", "-n", strconv.Itoa(count), "--format=%H")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, sha := range strings.Fields(shas) {
		line, err := describe(here, sha)
		if err != nil {
			return nil, err
		}
		out = append(out, line)
	}
	return out, nil
}

// describe returns one commit as a line with the short hash, the date, who
// stands behind it, and the subject. who is the key's holder when git
// verifies a signature, else the author and the session's line when the
// message ends in one, else the author, unsigned.
func describe(here repo.Repo, sha string) (string, error) {
	fields, err := here.Git("log", "-1", logFormat, sha)
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(fields, "\x00", logFields)
	if len(parts) < logFields {
		return "", fmt.Errorf("git log said %q for %s", fields, sha)
	}
	logged := logged(parts)
	by := vouched(logged.author(), logged.verified(), logged.holder(),
		logged.message())
	return fmt.Sprintf("%s %s %s  %s", logged.hash(), logged.day(), by,
		logged.subject()), nil
}

// logFormat is the format who asks git log for: the short hash, the
// date, the author, the signature's state and its holder, the subject
// and the body, nul apart.
const logFormat = "--format=%h%x00%as%x00%an%x00%G?%x00%GS%x00%s%x00%b"

// the fields logFormat defines, in its order.
const (
	hashField     = 0 // defined by logFormat: %h
	dayField      = 1 // defined by logFormat: %as
	authorField   = 2 // defined by logFormat: %an
	verifiedField = 3 // defined by logFormat: %G?
	holderField   = 4 // defined by logFormat: %GS
	subjectField  = 5 // defined by logFormat: %s
	messageField  = 6 // defined by logFormat: %b
	logFields     = 7 // defined by logFormat: seven, nul apart
)

// logged is one commit as logFormat printed it, read by field.
type logged []string

// hash is the short hash.
func (line logged) hash() string { return line[hashField] }

// day is the author date.
func (line logged) day() string { return line[dayField] }

// author is the author's name.
func (line logged) author() string { return line[authorField] }

// verified is the signature's state, as git marks it.
func (line logged) verified() string { return line[verifiedField] }

// holder is who holds the key git verified.
func (line logged) holder() string { return line[holderField] }

// subject is the message's first line.
func (line logged) subject() string { return line[subjectField] }

// message is the message's body.
func (line logged) message() string { return line[messageField] }

// vouched returns who stands behind a commit. that is the holder of a key
// git verified, else the author with the session line the message ends in,
// else the author alone and unsigned.
func vouched(author, verified, holder, message string) string {
	if verified == "G" || verified == "U" {
		return "signed by " + holder
	}
	if last := lastLine(message); signed.MatchString(last) {
		return author + "  " + last
	}
	return author + "  unsigned"
}

// lastLine is a commit message's last line with anything on it, trimmed.
// That is where a session's line goes.
func lastLine(message string) string {
	body := strings.Split(strings.TrimSpace(message), "\n")
	last := body[count.Last(len(body))]
	return strings.TrimSpace(last)
}

// sessionID is the shape of a session's id, hex in fields joined by
// hyphens; machineName is the shape of a host's first label.
var (
	sessionID   = regexp.MustCompile(`^[0-9a-f]+(-[0-9a-f]+)*$`)
	machineName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)
)

// builder is who ran a build and where, for a command's --who. It is the
// session's line as the commit hook makes it and the machine, such as
// "jm/4b732342 on loft". With no session id in the environment, it is
// "no session on loft"; that says nothing of who ran it.
//
// With no user.name in git there are no initials, and the line is the
// session's id alone. Ids not shaped like one count as none. Machines
// with no name of the right shape are "an unnamed machine". So nothing
// typed into either can break the stamp it goes into.
func builder(here repo.Repo, sid, host string) string {
	host, _, _ = strings.Cut(host, ".")
	if !machineName.MatchString(host) {
		host = "an unnamed machine"
	}
	if !sessionID.MatchString(sid) {
		return "no session on " + host
	}
	name, _ := here.Git("config", "user.name")
	line, _, _ := strings.Cut(sid, "-")
	if letters := initials(name); letters != "" {
		line = letters + "/" + line
	}
	return line + " on " + host
}

// initials are the letters a person signs with, as the commit hook makes
// them. Each part of the given names gives a letter, and the family name
// gives none. Hyphenated given names give a letter for each part.
func initials(full string) string {
	parts := strings.Fields(full)
	if len(parts) > count.One {
		parts = parts[:count.Last(len(parts))]
	}
	var letters strings.Builder
	for _, part := range parts {
		for _, piece := range strings.FieldsFunc(part, joins) {
			first := []rune(piece)[count.First]
			letter := strings.ToLower(string(first))
			letters.WriteString(letter)
		}
	}
	return letters.String()
}

// joins is whether a rune joins two parts of one given name.
func joins(char rune) bool { return char == '-' || char == '\'' }

// sessions is the last three different session lines that end the
// commits behind HEAD, newest first and joined with commas. Those lines
// are what the messages say, not a signature. It is "" when no message
// ends in one.
func sessions(here repo.Repo) string {
	log, _ := here.Git("log", "--format=%B%x00")
	var seen []string
	for _, message := range strings.Split(log, "\x00") {
		line := lastLine(message)
		if !signed.MatchString(line) || slices.Contains(seen, line) {
			continue
		}
		seen = append(seen, line)
		if len(seen) == lastThree {
			break
		}
	}
	return strings.Join(seen, ",")
}
