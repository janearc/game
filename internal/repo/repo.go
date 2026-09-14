// Package repo is the little of git that game needs, run as the git
// binary in a directory: no library, no second implementation of git.
package repo

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Repo is a working tree with git in it.
type Repo struct {
	Dir string
}

// Git runs one git command in the repository and returns its output,
// trimmed; an error carries git's own words.
func (r Repo) Git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(out.String()), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// GitIn is Git with stdin.
func (r Repo) GitIn(stdin string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(out.String()), fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// Clean is whether nothing is modified, staged or untracked.
func (r Repo) Clean() (bool, error) {
	out, err := r.Git("status", "--porcelain")
	return out == "", err
}

// Branch is the branch checked out.
func (r Repo) Branch() (string, error) { return r.Git("rev-parse", "--abbrev-ref", "HEAD") }

// Tree is the tree hash of a revision.
func (r Repo) Tree(rev string) (string, error) { return r.Git("rev-parse", rev+"^{tree}") }

// RemoteMain is the commit a remote's main is at, or empty when it has
// none yet.
func (r Repo) RemoteMain(remote string) (string, error) {
	out, err := r.Git("ls-remote", remote, "refs/heads/main")
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", nil
	}
	return strings.Fields(out)[0], nil
}

// Files lists every tracked file at a revision, excluding paths under
// the given prefixes.
func (r Repo) Files(rev string, exclude ...string) ([]string, error) {
	out, err := r.Git("ls-tree", "-r", "--name-only", rev)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(out, "\n") {
		if f == "" {
			continue
		}
		skip := false
		for _, ex := range exclude {
			if strings.HasPrefix(f, ex) {
				skip = true
			}
		}
		if !skip {
			files = append(files, f)
		}
	}
	return files, nil
}

// Show is a file's content at a revision.
func (r Repo) Show(rev, path string) (string, error) { return r.Git("show", rev+":"+path) }
