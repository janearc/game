package gittest

import (
	"bytes"
	"slices"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/janearc/game/internal/count"
)

// firstParentFlag is defined by git: show a merge against its parent.
const firstParentFlag = "--diff-merges=first-parent"

const (
	contextFlag    = "-U"       // defined by git: -U<n> is context
	defaultContext = 3          // defined by git: context without -U
	cachedFlag     = "--cached" // defined by git: index against HEAD
	twoDots        = ".."       // defined by git: a..b, a to b
	threeDots      = "..."      // defined by git: a...b, from the base
	singleParent   = 1          // defined by git: a merge has two
)

// contextOf is the lines of context a command asks for with -U.
func contextOf(args []string) int {
	for _, one := range args {
		if lines, ok := strings.CutPrefix(one, contextFlag); ok {
			asked, err := strconv.Atoi(lines)
			check(err)
			return asked
		}
	}
	return defaultContext
}

// patch is the change from one tree to another. Missing trees are empty.
func patch(from, to *object.Tree, context int) string {
	changes, err := object.DiffTree(from, to)
	check(err)
	made, err := changes.Patch()
	check(err)
	var out bytes.Buffer
	check(diff.NewUnifiedEncoder(&out, context).Encode(made))
	return out.String()
}

// headTree is the tree of HEAD, or none before the first commit.
func (mem *Mem) headTree() *object.Tree {
	if _, err := mem.repo.Head(); err != nil {
		return nil
	}
	return mem.treeAt("HEAD")
}

// indexTree is the tree of what is staged.
func (mem *Mem) indexTree() *object.Tree {
	held, err := mem.repo.Storer.Index()
	check(err)
	files := map[string]staged{}
	for _, entry := range held.Entries {
		files[entry.Name] = staged{mode: entry.Mode, hash: entry.Hash}
	}
	tree, err := mem.repo.TreeObject(mem.treeOf(files, ""))
	check(err)
	return tree
}

// diff is git diff of what is staged, or of a range.
func (mem *Mem) diff(args []string) string {
	context := contextOf(args)
	if slices.Contains(args, cachedFlag) {
		return patch(mem.headTree(), mem.indexTree(), context)
	}
	rng := last(args)
	if from, to, ok := strings.Cut(rng, threeDots); ok {
		base, err := mem.commitAt(from).MergeBase(mem.commitAt(to))
		check(err)
		before := treeOfCommit(base[count.First])
		return patch(before, mem.treeAt(to), context)
	}
	from, to, _ := strings.Cut(rng, twoDots)
	return patch(mem.treeAt(from), mem.treeAt(to), context)
}

// treeOfCommit is the tree a commit holds.
func treeOfCommit(commit *object.Commit) *object.Tree {
	tree, err := commit.Tree()
	check(err)
	return tree
}

// showCommit is git show of a commit with no header. It is the change
// against the first parent. Merges are shown so only when asked. Else
// they are empty, as the combined diff of a clean merge is.
func (mem *Mem) showCommit(args []string) string {
	commit := mem.commitAt(last(args))
	if commit.NumParents() > singleParent &&
		!slices.Contains(args, firstParentFlag) {
		return ""
	}
	var before *object.Tree
	if commit.NumParents() > count.Nothing {
		parent, err := commit.Parent(count.First)
		check(err)
		before = treeOfCommit(parent)
	}
	return patch(before, treeOfCommit(commit), contextOf(args))
}
