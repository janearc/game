// Package repo runs git commands in a directory.
package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/janearc/game/internal/count"
)

// Repo is a working tree with git in it.
type Repo struct {
	dir    string
	runner Runner
}

// At is the repository whose working tree is at dir, with git run by
// runner. Production gives it Exec().
func At(dir string, runner Runner) Repo {
	return Repo{dir: dir, runner: runner}
}

// Dir is where the working tree is.
func (here Repo) Dir() string {
	return here.dir
}

// shortSHA is how much of a commit's hash git shows by default, as git
// has defined --short: seven characters.
const shortSHA = 7

// Short is a commit's hash as git shows it short.
func Short(commit string) string {
	return commit[:shortSHA]
}

// Path is a path under the working tree, joined from its parts.
func (here Repo) Path(parts ...string) string {
	return filepath.Join(append([]string{here.dir}, parts...)...)
}

// Git runs a git command in the repo and returns output.
func (here Repo) Git(args ...string) (string, error) {
	return here.run("", nil, args)
}

// GitIn is Git with stdin.
func (here Repo) GitIn(stdin string, args ...string) (string, error) {
	return here.run(stdin, nil, args)
}

// GitEnv is Git with extra environment, NAME=value each.
func (here Repo) GitEnv(env []string, args ...string) (string, error) {
	return here.run("", env, args)
}

// GitEnvIn is Git with extra environment, NAME=value each, and stdin.
func (here Repo) GitEnvIn(
	env []string, stdin string, args ...string,
) (string, error) {
	return here.run(stdin, env, args)
}

// run is every git call but Files. env is added after this process's
// environment, so a name in both takes env's value. the output comes back
// trimmed, and a refusal is an error naming the command and carrying
// git's stderr.
func (here Repo) run(stdin string, env, args []string) (string, error) {
	out, err := here.output(stdin, env, args)
	return strings.TrimSpace(out), err
}

// output is run without the trim, for output where a space is part of a
// name.
func (here Repo) output(stdin string, env, args []string) (string, error) {
	if here.dir == "" {
		return "", fmt.Errorf("git %s: the repository has no directory",
			strings.Join(args, " "))
	}
	if here.runner == nil {
		return "", fmt.Errorf("git %s: the repository has no runner",
			strings.Join(args, " "))
	}
	return here.runner.Run(here.dir, stdin, env, args)
}

// Clean is whether nothing is modified, staged or untracked.
func (here Repo) Clean() (bool, error) {
	out, err := here.Git("status", "--porcelain")
	return out == "", err
}

// Branch is the branch checked out.
func (here Repo) Branch() (string, error) {
	return here.Git("rev-parse", "--abbrev-ref", "HEAD")
}

// Tree is the tree hash of a revision.
func (here Repo) Tree(
	rev string,
) (string, error) {
	return here.Git("rev-parse", rev+"^{tree}")
}

// TreeWithout is the tree of a revision with given paths removed: a file,
// or a directory and everything under it. it builds a temporary index, so
// the working tree is not touched.
func (here Repo) TreeWithout(rev string, omit []string) (string, error) {
	if count.None(len(omit)) {
		return here.Tree(rev)
	}
	return here.indexed(rev, omit, nil)
}

// TreeWithLicense is TreeWithout plus the file at from, added at path at.
// it is an error if the file cannot be read or is empty. the body read is
// the body committed: git is given it on stdin, not the path to read again.
func (here Repo) TreeWithLicense(
	rev string, omit []string, at, from string,
) (string, error) {
	body, err := os.ReadFile(from)
	if err != nil {
		return "", fmt.Errorf("the licence at %s: %w", from, err)
	}
	if count.None(len(body)) {
		return "", fmt.Errorf("the licence at %s is empty", from)
	}
	licence := &adding{at: here, body: string(body), path: at}
	return here.indexed(rev, omit, licence.add)
}

// adding is a file to write into a temporary index: its body, and the
// path it is committed at.
type adding struct {
	at   Repo
	body string
	path string
}

// fileMode is the mode a plain file is committed with, as git has
// defined it: 100644.
const fileMode = "100644"

// add hashes the body into the store and puts it in the index env
// names, at the path.
func (file *adding) add(env []string) error {
	blob, err := file.at.GitIn(file.body, "hash-object", "-w", "--stdin")
	if err != nil {
		return err
	}
	_, err = file.at.GitEnv(env, "update-index", "--add", "--cacheinfo",
		fileMode+","+blob+","+file.path)
	return err
}

// Scratch is a temporary git index. It holds a tree less some paths. It
// is read and written apart from the caller's index and working tree.
type Scratch struct {
	file string
	env  []string
}

// Env is the environment that points git at the scratch index. Add it to
// each git call that works on it. Then the GIT_INDEX_FILE already in the
// environment is not used and is not changed.
func (scratch Scratch) Env() []string { return scratch.env }

// Close removes the scratch index.
func (scratch Scratch) Close() { _ = os.Remove(scratch.file) }

// Scratch is a temporary index with rev read in and omit removed from it.
// Each path in omit is a file or a directory with everything under it.
// The caller closes the index. If it could not be made nothing is left.
func (here Repo) Scratch(rev string, omit []string) (*Scratch, error) {
	index, err := os.CreateTemp("", "game-index-*")
	if err != nil {
		return nil, err
	}
	index.Close()
	// git makes the index itself; the name is what is kept
	os.Remove(index.Name())
	scratch := Scratch{
		file: index.Name(),
		env:  []string{"GIT_INDEX_FILE=" + index.Name()},
	}
	if _, err := here.GitEnv(scratch.env, "read-tree", rev); err != nil {
		scratch.Close()
		return nil, err
	}
	if count.Positive(len(omit)) {
		args := append([]string{
			"rm", "-r", "--cached", "-q", "--ignore-unmatch", "--",
		}, omit...)
		if _, err := here.GitEnv(scratch.env, args...); err != nil {
			scratch.Close()
			return nil, err
		}
	}
	return &scratch, nil
}

// indexed is the tree written from a scratch index. Rev is read in and
// omit is removed from it. Then add runs against the same index.
func (here Repo) indexed(
	rev string, omit []string, add func(env []string) error,
) (string, error) {
	scratch, err := here.Scratch(rev, omit)
	if err != nil {
		return "", err
	}
	defer scratch.Close()
	if add != nil {
		if err := add(scratch.Env()); err != nil {
			return "", err
		}
	}
	return here.GitEnv(scratch.Env(), "write-tree")
}

// RemoteMain is the commit a remote's main is at. returns empty if none.
func (here Repo) RemoteMain(remote string) (string, error) {
	out, err := here.Git("ls-remote", remote, "refs/heads/main")
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", nil
	}
	commit, _, _ := strings.Cut(out, "\t")
	return commit, nil
}

// Show is a file's content at a revision.
func (here Repo) Show(
	rev, path string,
) (string, error) {
	return here.Git("show", rev+":"+path)
}

// Blob is an object's content as stored, every byte, where Show trims
// it: a patch whose last line is a blank line of context needs that line.
func (here Repo) Blob(object string) (string, error) {
	return here.output("", nil, []string{"cat-file", "blob", object})
}
