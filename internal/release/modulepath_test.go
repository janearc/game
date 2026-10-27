package release_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/game/internal/release"
	"github.com/janearc/game/internal/repo"
)

// twoModules is a stack of two members whose go.mod files each begin with
// the go line and a comment before the module line, with the module paths
// given.
func twoModules(t *testing.T, first, second string) (repo.Repo, string,
	[]release.Member) {
	t.Helper()
	root := t.TempDir()
	own := filepath.Join(root, "own")
	gitRepo(t, own, map[string]string{"README.md": "the stack\n"})
	dist := filepath.Join(root, "dist.git")
	gitIn(t, root, "init", "-q", "--bare", dist)
	var members []release.Member
	for name, path := range map[string]string{"p": first, "q": second} {
		at := filepath.Join(root, name)
		gitRepo(t, at, map[string]string{
			"go.mod":     "go 1.25\n// the module\nmodule " + path + "\n",
			name + ".go": "package " + name + "\n",
		})
		members = append(members, release.Taken(name, "main", at, false))
	}
	return repoAt(own), dist, members
}

// the module path is the one on the module line, wherever that is in
// go.mod: two members that differ there are two modules.
func TestTheModulePathIsOnTheModuleLine(t *testing.T) {
	t.Parallel()
	work, dist, members := twoModules(t, "example.com/p", "example.com/q")
	if _, err := release.Stack(work, stackOptions(dist),
		members); err != nil {
		t.Errorf("two modules with leading lines: %v", err)
	}
}

// two members whose module lines say the same path are refused. the path
// is named. it does not matter how many lines come before it.
func TestTwoMembersWithOneModulePathAreRefused(t *testing.T) {
	t.Parallel()
	work, dist, members := twoModules(t, "example.com/m", "example.com/m")
	_, err := release.Stack(work, stackOptions(dist), members)
	if err == nil || !strings.Contains(err.Error(), "example.com/m in ") {
		t.Errorf("one path twice: %v", err)
	}
}
