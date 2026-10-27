package gittest

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/janearc/game/internal/count"
)

const indexVariable = "GIT_INDEX_FILE="

const (
	verbWord    = 0  // defined by git: the command comes first
	operandWord = 1  // defined by git: its first operand follows
	targetWord  = 2  // defined by git: a second operand follows
	abbrevArgs  = 2  // defined by git: --abbrev-ref and a revision
	infoFields  = 3  // defined by git: --cacheinfo is mode,hash,path
	modeField   = 0  // defined by --cacheinfo: mode first
	hashField   = 1  // defined by --cacheinfo: hash second
	pathField   = 2  // defined by --cacheinfo: path third
	octal       = 8  // defined by git: a mode is octal
	bits32      = 32 // defined by git: a mode fits 32 bits
)

// Run answers a git command, or refuses one it does not know.
func (mem *Mem) Run(
	_, stdin string, env, args []string,
) (string, error) {
	out, err := mem.answer(stdin, env, args)
	if err != nil {
		said := strings.Join(args, " ")
		return out, fmt.Errorf("git %s: %w", said, err)
	}
	return out, nil
}

// answer is what git says to a command. The error is only what git
// would have said.
func (mem *Mem) answer(
	stdin string, env, args []string,
) (out string, err error) {
	defer caught(&err)
	if mem.absent {
		refuse("not a git repository")
	}
	if count.None(len(args)) {
		refuse("no command")
	}
	switch args[verbWord] {
	case "status":
		out = mem.status()
	case "rev-parse":
		out = mem.revParse(args[operandWord:])
	case "show":
		out = mem.show(args)
	case "cat-file":
		out = mem.catFile(stdin, args)
	case "ls-tree":
		out = mem.lsTree(last(args))
	case "hash-object":
		out = mem.hashObject(stdin)
	case "read-tree":
		mem.readTree(indexOf(env), args[operandWord])
	case "rm":
		mem.remove(indexOf(env), args[len(args)-omitted(args):])
	case "update-index":
		mem.updateIndex(indexOf(env), last(args))
	case "diff":
		out = mem.diff(args)
	case "log":
		out = mem.log(args)
	case "write-tree":
		out = mem.writeTree(indexOf(env))
	case "ls-remote":
		out = mem.lsRemote(args[operandWord])
	case "remote":
		out = mem.remoteURL(last(args))
	case "config":
		out = strings.Join(mem.config, "\x00")
	default:
		refuse("memgit does not know the command")
	}
	return out, nil
}

// indexOf is the scratch index a call names, or empty.
func indexOf(env []string) string {
	for _, one := range env {
		if name, ok := strings.CutPrefix(one, indexVariable); ok {
			return name
		}
	}
	return ""
}

// omitted is how many arguments follow the "--" of git rm.
func omitted(args []string) int {
	for at, one := range args {
		if one == "--" {
			return count.Last(len(args)) - at
		}
	}
	return count.Nothing
}

// status is git status --porcelain: a line for each change.
func (mem *Mem) status() string {
	state, err := mem.worktree().Status()
	check(err)
	return state.String()
}

// revParse is a commit or its tree. With --abbrev-ref it is the branch.
func (mem *Mem) revParse(args []string) string {
	if len(args) == abbrevArgs && args[count.First] == "--abbrev-ref" {
		head, err := mem.repo.Head()
		check(err)
		return head.Name().Short()
	}
	rev, tree := strings.CutSuffix(args[count.First], "^{tree}")
	hash := mem.resolve(rev)
	if !tree {
		return hash.String() + "\n"
	}
	return mem.commitAt(rev).TreeHash.String() + "\n"
}

// resolve is the hash a revision names.
func (mem *Mem) resolve(rev string) plumbing.Hash {
	hash, err := mem.repo.ResolveRevision(plumbing.Revision(rev))
	check(err)
	return *hash
}

// commitAt is the commit a revision names.
func (mem *Mem) commitAt(rev string) *object.Commit {
	commit, err := mem.repo.CommitObject(mem.resolve(rev))
	check(err)
	return commit
}

// treeAt is the tree a revision names: a tree by its hash, else the tree
// of a commit.
func (mem *Mem) treeAt(rev string) *object.Tree {
	if tree, err := mem.repo.TreeObject(plumbing.NewHash(rev)); err == nil {
		return tree
	}
	return treeOfCommit(mem.commitAt(rev))
}

// show is a file as committed, from rev:path. With no colon it is a
// commit: what it changed.
func (mem *Mem) show(args []string) string {
	spec := last(args)
	if !strings.Contains(spec, ":") {
		return mem.showCommit(args)
	}
	rev, path, _ := strings.Cut(spec, ":")
	file, err := mem.treeAt(rev).File(path)
	check(err)
	body, err := file.Contents()
	check(err)
	return body
}

// blob is the content of a blob, every byte.
func (mem *Mem) blob(hash plumbing.Hash) string {
	blob, err := mem.repo.BlobObject(hash)
	check(err)
	reader, err := blob.Reader()
	check(err)
	defer reader.Close()
	body, err := io.ReadAll(reader)
	check(err)
	return string(body)
}

