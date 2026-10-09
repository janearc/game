// Package repo runs the git binary in a directory, for the little of git that
// game needs.
package repo

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Repo is a working tree with git in it.
type Repo struct {
	Dir string
}

// Git runs a git command in the repo and returns output.
func (r Repo) Git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(
				out.String(),
			), fmt.Errorf(
				"git %s: %s",
				strings.Join(args, " "),
				strings.TrimSpace(errb.String()),
			)
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
		return strings.TrimSpace(
				out.String(),
			), fmt.Errorf(
				"git %s: %s",
				strings.Join(args, " "),
				strings.TrimSpace(errb.String()),
			)
	}
	return strings.TrimSpace(out.String()), nil
}

// GitEnv is Git with extra environment, NAME=value each.
func (r Repo) GitEnv(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(
				out.String(),
			), fmt.Errorf(
				"git %s: %s",
				strings.Join(args, " "),
				strings.TrimSpace(errb.String()),
			)
	}
	return strings.TrimSpace(out.String()), nil
}

// Clean is whether nothing is modified, staged or untracked.
func (r Repo) Clean() (bool, error) {
	out, err := r.Git("status", "--porcelain")
	return out == "", err
}

// Branch is the branch checked out.
func (r Repo) Branch() (string, error) {
	return r.Git("rev-parse", "--abbrev-ref", "HEAD")
}

// Tree is the tree hash of a revision.
func (r Repo) Tree(
	rev string,
) (string, error) {
	return r.Git("rev-parse", rev+"^{tree}")
}

// TreeWithout is the tree of a revision with the given paths removed. it
// builds a temporary index, so the working tree is not touched.
func (r Repo) TreeWithout(rev string, omit []string) (string, error) {
	if len(omit) == 0 {
		return r.Tree(rev)
	}
	idx, err := os.CreateTemp("", "game-index-*")
	if err != nil {
		return "", err
	}
	idx.Close()
	defer os.Remove(idx.Name())
	env := append(os.Environ(), "GIT_INDEX_FILE="+idx.Name())
	if _, err := r.GitEnv(env, "read-tree", rev); err != nil {
		return "", err
	}
	args := append([]string{"rm", "--cached", "-q", "--ignore-unmatch",
		"--"}, omit...)
	if _, err := r.GitEnv(env, args...); err != nil {
		return "", err
	}
	return r.GitEnv(env, "write-tree")
}

// TreeWithLicense is TreeWithout plus the file at from, added to the tree at
// path at. a file that cannot be read or is empty is an error. the working
// copy is not touched.
func (r Repo) TreeWithLicense(
	rev string, omit []string, at, from string,
) (string, error) {
	body, err := os.ReadFile(from)
	if err != nil {
		return "", fmt.Errorf("the licence at %s: %w", from, err)
	}
	if len(body) == 0 {
		return "", fmt.Errorf("the licence at %s is empty", from)
	}
	idx, err := os.CreateTemp("", "game-index-*")
	if err != nil {
		return "", err
	}
	idx.Close()
	defer os.Remove(idx.Name())
	env := append(os.Environ(), "GIT_INDEX_FILE="+idx.Name())
	if _, err := r.GitEnv(env, "read-tree", rev); err != nil {
		return "", err
	}
	if len(omit) > 0 {
		args := append([]string{
			"rm", "--cached", "-q", "--ignore-unmatch", "--",
		}, omit...)
		if _, err := r.GitEnv(env, args...); err != nil {
			return "", err
		}
	}
	blob, err := r.GitEnv(env, "hash-object", "-w", "--", from)
	if err != nil {
		return "", err
	}
	if _, err := r.GitEnv(
		env, "update-index", "--add", "--cacheinfo",
		"100644,"+strings.TrimSpace(blob)+","+at,
	); err != nil {
		return "", err
	}
	return r.GitEnv(env, "write-tree")
}

// RemoteMain is the commit a remote's main is at. returns empty if none.
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

// Files lists every tracked file at a revision, leaving out paths that start
// with any exclude prefix.
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
func (r Repo) Show(
	rev, path string,
) (string, error) {
	return r.Git("show", rev+":"+path)
}
