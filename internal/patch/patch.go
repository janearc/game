// Package patch lays a stack's patches onto a member's fresh clone. the
// patches are the files in patches/NAME, or in NAME under a directory
// .game names with a patches line. they are laid in name order, and each
// is checked and then applied with git apply.
//
// a series lands whole or not at all. git does not promise that for a
// series, so when one patch does not apply, the clone is reset to the
// commit it was cloned at, ignored files and all.
//
// that is why Lay refuses a tree with changes in it, or a directory that
// is not the top of its own clone: a reset is only safe where there is
// nothing of anyone's to lose. it commits nothing.
package patch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/janearc/game/internal/count"
)

// Places is where a member's patches may be, from the stack's root and
// slash-separated: patches/NAME first, then NAME under each directory
// .game names, in the order named. game patch reads them from disk and
// game release stack from the stack's commit, by this one rule.
func Places(dirs []string, member string) []string {
	places := []string{path.Join("patches", member)}
	for _, dir := range dirs {
		under := filepath.ToSlash(dir)
		places = append(places, path.Join(under, member))
	}
	return places
}

// Twice is the refusal for a member whose patches are in two places,
// naming both. which came first would be a guess.
func Twice(member, first, second string) error {
	return fmt.Errorf("%s's patches are in both %s and %s; "+
		"keep them in one place", member, first, second)
}

// find is the patches a member has. They are read from the one place they
// are in: patches/NAME or NAME under a directory a .game patches line
// names. With none anywhere there are no patches.
//
// stacks that can't put patches in a member's own repository keep them
// beside it, as carrie keeps charm's in charmbracelet/patches.
//
// only a place holding a .patch file counts. game release stack counts
// the commit the same way, so an empty directory is never a second place.
// two places for one member are refused. which came first would be a
// guess.
func find(
	root string, dirs []string, member string,
) ([]string, error) {
	var found []string
	var patches []string
	for _, place := range Places(dirs, member) {
		dir := filepath.Join(root, filepath.FromSlash(place))
		held, err := patchesIn(dir)
		if err != nil {
			return nil, err
		}
		if count.Positive(len(held)) {
			found = append(found, place)
			patches = held
		}
	}
	if len(found) < count.Next(count.One) {
		return patches, nil
	}
	first := found[count.First]
	second := found[count.Next(count.First)]
	return nil, fmt.Errorf("patch: %w", Twice(member, first, second))
}

// Series lists a member's patches from the directory find gives. they
// come in name order, which is the order they're laid. each path is
// absolute, since git runs them from the member's clone. no patches is an
// empty series and not an error.
//
// the directory is read, not globbed, so a root named like a pattern,
// a[1] say, is only itself and never another stack's a1.
func Series(root string, dirs []string, member string) ([]string, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf(
			"patch: the stack's root %q is not absolute", root)
	}
	return find(root, dirs, member)
}

// patchesIn is the .patch files directly in dir, by name. missing
// directories hold none, and that's not an error.
func patchesIn(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// ReadDir gives them sorted by name
	var found []string
	for _, each := range entries {
		if !each.IsDir() && strings.HasSuffix(each.Name(), ".patch") {
			at := filepath.Join(dir, each.Name())
			found = append(found, at)
		}
	}
	return found, nil
}

// Result is what happened to one patch: whether it landed, and git's
// words when it did not.
type Result struct {
	patch  string
	landed bool
	why    string
}

// Patch is the path of the patch.
func (result Result) Patch() string {
	return result.patch
}

// Landed is whether the patch applied.
func (result Result) Landed() bool {
	return result.landed
}

// Lay applies the series to dir, in order. it stops at the first patch
// that does not apply and resets dir, so nothing is left half laid. dry
// lays the series the same way, says whether each would land, and resets
// dir after. dir must be a clean git tree.
func Lay(dir string, series []string, dry bool) ([]Result, error) {
	if count.None(len(series)) {
		return nil, nil
	}
	if err := clean(dir); err != nil {
		return nil, err
	}
	var results []Result
	for _, each := range series {
		why, landed := lay(dir, each)
		results = append(results, Result{each, landed, why})
		if landed {
			continue
		}
		if err := reset(dir); err != nil {
			return results, err
		}
		return results, fmt.Errorf("patch: %s does not apply: %s",
			filepath.Base(each), why)
	}
	if dry {
		return results, reset(dir)
	}
	return results, nil
}

// clean says why dir may not be laid on: it is not the top of its own git
// tree, so a reset would land on whatever holds it, or it has changes
// that a reset would take.
func clean(dir string) error {
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("patch: %s is not a git tree: %s", dir, top)
	}
	same, err := samePlace(top, dir)
	if err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("patch: %s is inside the tree at %s, not a "+
			"clone of its own", dir, top)
	}
	changed, err := git(dir, "status", "--porcelain", "--ignored")
	if err != nil {
		return fmt.Errorf("patch: %s: %s", dir, changed)
	}
	if changed != "" {
		return fmt.Errorf("patch: %s has changes in it; patches are "+
			"laid only on a fresh clone", dir)
	}
	return nil
}

// lay checks one patch against dir and applies it, or says why it cannot.
func lay(dir, file string) (string, bool) {
	if why, err := git(dir, "apply", "--check", file); err != nil {
		return why, false
	}
	if why, err := git(dir, "apply", file); err != nil {
		return why, false
	}
	return "", true
}

// reset puts dir back at the commit it was cloned at.
func reset(dir string) error {
	if why, err := git(dir, "reset", "--quiet", "--hard"); err != nil {
		return fmt.Errorf("patch: resetting %s: %s", dir, why)
	}
	why, err := git(dir, "clean", "--quiet", "-d", "-x", "--force")
	if err != nil {
		return fmt.Errorf("patch: cleaning %s: %s", dir, why)
	}
	return nil
}

// samePlace says whether two paths name one directory, after symlinks,
// since a temporary directory on macOS is reached through one.
func samePlace(one, other string) (bool, error) {
	first, err := filepath.EvalSymlinks(one)
	if err != nil {
		return false, err
	}
	second, err := filepath.EvalSymlinks(other)
	if err != nil {
		return false, err
	}
	return first == second, nil
}

// git runs git in dir and returns what it said, trimmed.
func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git",
		append([]string{"-C", dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