// catFile is one blob, or cat-file --batch for the objects on stdin.
func (mem *Mem) catFile(stdin string, args []string) string {
	if args[operandWord] == "blob" {
		return mem.blob(plumbing.NewHash(args[targetWord]))
	}
	var out strings.Builder
	for _, name := range strings.Fields(stdin) {
		body := mem.blob(plumbing.NewHash(name))
		fmt.Fprintf(&out, "%s blob %d\n%s\n", name, len(body), body)
	}
	return out.String()
}

// filesOf is the files of a tree by path. Directories are left out.
func filesOf(tree *object.Tree) (found []string,
	entries map[string]object.TreeEntry) {
	entries = map[string]object.TreeEntry{}
	walker := object.NewTreeWalker(tree, true, nil)
	defer walker.Close()
	for {
		path, entry, err := walker.Next()
		if err == io.EOF {
			return found, entries
		}
		check(err)
		if entry.Mode != filemode.Dir {
			found = append(found, path)
			entries[path] = entry
		}
	}
}

// lsTree is ls-tree -r -z: the files of a revision, not its directories.
func (mem *Mem) lsTree(rev string) string {
	var out strings.Builder
	paths, entries := filesOf(mem.treeAt(rev))
	for _, path := range paths {
		entry := entries[path]
		kind := "blob"
		if entry.Mode == filemode.Submodule {
			kind = "commit"
		}
		fmt.Fprintf(&out, "%06o %s %s\t%s\x00", uint32(entry.Mode),
			kind,
			entry.Hash, path)
	}
	return out.String()
}

// hashObject stores a blob and returns its hash.
func (mem *Mem) hashObject(body string) string {
	stored := mem.repo.Storer.NewEncodedObject()
	stored.SetType(plumbing.BlobObject)
	writer, err := stored.Writer()
	check(err)
	_, err = writer.Write([]byte(body))
	check(err)
	check(writer.Close())
	hash, err := mem.repo.Storer.SetEncodedObject(stored)
	check(err)
	return hash.String()
}

// readTree fills a scratch index with the files of a revision.
func (mem *Mem) readTree(index, rev string) {
	paths, entries := filesOf(mem.treeAt(rev))
	held := map[string]staged{}
	for _, path := range paths {
		held[path] = staged{mode: entries[path].Mode,
			hash: entries[path].Hash}
	}
	mem.indexes[index] = held
}

// remove drops paths from an index; a directory goes with its files.
func (mem *Mem) remove(index string, paths []string) {
	held := mem.indexes[index]
	for path := range held {
		for _, gone := range paths {
			if path == gone || strings.HasPrefix(path, gone+"/") {
				delete(held, path)
			}
		}
	}
}

// updateIndex adds the file that its mode-hash-path triple names.
func (mem *Mem) updateIndex(index, info string) {
	fields := strings.SplitN(info, ",", infoFields)
	if len(fields) != infoFields {
		refuse("cacheinfo %q", info)
	}
	mode, err := strconv.ParseUint(fields[modeField], octal, bits32)
	check(err)
	if mem.indexes[index] == nil {
		mem.indexes[index] = map[string]staged{}
	}
	mem.indexes[index][fields[pathField]] = staged{
		mode: filemode.FileMode(mode),
		hash: plumbing.NewHash(fields[hashField])}
}

// writeTree writes the tree of a scratch index.
func (mem *Mem) writeTree(index string) string {
	return mem.treeOf(mem.indexes[index], "").String()
}

// treeOf writes the tree of the files under prefix and returns its hash.
func (mem *Mem) treeOf(files map[string]staged, prefix string) plumbing.Hash {
	entries := map[string]object.TreeEntry{}
	for path, file := range files {
		below, ok := strings.CutPrefix(path, prefix)
		if !ok {
			continue
		}
		head, _, nested := strings.Cut(below, "/")
		if !nested {
			entries[head] = object.TreeEntry{
				Name: head, Mode: file.mode, Hash: file.hash,
			}
			continue
		}
		if _, seen := entries[head]; !seen {
			entries[head] = object.TreeEntry{Name: head,
				Mode: filemode.Dir,
				Hash: mem.treeOf(files, prefix+head+"/")}
		}
	}
	tree := &object.Tree{}
	for _, entry := range entries {
		tree.Entries = append(tree.Entries, entry)
	}
	sort.Sort(object.TreeEntrySorter(tree.Entries))
	stored := mem.repo.Storer.NewEncodedObject()
	check(tree.Encode(stored))
	hash, err := mem.repo.Storer.SetEncodedObject(stored)
	check(err)
	return hash
}

// lsRemote is the commit a remote's main is at; empty if none.
func (mem *Mem) lsRemote(remote string) string {
	other, ok := mem.remotes[remote]
	if !ok {
		refuse("%q is not a git repository", remote)
	}
	ref, err := other.repo.Reference(
		plumbing.NewBranchReferenceName(trunk), true)
	if err != nil {
		return ""
	}
	return ref.Hash().String() + "\trefs/heads/" + trunk + "\n"
}

// remoteURL is where a push to a remote goes.
func (mem *Mem) remoteURL(remote string) string {
	url, ok := mem.urls[remote]
	if !ok {
		refuse("no such remote '%s'", remote)
	}
	return url + "\n"
}

// last is the final word of a command, where git puts what it works on.
func last(args []string) string {
	return args[count.Last(len(args))]
}
