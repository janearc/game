package release

import (
	"fmt"
	"path"
	"strings"

	"github.com/janearc/game/internal/count"
	"github.com/janearc/game/internal/patch"
	"github.com/janearc/game/internal/repo"
)

// theirKinds is what a member marked theirs may carry in any of its
// files: its own addresses, urls, uuids and paths. never-words and the
// author are still swept there, patched lines and all.
var theirKinds = []string{"paths", "sessions", "uuids", "emails"}

// settle finishes a member just read into the index: the stack's own
// patches for it are laid, and a member marked theirs is allowed its own
// addresses, urls, uuids and paths.
func (lay *layout) settle(here repo.Repo, env []string, member Member) error {
	if err := lay.patch(here, env, member.name); err != nil {
		return err
	}
	if !member.theirs {
		return nil
	}
	files, err := here.GitEnv(env, "ls-files", "-z", "--", member.name+"/")
	if err != nil {
		return fmt.Errorf("the stack's index: %w", err)
	}
	for _, file := range strings.Split(files, "\x00") {
		if file == "" {
			continue
		}
		if lay.allow == nil {
			lay.allow = map[string][]string{}
		}
		lay.allow[file] = append(lay.allow[file], theirKinds...)
	}
	return nil
}

// patch lays the stack's own patches for a member onto its files in the
// index. they come from the stack's commit in name order. game patch and
// game build stack lay them the same way on a clone. the first that does
// not apply refuses the stack, naming it.
func (lay *layout) patch(here repo.Repo, env []string, name string) error {
	series, err := lay.series(here, name)
	if err != nil {
		return err
	}
	for _, each := range series {
		if err := laid(here, env, name, each.object); err != nil {
			return fmt.Errorf("%s: %s does not apply: %v",
				name, each.file, err)
		}
		if lay.patched == nil {
			lay.patched = map[string]int{}
		}
		lay.patched[name]++
	}
	return nil
}

// ownPatch is one patch in the stack's own commit: its path and object.
type ownPatch struct{ file, object string }

// series is a member's patches in the stack's own commit. they're the
// *.patch files in patches/NAME or in NAME under a directory .game names.
//
// they're read from HEAD, not the index. the index also holds the members
// laid so far, and a member's own files are never the stack's patches.
//
// two places for one member are refused, as game patch refuses them: the
// places and the refusal are internal/patch's.
func (lay *layout) series(here repo.Repo, name string) ([]ownPatch, error) {
	var found string
	var series []ownPatch
	for _, dir := range patch.Places(lay.patches, name) {
		// -z, so a name git would quote, as it does one with an
		// accent in it, comes as committed and is laid like any other
		listed, err := here.Git("ls-tree", "-r", "-z", "HEAD", "--",
			dir+"/")
		if err != nil {
			return nil, fmt.Errorf("the stack's commit: %w", err)
		}
		here := patchesIn(listed, dir)
		if count.None(len(here)) {
			continue
		}
		if found != "" {
			return nil, patch.Twice(name, found, dir)
		}
		found, series = dir, here
	}
	return series, nil
}

// patchesIn is the .patch files directly in dir, from ls-tree -r -z.
func patchesIn(listed, dir string) []ownPatch {
	var here []ownPatch
	for _, record := range strings.Split(listed, "\x00") {
		kind, object, file, ok := repo.Listed(record)
		if !ok || kind != "blob" || path.Dir(file) != dir ||
			!strings.HasSuffix(file, ".patch") {
			continue
		}
		here = append(here, ownPatch{file: file, object: object})
	}
	return here
}

// laid applies one patch, by its object, to a member's files in the
// index, its paths taken under the member's directory. git reads the patch
// from stdin.
func laid(here repo.Repo, env []string, name, object string) error {
	body, err := here.Blob(object)
	if err != nil {
		return err
	}
	_, err = here.GitEnvIn(env, body, "apply", "--cached",
		"--directory="+name+"/", "-")
	return err
}

// omit takes out of the index what a member's own .game keeps out of
// every release of it, under the member's name: a file, or a directory
// and all below it. what a member never ships alone, a stack does not
// ship for it.
func omit(here repo.Repo, env []string, name string, omitted []string) error {
	if count.None(len(omitted)) {
		return nil
	}
	var paths []string
	for _, each := range omitted {
		paths = append(paths, name+"/"+each)
	}
	args := append([]string{
		"rm", "-r", "--cached", "-q", "--ignore-unmatch", "--",
	}, paths...)
	if _, err := here.GitEnv(env, args...); err != nil {
		return fmt.Errorf("%s: its omit lines: %v", name, err)
	}
	return nil
}
