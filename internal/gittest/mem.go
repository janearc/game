package gittest

import (
	"io"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/janearc/game/internal/count"
)

// trunk is the branch a new repository starts on.
const trunk = "main"

// Mem is a git repository in memory, on the branch main.
type Mem struct {
	repo    *git.Repository
	remotes map[string]*Mem
	urls    map[string]string
	config  []string
	indexes map[string]map[string]staged
	absent  bool
}

// staged is a file in a scratch index: its mode and its object.
type staged struct {
	mode filemode.FileMode
	hash plumbing.Hash
}

// New is an empty repository on main.
func New() (mem *Mem, err error) {
	defer caught(&err)
	repo, err := git.Init(memory.NewStorage(), memfs.New())
	check(err)
	head := plumbing.NewSymbolicReference(plumbing.HEAD,
		plumbing.NewBranchReferenceName(trunk))
	check(repo.Storer.SetReference(head))
	return &Mem{
		repo:    repo,
		remotes: map[string]*Mem{},
		urls:    map[string]string{},
		indexes: map[string]map[string]staged{},
	}, nil
}

// worktree is the repository's working tree, which is in memory too.
func (mem *Mem) worktree() *git.Worktree {
	tree, err := mem.repo.Worktree()
	check(err)
	return tree
}

// put writes a file into the working tree without staging it.
func (mem *Mem) put(name, body string) {
	file, err := mem.worktree().Filesystem.Create(name)
	check(err)
	_, err = file.Write([]byte(body))
	check(err)
	check(file.Close())
}

// signature is the author of every commit here. The time is the epoch,
// so the same history has the same hashes.
func signature() *object.Signature {
	return &object.Signature{
		Name: "fixture", Email: "fixture@example.invalid",
		When: time.Unix(count.Nothing, count.Nothing).UTC(),
	}
}

// Commit writes each file into the working tree, stages it and commits.
// It returns the commit's hash.
func (mem *Mem) Commit(
	message string, files map[string]string,
) (hash string, err error) {
	defer caught(&err)
	for name, body := range files {
		mem.put(name, body)
		_, err := mem.worktree().Add(name)
		check(err)
	}
	made, err := mem.worktree().Commit(message,
		&git.CommitOptions{Author: signature()})
	check(err)
	return made.String(), nil
}

// Write puts a file in the working tree without staging it.
func (mem *Mem) Write(name, body string) (err error) {
	defer caught(&err)
	mem.put(name, body)
	return nil
}

// Remote names another repository as a remote, answering ls-remote and
// remote get-url with url.
func (mem *Mem) Remote(name, url string, other *Mem) {
	mem.remotes[name] = other
	mem.urls[name] = url
}

// Config adds a line of git config --null --list, key then newline then
// value.
func (mem *Mem) Config(key, value string) {
	dot := strings.LastIndex(key, ".")
	key = key[:count.Next(dot)] + strings.ToLower(key[count.Next(dot):])
	mem.config = append(mem.config, key+"\n"+value)
}

// Stage stages a file already in the working tree.
func (mem *Mem) Stage(name string) (err error) {
	defer caught(&err)
	_, err = mem.worktree().Add(name)
	return err
}

// Checkout switches to a new branch at the current commit.
func (mem *Mem) Checkout(branch string) (err error) {
	defer caught(&err)
	return mem.worktree().Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch), Create: true,
	})
}

// Names are the files of a tree in git's order. The tree is given by hash.
func (mem *Mem) Names(tree string) (names []string, err error) {
	defer caught(&err)
	found, err := mem.repo.TreeObject(plumbing.NewHash(tree))
	check(err)
	walker := object.NewTreeWalker(found, true, nil)
	defer walker.Close()
	for {
		path, entry, err := walker.Next()
		if err == io.EOF {
			return names, nil
		}
		check(err)
		if entry.Mode != filemode.Dir {
			names = append(names, path)
		}
	}
}

// Gitlink stages and commits a submodule's entry at a path. It points at
// a commit that need not be here.
func (mem *Mem) Gitlink(path, commit string) (hash string, err error) {
	defer caught(&err)
	held, err := mem.repo.Storer.Index()
	check(err)
	held.Entries = append(held.Entries, &index.Entry{Name: path,
		Hash: plumbing.NewHash(commit), Mode: filemode.Submodule})
	check(mem.repo.Storer.SetIndex(held))
	made, err := mem.worktree().Commit("gitlink "+path,
		&git.CommitOptions{Author: signature()})
	check(err)
	return made.String(), nil
}

// Branch makes a branch at the commit a revision names, and stays where
// it is.
func (mem *Mem) Branch(name, rev string) (err error) {
	defer caught(&err)
	mem.point(plumbing.NewBranchReferenceName(name), mem.resolve(rev))
	return nil
}

// point makes a reference name a commit.
func (mem *Mem) point(name plumbing.ReferenceName, commit plumbing.Hash) {
	ref := plumbing.NewHashReference(name, commit)
	check(mem.repo.Storer.SetReference(ref))
}

// Switch moves to a branch that is there.
func (mem *Mem) Switch(branch string) (err error) {
	defer caught(&err)
	return mem.worktree().Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName(branch)})
}

// Merge brings a branch into the current one with a merge commit. It
// gives the commit's hash. Files changed on one side come from that
// side. Files changed on both sides are a conflict, and are refused.
func (mem *Mem) Merge(branch string) (hash string, err error) {
	defer caught(&err)
	ours, theirs := mem.commitAt("HEAD"), mem.commitAt(branch)
	bases, err := ours.MergeBase(theirs)
	check(err)
	base := heldBy(bases[count.First])
	files := threeWay(base, heldBy(ours), heldBy(theirs))
	merged := mem.treeOf(files, "")
	made := &object.Commit{Author: *signature(), Committer: *signature(),
		Message: "Merge branch '" + branch + "'", TreeHash: merged,
		ParentHashes: []plumbing.Hash{ours.Hash, theirs.Hash}}
	stored := mem.repo.Storer.NewEncodedObject()
	check(made.Encode(stored))
	commit, err := mem.repo.Storer.SetEncodedObject(stored)
	check(err)
	head, err := mem.repo.Head()
	check(err)
	mem.point(head.Name(), commit)
	check(mem.worktree().Reset(&git.ResetOptions{Commit: commit,
		Mode: git.HardReset}))
	return commit.String(), nil
}

// heldBy is the files of a commit as an index holds them.
func heldBy(commit *object.Commit) map[string]staged {
	return filesStaged(treeOfCommit(commit))
}

// filesStaged is the files of a tree as an index holds them.
func filesStaged(tree *object.Tree) map[string]staged {
	paths, entries := filesOf(tree)
	held := map[string]staged{}
	for _, path := range paths {
		held[path] = staged{mode: entries[path].Mode,
			hash: entries[path].Hash}
	}
	return held
}

// threeWay is the files of two sides taken from the side that changed
// them, against their base. Both sides changing one file differently is
// refused.
func threeWay(base, ours, theirs map[string]staged) map[string]staged {
	merged := map[string]staged{}
	for _, side := range []map[string]staged{base, ours, theirs} {
		for path := range side {
			merged[path] = take(path, base, ours, theirs)
		}
	}
	for path, file := range merged {
		if file == (staged{}) {
			delete(merged, path)
		}
	}
	return merged
}

// take is the file at a path as the side that changed it holds it.
// Files a side dropped are empty, for threeWay to drop.
func take(path string, base, ours, theirs map[string]staged) staged {
	switch {
	case ours[path] == theirs[path], theirs[path] == base[path]:
		return ours[path]
	case ours[path] == base[path]:
		return theirs[path]
	}
	refuse("%s was changed on both sides", path)
	return staged{}
}

// Nowhere is a place that is no repository: git refuses every command
// in it.
func Nowhere() *Mem {
	return &Mem{absent: true}
}

// Delete removes a file from the working tree and the index and commits
// that. It gives the commit's hash.
func (mem *Mem) Delete(name string) (hash string, err error) {
	defer caught(&err)
	_, err = mem.worktree().Remove(name)
	check(err)
	made, err := mem.worktree().Commit("delete "+name,
		&git.CommitOptions{Author: signature()})
	check(err)
	return made.String(), nil
}

// Staged is the hash of the tree of what is staged, as git write-tree
// says it.
func (mem *Mem) Staged() (tree string, err error) {
	defer caught(&err)
	return mem.indexTree().Hash.String(), nil
}
